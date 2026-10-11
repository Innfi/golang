//go:ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>

#define TASK_COMM_LEN 16
#define PATH_LEN 256
#define ARGS_LEN 256

#define AF_INET 2
#define AF_INET6 10

enum event_type {
  EVENT_FORK = 1,
  EVENT_EXEC = 2,
  EVENT_EXIT = 3,
  EVENT_OPEN = 4,
  EVENT_CONNECT = 5,
};

struct event {
  __u64 ts_ns;
  __u64 cgroup_id;
  __u32 type;
  __u32 pid;
  __u32 tid;
  __u32 ppid;
  __u32 uid;
  __s32 ret;
  __u32 flags;
  __u16 family;
  __u16 dport;
  __u8 daddr[16];
  __u32 args_len;
  char comm[TASK_COMM_LEN];
  char path[PATH_LEN];
  char args[ARGS_LEN];
};

const struct event *unused_event __attribute__((unused));

struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 8*1024*1024);
} events SEC(".maps");

struct open_args {
  __u64 filename;
  __u32 flags;
};

struct connect_args {
  __u64 uaddr;
};

struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 16384);
  __type(key, __u32);
  __type(value, struct open_args);
} pending_open SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 16384);
  __type(key, __u32);
  __type(value, struct connect_args);
} pending_connect SEC(".maps");

volatile const __u32 self_tgid = 0;
volatile const bool trace_opens = true;

static __always_inline bool is_self(void) {
  return (bpf_get_current_pid_tgid() >> 32) == self_tgid;
}

static __always_inline struct event *reserve(__u32 type) {
  struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
  if (!e) return NULL;

  __builtin_memset(e, 0, sizeof(*e));

  return e;
}

static __always_inline void fill_current(struct event *e, __u32 type) {
  struct task_struct *task = (struct task_struct *)bpf_get_current_task_btf();
  __u64 id = bpf_get_current_pid_tgid();

  e->type = type;
  e->ts_ns = bpf_ktime_get_boot_ns();
  e->pid = id >> 32;
  e->tid = (__u32)id;
  e->ppid = BPF_CORE_READ(task, real_parent, tgid);
  e->uid = (__u32)bpf_get_current_uid_gid();
  e->cgroup_id = bpf_get_current_cgroup_id();

  bpf_get_current_comm(e->comm, sizeof(e->comm));
}

SEC("tp_btf/sched_process_fork")
int BPF_PROG(handle_fork, struct task_struct *parent, struct task_struct *child) {
  // new thread not new process
  if (child->pid != child->tgid) return 0;
  if (parent->tgid == self_tgid) return 0;

  struct event *e = reserve(EVENT_FORK);
  if (!e) return 0;

  e->type = EVENT_FORK;
  e->ts_ns = bpf_ktime_get_boot_ns();
  e->pid = child->tgid;
  e->tid = child->pid;
  e->ppid = parent->tgid;
  e->uid = (__u32)bpf_get_current_uid_gid();
  e->cgroup_id = bpf_get_current_cgroup_id();

  __builtin_memcpy(e->comm, parent->comm, sizeof(e->comm));
  bpf_ringbuf_submit(e, 0);

  return 0;
}

SEC("tp_bpf/sched_process_exec")
int BPF_PROG(handle_exec, struct task_struct *p, pid_t old_pid, struct linux_binprm *bprm) {
  if (is_self()) return 0;

  struct event *e = reserve(EVENT_EXEC);
  if (!e) return 0;

  fill_current(e, EVENT_EXEC);

  bpf_probe_read_kernel_str(e->path, sizeof(e->path), bprm->filename);

  struct mm_struct *mm = p->mm;
  unsigned long start = BPF_CORE_READ(mm, arg_start);
  unsigned long end = BPF_CORE_READ(mm, arg_end);
  unsigned long len = end - start;

  if (len > ARGS_LEN-1) len = ARGS_LEN - 1;

  len &= ARGS_LEN - 1; // explicit mask ?

  if (bpf_probe_read_user(e->args, len, (void *)start) == 0) e->args_len = len;

  bpf_ringbuf_submit(e, 0);

  return 0;
}

