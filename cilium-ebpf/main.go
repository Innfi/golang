package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/rlimit"
)

const bpffs = "/sys/fs/bpf"

func main() {
	pin := flag.Bool("pin", false, "pin the program to bpffs so it outlives this process")
	flag.Parse()

	if err := rlimit.RemoveMemlock(); err != nil {
		fmt.Fprintf(os.Stderr, "remove memlock: %v\n", err)
		os.Exit(1)
	}

	spec := &ebpf.ProgramSpec{
		Name: "lifecycle_demo",
		Type: ebpf.SocketFilter,
		Instruction: asm.Instructions{
			asm.LoadImm(asm.R0, 0, asm.DWord),
			asm.Return(),
		},
		License: "MIT",
	}

	prog, err := ebpf.NewProgram(spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load program: %v\n", err)
		os.Exit(1)
	}
	defer prog.Close()

	info, err := prog.Info()
	if err != nil {
		fmt.Fprintf(os.Stderr, "program info: %v\n", err)
		os.Exit(1)
	}
	id, _ := info.ID()

	fmt.Printf("loaded program: %q\n", info.Name)
	fmt.Printf("kernel id: %d\n", id)
	fmt.Printf("type: %s\n", info.Type)
	fmt.Printf("instructions: %d\n", len(spec.Instructions))
	fmt.Printf("\ninspect it with:\n sudo bpftool prog show id %d\n", id)

	if *pin {
		path := filepath.Join(bpffs, spec.Name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "remove stale pin: %v\n", err)
			os.Exit(1)
		}

		if err := prog.Pin(path); err != nil {
			fmt.Fprintf(os.Stderr, "pind: %v (is %s mounted?)\n", err, bpffs)
			os.Exit(1)
		}

		fmt.Printf("\npinned at %s - this program will survive process exit\n", path)
		fmt.Printf("to remove: sudo rm %s\n", path)
	} else {
		fmt.Printf("\nnot pinned - this program dies with this process exits\n")
	}

	fmt.Printf("\nholding the fd open. ctrl-c to exit\n")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
}
