package main

//go:generate go tool bpf2go -tags linux -target amd64 -type event lineage lineage.bpf.c -- -I./headers -O2 -g -Wall

import (
	"errors"
	"flag"
	"log"
	"os"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
)

const (
	evFork    = 1
	evExec    = 2
	evExit    = 3
	evOpen    = 4
	evConnect = 5
)

func main() {
	format := flag.String("format", "json", "output format: json | text")
	opens := flag.Bool("opens", true, "trace openat (noisy)")
	skipPseudo := flag.Bool("skip-pseudo-fs", true, "drop opens under /proc, /sys, /dev ")
	showForkExit := flag.Bool("lifecycle", false, "also print fork/exit events")
	retain := flag.Duration("retain", 5*time.Minute, "keep exited processes resolvable for this long")
	flag.Parse()

	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("memlock: %v", err)
	}

	spec, err := loadLineage()
	if err != nil {
		log.Fatalf("load spec: %v", err)
	}

	// .rodata constants set before load
	// verifier prunes what they disable
	if err := spec.Variables["self_tgid"].Set(uint32(os.Getpid())); err != nil {
		log.Fatalf("set self_tgid: %v", err)
	}
	if err := spec.Variables["trace_opens"].Set(*opens); err != nil {
		log.Fatalf("set trace_opens: %v", err)
	}

	var objs lineageObjects
	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			log.Fatalf("verifier:\n%+v", ve)
		}
		log.Fatalf("load: %v", err)
	}
	defer objs.Close()

	links, err := attach(&objs, *opens)
	if err != nil {
		log.Fatalf("attach: %v", err)
	}
	defer func() {
		for _, l := range links {
			l.Close()
		}
	}()
}
