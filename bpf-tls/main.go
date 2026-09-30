package main

import (
	"log"
	"os"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" -type event bpf tls.bpf.c

func main() {
	libPath := "/usr/lib/x86_64-linux-gnu/libssl.so.3"
	if len(os.Args) > 1 {
		libPath = os.Args[1]
	}

	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatal(err)
	}

	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		log.Fatalf("load objects: %v", err)
	}

	defer objs.Close()

	ex, err := link.OpenExecutable(libPath)
	if err != nil {
		log.Fatalf("open %s: %v", libPath, err)
	}

	ex.Uprobe("SSL_write", objs.ProbeSslWrite, nil)
}
