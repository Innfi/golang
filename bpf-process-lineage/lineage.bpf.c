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
