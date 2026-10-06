package main

import (
	"flag"
	"log"
	"time"

	"github.com/cilium/ebpf/rlimit"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpfel -type key_t profile profile.bpf.c -- -I./headers

const maxStackDepth = 127

func main() {
	freq := flag.Uint64("freq", 99, "sampling frequency per CPU (hz)")
	interval := flag.Duration("interval", 10*time.Second, "flush interval")
	pid := flag.Uint("pid", 0, "only profile this tgid (0 = all processes")
	outDir := flag.String("out", ".", "directory for .folded output")
	flag.Parse()

	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("remove memlock: %v", err)
	}

	spec, err := loadProfile()
	if err != nil {
		log.Fatalf("loadSpec: %v", err)
	}
}
