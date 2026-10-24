//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -tags linux ctfw bpf/conntrack.c -- -I/usr/include -Ibpf

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"
)

const pinDir = "/sys/fs/bpf/ctfw"

var statNames = []string{
	"egress packets seen",
	"ingress allowed (conntrack hit)",
	"ingress allowed (policy)",
	"ingress DROPPED",
}

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
	case "dump":
		err = dump()
	case "stats":
		err = showStats()
	case "allow":
		if len(os.Args) != 3 {
			usage()
		}
		err = setPolicy(os.Args[2], true)
	case "deny":
		if len(os.Args) != 3 {
			usage()
		}
		err = setPolicy(os.Args[2], false)
	default:
		usage()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(1)
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

// run
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

	var ev ctfwDropEvent
	for {
		rec, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				fmt.Println("\ndetaching")

				return nil
			}

			fmt.Fprintf(os.Stderr, "read ringbuf: %v\n", err)
			continue
		}

		if err := binary.Read(bytes.NewReader(rec.RawSample), binary.NativeEndian, &ev); err != nil {
			fmt.Fprintf(os.Stderr, "parse event: %v\n", err)
			continue
		}

		fmt.Printf("%s DROP %s -> %s %s\n",
			time.Now().Format("15:04:05.000"),
			endpoint(ev.Saddr, ev.Sport),
			endpoint(ev.Daddr, ev.Dport),
			protoName(ev.Proto),
		)
	}
}

func endpoint(addr uint32, port uint16) string {
	p := ntohs(port)
	if p == 0 {
		return ipv4(addr).String()
	}

	return fmt.Sprintf("%s:%d", ipv4(addr), p)
}

func ntohs(beValue uint16) uint16 {
	var b [2]byte
	binary.NativeEndian.PutUint16(b[:], beValue)

	return binary.BigEndian.Uint16(b[:])
}

func ipv4(beValue uint32) netip.Addr {
	var b [4]byte
	binary.NativeEndian.PutUint32(b[:], beValue)

	return netip.AddrFrom4(b)
}

func protoName(p uint8) string {
	switch p {
	case 1:
		return "ICMP"
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	default:
		return fmt.Sprintf("proto=%d", p)
	}
}

// dump
func dump() error {
	m, err := ebpf.LoadPinnedMap(filepath.Join(pinDir, "ct_map"), nil)
	if err != nil {
		return fmt.Errorf("open pinned ct_map (is ctfw run active?): %w", err)
	}
	defer m.Close()

	fmt.Printf("%-46s %-12s %8s %8s %10s\n",
		"FLOW", "STATE", "PKTS-FWD", "PKTS-REV", "AGE")

	var (
		key   ctfwCtKey
		entry ctfwCtEntry
		n     int
	)
	iter := m.Iterate()
	for iter.Next(&key, &entry) {
		n++
		flow := fmt.Sprintf("%s -> %s %s",
			endpoint(key.Saddr, key.Sport),
			endpoint(key.Daddr, key.Dport),
			protoName(key.Proto),
		)

		state := "NEW"
		if entry.State == 1 {
			state = "ESTABLISHED"
		}

		fmt.Printf("%-46s %-12s %8d %8d %10s\n",
			flow, state, entry.PktsFwd, entry.PktsRev,
			ktimeAge(entry.LastSeenNs).Truncate(time.Millisecond))
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("iterate ct_map: %w", err)
	}
	if n == 0 {
		fmt.Println("empty")
	}

	return nil
}

func ktimeAge(ns uint64) time.Duration {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0
	}

	now := uint64(ts.Sec)*1e9 + uint64(ts.Nsec)
	if now < ns {
		return 0
	}

	return time.Duration(now - ns)
}

// showStats
func showStats() error {
	m, err := ebpf.LoadPinnedMap(filepath.Join(pinDir, "stats"), nil)
	if err != nil {
		return fmt.Errorf("open pinned stats: %w", err)
	}
	defer m.Close()

	for i, name := range statNames {
		var v uint64
		if err := m.Lookup(uint32(i), &v); err != nil {
			return fmt.Errorf("lookup stats[%d]: %w", i, err)
		}
		fmt.Printf("%-23s %d\n", name, v)
	}

	return nil
}

// setPolicy
func setPolicy(ipStr string, allow bool) error {
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return fmt.Errorf("parse ip: %w", err)
	}
	if !addr.Is4() {
		return fmt.Errorf("only IPv4 is supported")
	}

	m, err := ebpf.LoadPinnedMap(filepath.Join(pinDir, "allow_src"), nil)
	if err != nil {
		return fmt.Errorf("open pinned allow_src (is `ctfw run` active?): %w", err)
	}
	defer m.Close()

	b := addr.As4()
	key := binary.NativeEndian.Uint32(b[:])

	if allow {
		if err := m.Put(key, uint8(1)); err != nil {
			return fmt.Errorf("add to allow list: %w", err)
		}
		fmt.Printf("allowing unsolicited inbound from %s\n", addr)
	} else {
		if err := m.Delete(key); err != nil {
			if errors.Is(err, ebpf.ErrKeyNotExist) {
				fmt.Printf("%s was not in the allow list\n", addr)

				return nil
			}

			return fmt.Errorf("remove from allow list: %w", err)
		}
		fmt.Printf("removed %s from the allow list\n", addr)
	}

	return nil
}
