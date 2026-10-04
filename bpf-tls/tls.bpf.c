//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#define MAX_DATA_SIZE 4096

char __license[] SEC("license") = "GPL";

struct event {
  __u32 pid;
  __u32 tid;
  __u32 len;
  __u32 data_len;
  __u8 is_read;
  __u8 comm[16];
  __u8 data[MAX_DATA_SIZE];
};

struct event *unused_event __attribute__((unused));

struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 1 << 24); // 16MiB
} events SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 10240);
  __type(key, __u64);
  __type(value, __u64);
} read_bufs SEC("maps");

const volatile __u32 target_pid = 0;

static __always_inline int emit(void *buf, int num, __u8 is_read) {
  if (num <= 0) return 0;

  __u64 id = bpf_get_current_pid_tgid();
  __u32 pid = id >> 32;

  if (target_pid && pid != target_pid) return 0;

  struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
  if (!e) return 0;

  e->pid = pid;
  e->tid = (__u32)id;
  e->len = num;
  e->is_read = is_read;
  bpf_get_current_comm(&e->comm, sizeof(e->comm));

  __u32 size = (__u32)num;
  if (size > MAX_DATA_SIZE) size = MAX_DATA_SIZE;

  e->data_len = size;

  if (bpf_probe_read_user(&e->data, size, buf) != 0) {
    bpf_ringbuf_discard(e, 0);
    return 0;
  }

  bpf_ringbuf_submit(e, 0);
  return 0;
}

SEC("uprobe/SSL_write")
int BPF_UPROBE(probe_ssl_write, void *ssl, const void *buf, int num) {
  return emit((void *)buf, num, 0);
}

SEC("uprobe/SSL_read")
int BPF_UPROBE(probe_ssl_read_enter, void *ssl, void *buf, int num) {
  __u64 id = bpf_get_current_pid_tgid();
  __u64 p = (__u64)buf;

  bpf_map_update_elem(&read_bufs, &id, &p, BPF_ANY);

  return 0;
}

SEC("uretprobe/SSL_read")
int BPF_URETPROBE(probe_ssl_read_exit, int ret) {
  __u64 id = bpf_get_current_pid_tgid();
  __u64 *p = bpf_map_lookup_elem(&read_bufs, &id);
  if (!p) return 0;

  void *buf = (void *)*p;

  bpf_map_delete_elem(&read_bufs, &id);

  return emit(buf, ret, 1);
}
