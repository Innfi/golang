package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64 -cflags "-O2 -g -Wall -Wno-missing-declarations" -type event bpf tls.bpf.c

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

	// OpenExecutable itself does not attach anything
	ex, err := link.OpenExecutable(libPath)
	if err != nil {
		log.Fatalf("open %s: %v", libPath, err)
	}

	w, err := ex.Uprobe("SSL_write", objs.ProbeSslWrite, nil)
	if err != nil {
		log.Fatalf("uprobe SSL_write: %v", err)
	}
	defer w.Close()

	re, err := ex.Uprobe("SSL_read", objs.ProbeSslReadEnter, nil)
	if err != nil {
		log.Fatalf("uprobe SSL_read: %v", err)
	}
	defer re.Close()

	rx, err := ex.Uretprobe("SSL_read", objs.ProbeSslReadExit, nil)
	if err != nil {
		log.Fatalf("uretprobe SSL_read: %v", err)
	}
	defer rx.Close()

	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		log.Fatalf("ringbuf: %v", err)
	}
	defer rd.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() { <-stop; rd.Close() }()

	log.Printf("sniffing %s - ctrl-c to stop", libPath)

	var event bpfEvent
	for {
		rec, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			log.Printf("read: %v", err)
			continue
		}

		if err := binary.Read(bytes.NewBuffer(rec.RawSample), binary.LittleEndian, &event); err != nil {
			log.Printf("parse: %v", err)
			continue
		}

		dir := "WRITE ->"
		if event.IsRead == 1 {
			dir = "READ <-"
		}

		n := event.DataLen
		if n > uint32(len(event.Data)) {
			n = uint32(len(event.Data))
		}

		fmt.Printf("[%s] pid=%d comm=%s len=%d\n%s\n",
			dir, event.Pid, unix.ByteSliceToString(event.Comm[:]),
			event.Len, event.Data[:n])
	}
}
