//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -tags linux ctfw bpf/conntrack.c -- -I/usr/include -Ibpf

package main

import (
	"fmt"
	"net"
	"os"

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

	return nil
}
