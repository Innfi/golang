//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -tags linux ctfw bpf/conntrack.c -- -I/usr/include -Ibpf

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

const pinDir = "/sys/fs/bpf/ctfw"

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	var err error
	switch os.Args[1] {
	case "run":
		if len(os.Args) != 3 {
			usage()
		}
		err = run(os.Args[2])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  ctfw run <iface>     attach to iface and stream drop events
  ctfw dump            print the conntrack table
  ctfw stats           print counters
  ctfw allow <ip>      permit unsolicited inbound from ip
  ctfw deny <ip>       remove ip from the allow list
`)
	os.Exit(2)
}

func run(ifaceName string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("lookup iface %q: %w", ifaceName, err)
	}

	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("remove memlock: %w", err)
	}

	if err := os.MkdirAll(pinDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", pinDir, err)
	}

	var objs ctfwObjects
	err = loadCtfwObjects(&objs, &ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{PinPath: pinDir},
	})
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			return fmt.Errorf("verifier rejected program:\n%+v", ve)
		}

		return fmt.Errorf("load objects: %w", err)
	}
	defer objs.Close()

	egressLink, err := link.AttachTCX(link.TCXOptions{
		Interface: iface.Index,
		Program:   objs.EgressProg,
		Attach:    ebpf.AttachTCXEgress,
	})
	if err != nil {
		return fmt.Errorf("attach egress (kernel 6.6+ required for TCX): %w", err)
	}
	defer egressLink.Close()

	ingressLink, err := link.AttachTCX(link.TCXOptions{
		Interface: iface.Index,
		Program:   objs.IngressProg,
		Attach:    ebpf.AttachTCXIngress,
	})
	if err != nil {
		return fmt.Errorf("attach ingress: %w", err)
	}
	defer ingressLink.Close()

	rd, err := ringbuf.NewReader(objs.Drops)
	if err != nil {
		return fmt.Errorf("open ringbuf: %w", err)
	}
	defer rd.Close()

	fmt.Printf("attached to %s (ingress + egress)\n", ifaceName)
	fmt.Printf("maps pinned under %s\n", pinDir)
	fmt.Printf("waiting for dropped packets - ctrl c to detach\n\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		rd.Close()
	}()

	return nil
}
