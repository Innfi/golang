package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/rlimit"
)

type testCase struct {
	name     string
	why      string
	insns    asm.Instruction
	progType ebpf.ProgramType
}

func cases() []testCase {
	return []testCase{
		{
			name:     "valid baseline",
			why:      "r0 = 0; exit. The verifier accepts this. Everything else is a delta from here.",
			progType: ebpf.SocketFilter,
			insns: asm.Instructions{
				asm.LoadImm(asm.R0, 0, asm.DWord),
				asm.Return(),
			},
		},
		{
			name:     "uninitialised register read",
			why:      "Returns r2, which was never written. BPF has no undefined values: every register must be provably initialised on every path before it is read.",
			progType: ebpf.SocketFilter,
			insns: asm.Instructions{
				asm.Mov.Reg(asm.R0, asm.R2),
				asm.Return(),
			},
		},
		{
			name:     "write to the frame pointer",
			why:      "r10 is the read-only frame pointer. This is the class of error you hit when stack handling in a C helper goes wrong.",
			progType: ebpf.SocketFilter,
			insns: asm.Instructions{
				asm.LoadImm(asm.R0, 0, asm.DWord),
				asm.Mov.Reg(asm.R10, asm.R0),
				asm.Return(),
			},
		},
		{
			name:     "unreachable exit / fallthrough off the end",
			why:      "The last instruction is not an exit, so control falls off the end of the program. Every path must terminate in exit.",
			progType: ebpf.SocketFilter,
			insns: asm.Instructions{
				asm.LoadImm(asm.R0, 0, asm.DWord),
				asm.Mov.Reg(asm.R1, asm.R0),
			},
		},
		{
			name:     "out-of-bounds stack access",
			why:      "Stack is 512 bytes at negative offsets from r10. Reading past it is rejected. In real datapath C this is what an unchecked struct write looks like.",
			progType: ebpf.SocketFilter,
			insns: asm.Instructions{
				asm.LoadMem(asm.R0, asm.R10, -1024, asm.Word),
				asm.Return(),
			},
		},
	}
}

func verifier_main() {
	if err := rlimit.RemoveMemlock(); err != nil {
		fmt.Fprintf(os.Stderr, "remove memlock: %v\n", err)
		os.Exit(1)
	}

	for i, tc := range cases() {
		fmt.Printf("\n%s\n", strings.Repeat("=", 72))
		fmt.Printf("case %d: %s\n", i+1, tc.name)
		fmt.Printf("%s\n", tc.why)
		fmt.Printf("\n%s\n", strings.Repeat("=", 72))

		prog, err := ebpf.NewProgram(&ebpf.ProgramSpec{
			Name:        fmt.Sprintf("verif_%d", i),
			Type:        tc.progType,
			Instuctions: tc.insns,
			License:     "MIT",
		})

		if err == nil {
			fmt.Printf("accepted\n")
			prog.Close()
			continue
		}

		var ve *ebpf.VerifierError

		// %+v
		if errors.As(err, &ve) {
			fmt.Printf("rejected (short form, %%v):\n  %v\n\n", ve)
			fmt.Printf("rejected (full, %%+v):\n  %+v\n\n", ve)
		} else {
			fmt.Printf("failed but not verifier error: %v\n", err)
		}
	}
}
