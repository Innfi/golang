#include "vmlinux.h"
#include <bpf/bpf_headers.h>

#define MAX_STACK_DEPTH 127
#define TASK_COMM_LEN 16

struct key_t {
  u32 pid;
  s32 user_stack_id;
  s32 kernel_stack_id;
  char comm[TASK_COMM_LEN];
};

struct {
  __uint(type, BPF_MAP_TYPE_STACK_TRACE);
  __uint(max_entries, 16384);
  __uint(key_size, sizeof(u32));
  __uint(value_size, MAX_STACK_DEPTH*sizeof(u64));
} stacks SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 40960);
  __type(key, struct key_t);
  __type(value, u64);
} counts SEC(".maps");

const volatile u32 target_pid = 0;

SEC("perf_event")
int do_sample(struct bpf_perf_event_data *ctx) {
  u64 id = bpf_get_current_pid_tgid();
  u32 tgid = id >> 32;
  u32 tid = (u32)id;

  if (tid == 0) return 0;
  if (target_pid && tgid != target_pid) return 0;

  struct key_t key = {};
  key.pid = tgid;
  bpf_get_current_comm(&key.comm, sizeof(key.comm));
  key.kernel_stack_id = bpf_get_stackid(ctx, &stacks, 0);
  key.user_stack_id = bpf_get_stackid(ctx, &stacks, BPF_F_USER_STACK);

  u64 *val = bpf_map_lookup_elem(&counts, &key);
  if (val) {
    __sync_fetch_and_add(val, 1);
  } else {
    u64 one = 1;

    if (bpf_map_update_elem(&counts, &key, &one, BPF_NOEXIST)) {
      val = bpf_map_lookup_elem(&counts, &key);

      if (val) __sync_fetch_and_add(val, 1);
    }
  }

  return 0;
}

char LICENSE[] SEC("license") = "GPL";