SEC("tp_btf/sched_process_exit")
int BPF_PROG(handle_exit, struct task_struct *p) {
  if (p->pid != p->tgid) return 0;
  if (p->tgid == self_tgid) return 0;

  struct event *e = reserve(EVENT_EXIT);
  if (!e) return 0;

  fill_current(e, EVENT_EXIT);
  e->ret = BPF_CORE_READ(p, exit_code) >> 8;
  bpf_ringbuf_submit(e, 0);

  return 0;
}


// openat

SEC("tracepoint/syscalls/sys_enter_openat")
int handle_openat_enter(struct trace_event_raw_sys_enter *ctx) {
  if (!trace_opens || is_self()) return 0;

  __u32 tid = (__u32)bpf_get_current_pid_tgid();
  struct open_args a = {
    .filename = ctx->args[1],
    .flags = (__u32)ctx->args[2],
  };

  bpf_map_update_elem(&pending_open, &tid, &a, BPF_ANY);

  return 0;
}

SEC("tracepoint/syscalls/sys_exit_openat")
int handle_openat_exit(struct trace_event_raw_sys_exit *ctx) {
  __u32 tid = (__u32)bpf_get_current_pid_tgid();
  struct open_args *a = bpf_map_lookup_elem(&pending_open, &tid);
  if (!a) return 0;

  struct event *e = reserve(EVENT_OPEN);
  if (e) {
    fill_current(e, EVENT_OPEN);

    e->ret = (__s32)ctx->ret;
    e->flags = a->flags;

    bpf_probe_read_user_str(e->path, sizeof(e->path), (const char *)a->filename);
    bpf_ringbuf_submit(e, 0);
  }

  bpf_map_delete_elem(&pending_open, &tid);

  return 0;
}


// connect

SEC("tracepoint/syscalls/sys_enter_connect")
int handle_connect_enter(struct trace_event_raw_sys_enter *ctx) {
  if (is_self()) return 0;

  __u32 tid = (__u32)bpf_get_current_pid_tgid();
  struct connect_args a = {.uaddr = ctx->args[1] };

  bpf_map_update_elem(&pending_connect, &tid, &a, BPF_ANY);

  return 0;
}

SEC("tracepoint/syscalls/sys_exit_connect")
int handle_connect_exit(struct trace_event_raw_sys_exit *ctx) {
  __u32 tid = (__u32)bpf_get_current_pid_tgid();
  struct connect_args *a = bpf_map_lookup_elem(&pending_connect, &tid);
  if (!a) return 0;

  __u16 family = 0;
  bpf_probe_read_user(&family, sizeof(family), (const void *)a->uaddr);
  if (family != AF_INET && family != AF_INET6) {
    bpf_map_delete_elem(&pending_connect, &tid);

    return 0;
  }

  struct event *e = reserve(EVENT_CONNECT);
  if (!e) {
    bpf_map_delete_elem(&pending_connect, &tid);

    return 0;
  }

  fill_current(e, EVENT_CONNECT);
  e->ret = (__s32)ctx->ret;
  e->family = family;

  if (family == AF_INET) {
    struct sockaddr_in sin = {};
    bpf_probe_read_user(&sin, sizeof(sin), (const void *)a->uaddr);
    e->dport = bpf_ntohs(sin.sin_port);
    __builtin_memcpy(e->daddr, &sin.sin_addr.s_addr, 4);
  } else {
    struct sockaddr_in6 sin6 = {};
    bpf_probe_read_user(&sin6, sizeof(sin6), (const void *)a->uaddr);
    e->dport = bpf_ntohs(sin6.sin6_port);
    __builtin_memcpy(e->daddr, &sin6.sin6_addr, 16);
  }

  bpf_ringbuf_submit(e, 0);

  bpf_map_delete_elem(&pending_connect, &tid);

  return 0;
}

char LICENSE[] SEC("license") = "GPL";
