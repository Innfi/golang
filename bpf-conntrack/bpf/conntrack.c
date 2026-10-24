//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

char __license[] SEC("license") = "GPL";

#ifndef TC_ACT_OK
#define TC_ACT_OK 0
#endif
#ifndef TC_ACT_SHOT
#define TC_ACT_SHOT 2
#endif

#define ETH_P_IP 0x0800

#define CT_NEW 0
#define CT_ESTABLISHED 1

#define ST_EGRESS 0
#define ST_IN_CT_HIT 1
#define ST_IN_POLICY_HIT 2
#define ST_IN_DROP 3

struct ct_key {
  __be32 saddr;
  __be32 daddr;
  __be16 sport;
  __be16 dport;
  __u8 proto;
  __u8 pad[3];
};

struct ct_entry {
  __u64 created_ns;
  __u64 last_seen_ns;
  __u64 pkts_fwd;
  __u64 pkts_rev;
  __u8 state;
  __u8 pad[7];
};

struct drop_event {
  __be32 saddr;
  __be32 daddr;
  __be16 sport;
  __be16 dport;
  __u8 proto;
  __u8 pad[3];
};

struct {
  __uint(type, BPF_MAP_TYPE_LRU_HASH);
  __type(key, struct ct_key);
  __type(value, struct ct_entry);
  __uint(max_entries, 65536);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} ct_map SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __type(key, __be32);
  __type(value, __u8);
  __uint(max_entries, 1024);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} allow_src SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 4);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} stats SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 1 << 16);
  __type(value, struct drop_event)
} drops SEC(".maps");

struct tuple {
  __be32 saddr;
  __be32 daddr;
  __be16 sport;
  __be16 dport;
  __u8 proto;
};

static __always_inline void bump(__u32 idx) {
  __u64 *v = bpf_map_lookup_elem(&stats, &idx);
  if (v) __sync_fetch_and_add(v, 1);
}

static __always_inline int parse_tuple(struct __sk_buff *skb, struct tuple *t) {
  void *data = (void *)(long)skb->data;
  void *data_end = (void *)(long)skb->data_end;

  struct ethhdr *eth = data;
  if ((void *)(eth + 1) > data_end) return -1;

  if (eth->h_proto != bpf_htons(ETH_P_IP)) return -1;

  struct iphdr *ip = (void *)(eth+1);
  if ((void *)(ip+1) > data_end) return -1;

  __u32 ihl = ip->ihl;
  if (ihl < 5) return -1;

  void *l4 = (void *)ip + ihl * 4;
  if (l4 > data_end) return -1;

  t->saddr = ip->saddr;
  t->daddr = ip->daddr;
  t->proto = ip->protocol;
  t->sport = 0;
  t->dport = 0;

  if (ip->protocol == IPPROTO_TCP) {
    struct tcphdr *tcp = l4;
    if ((void *)(tcp + 1) > data_end) return -1;

    t->sport = tcp->source;
    t->dport = tcp->dest;
  } else if (ip->protocol == IPPROTO_UDP) {
    struct udphdr *udp = l4;
    if ((void *)(udp + 1) > data_end) return -1;

    t->sport = udp->source;
    t->dport = udp->dest;
  }

  return 0;
}

SEC("tc")
int egress_prog(struct __sk_buff *skb) {
  struct tuple t;
  if (parse_tuple(skb, &t) < 0) return TC_ACT_OK;

  bump(ST_EGRESS);

  struct ct_key key = {};
  key.saddr = t.saddr;
  key.daddr = t.daddr;
  key.sport = t.sport;
  key.dport = t.dport;
  key.proto = t.proto;

  __u64 now = bpf_ktime_get_ns();

  struct ct_entry *e = bpf_map_lookup_elem(&ct_map, &key);
  if (e) {
    __sync_fetch_and_add(&e->pkts_fwd, 1);
    e->last_seen_ns = now;
  } else {
    struct ct_entry new_entry = {};
    new_entry.created_ns = now;
    new_entry.last_seen_ns = now;
    new_entry.pkts_fwd = 1;
    new_entry.state = CT_NEW;

    bpf_map_update_elem(&ct_map, &key, &new_entry, BPF_ANY);
  }

  return TC_ACT_OK;
}

SEC("tc")
int ingress_prog(struct __sk_buff *skb) {
  struct tuple t;
  if (parse_tuple(skb, &t) < 0) return TC_ACT_OK;

  struct ct_key key = {};
  key.saddr = t.daddr;
  key.daddr = t.saddr;
  key.sport = t.dport;
  key.dport = t.sport;
  key.proto = t.proto;

  struct ct_entry *e = bpf_map_lookup_elem(&ct_map, &key);
  if (e) {
    __sync_fetch_and_add(&e->pkts_rev, 1);
    e->last_seen_ns = bpf_ktime_get_ns();
    e->state = CT_ESTABLISHED;
    bump(ST_IN_CT_HIT);

    return TC_ACT_OK;
  }

  __u8 *allowed = bpf_map_lookup_elem(&allow_src, &t.saddr);
  if (allowed) {
    bump(ST_IN_POLICY_HIT);

    return TC_ACT_OK;
  }

  struct drop_event *ev = bpf_ringbuf_reserve(&drops, sizeof(*ev), 0);
  if (ev) {
    ev->saddr = t.saddr;
    ev->daddr = t.daddr;
    ev->sport = t.sport;
    ev->dport = t.dport;
    ev->proto = t.proto;
    ev->pad[0] = 0;
    ev->pad[1] = 0;
    ev->pad[2] = 0;
    bpf_ringbuf_submit(ev, 0);
  }

  bump(ST_IN_DROP);

  return TC_ACT_SHOT;
}
