package main

//go:generate go tool bpf2go -tags linux -target amd64 -type event lineage lineage.bpf.c -- -I./headers -O2 -g -Wall

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"

	"proclineage/internal/lineage"
)

const (
	evFork    = 1
	evExec    = 2
	evExit    = 3
	evOpen    = 4
	evConnect = 5
)

var evNames = map[uint32]string{
	evFork: "fork", evExec: "exec", evExit: "exit", evOpen: "open", evConnect: "connect",
}

type Record struct {
	Time     time.Time          `json:"time"`
	Event    string             `json:"event"`
	PID      uint32             `json:"pid"`
	PPID     uint32             `json:"ppid"`
	UID      uint32             `json:"uid"`
	Comm     string             `json:"comm"`
	Exe      string             `json:"exe,omitempty"`
	Args     []string           `json:"args,omitempty"`
	Path     string             `json:"path,omitempty"`
	Flags    string             `json:"flags,omitempty"`
	Dst      string             `json:"dst,omitempty"`
	Ret      int32              `json:"ret"`
	CgroupID uint64             `json:"cgroup_id"`
	Ancestry []lineage.Ancestor `json:"ancestry"`
}

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

	tracker := lineage.NewTracker(*retain)
	n, err := lineage.SeedFromProc(tracker, "/proc")
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	log.Printf("seeded %d processes, tracing (format=%s opens=%v)", n, *format, *opens)

	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		log.Fatalf("ringbuf: %v", err)
	}
	defer rd.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		rd.Close()
	}()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tracker.GC()
			}
		}
	}()

	bootEpoch := bootTimeEpoch()
	out := json.NewEncoder(os.Stdout)
	var ev lineageEvent

	for {
		rec, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}

			log.Printf("ringbuf read: %v", err)
			continue
		}

		if err := binary.Read(bytes.NewReader(rec.RawSample), binary.LittleEndian, &ev); err != nil {
			log.Printf("decode: %v", err)
			continue
		}

		r, emit := handle(tracker, &ev, bootEpoch, *skipPseudo, *showForkExit)
		if !emit {
			continue
		}

		if *format == "text" {
			fmt.Println(formatText(r))
		} else {
			_ = out.Encode(r)
		}
	}
}

func attach(objs *lineageObjects, opens bool) ([]link.Link, error) {
	var links []link.Link
	add := func(l link.Link, err error) error {
		if err != nil {
			return err
		}

		links = append(links, l)

		return nil
	}

	for _, p := range []*ebpf.Program{objs.HandleFork, objs.HandleExec, objs.HandleExit} {
		if err := add(link.AttachTracing(link.TracingOptions{Program: p})); err != nil {
			return links, fmt.Errorf("%s: %w", p, err)
		}
	}

	type tracepoint struct {
		name string
		prog *ebpf.Program
	}
	tps := []tracepoint{
		{"sys_enter_connect", objs.HandleConnectEnter},
		{"sys_exit_connect", objs.HandleConnectExit},
	}
	if opens {
		tps = append(tps,
			tracepoint{"sys_enter_openat", objs.HandleOpenatEnter},
			tracepoint{"sys_exit_openat", objs.HandleOpenatExit},
		)
	}

	for _, tp := range tps {
		if err := add(link.Tracepoint("syscalls", tp.name, tp.prog, nil)); err != nil {
			return links, fmt.Errorf("%s: %w", tp.name, err)
		}
	}

	return links, nil
}

func handle(t *lineage.Tracker, ev *lineageEvent, bootEpoch time.Time,
	skipPseudo, lifecycle bool) (Record, bool) {
	comm := cstr(ev.Comm[:])

	switch ev.Type {
	case evFork:
		t.Fork(ev.Pid, ev.Ppid, ev.Uid, comm)
		if !lifecycle {
			return Record{}, false
		}
	case evExec:
		args := lineage.SplitArgs(i8bytes(ev.Args[:ev.ArgsLen]))
		t.Exec(ev.Pid, ev.Ppid, ev.Uid, comm, cstr(ev.Path[:]), args)
	case evExit:
		r := baseRecord(t, ev, comm, bootEpoch)
		t.Exit(ev.Pid, int(ev.Ret))

		return r, lifecycle
	case evOpen:
		path := cstr(ev.Path[:])
		if skipPseudo && isPseudoFS(path) {
			return Record{}, false
		}
		t.Touch(ev.Pid, ev.Ppid, ev.Uid, comm)
	case evConnect:
		t.Touch(ev.Pid, ev.Ppid, ev.Uid, comm)
	default:
		return Record{}, false
	}

	r := baseRecord(t, ev, comm, bootEpoch)
	switch ev.Type {
	case evOpen:
		r.Path = cstr(ev.Path[:])
		r.Flags = openFlags(ev.Flags)
	case evConnect:
		r.Dst = dst(ev)
	}

	return r, true
}

func i8bytes(s []int8) []byte {
	b := make([]byte, len(s))
	for i, c := range s {
		b[i] = byte(c)
	}

	return b
}

func cstr(s []int8) string {
	b := i8bytes(s)
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}

	return string(b)
}

func baseRecord(t *lineage.Tracker, ev *lineageEvent, comm string, bootEpoch time.Time) Record {
	r := Record{
		Time:     bootEpoch.Add(time.Duration(ev.TsNs)),
		Event:    evNames[ev.Type],
		PID:      ev.Pid,
		PPID:     ev.Ppid,
		UID:      ev.Uid,
		Comm:     comm,
		Ret:      ev.Ret,
		CgroupID: ev.CgroupId,
		Ancestry: t.Ancestry(ev.Pid),
	}
	if p, ok := t.Get(ev.Pid); ok {
		r.Exe, r.Args = p.Exe, p.Args
		r.PPID = p.PPID
	}

	return r
}

func isPseudoFS(p string) bool {
	return strings.HasPrefix(p, "/proc/") || strings.HasPrefix(p, "/sys/") || strings.HasPrefix(p, "/dev/")
}

func dst(ev *lineageEvent) string {
	var addr netip.Addr
	if ev.Family == unix.AF_INET {
		addr = netip.AddrFrom4([4]byte(ev.Daddr[:4]))
	} else {
		addr = netip.AddrFrom16(ev.Daddr).Unmap()
	}
	return netip.AddrPortFrom(addr, ev.Dport).String()
}

func openFlags(f uint32) string {
	var parts []string
	switch f & unix.O_ACCMODE {
	case unix.O_RDONLY:
		parts = append(parts, "RDONLY")
	case unix.O_WRONLY:
		parts = append(parts, "WRONLY")
	case unix.O_RDWR:
		parts = append(parts, "RDWR")
	}
	for _, x := range []struct {
		bit  uint32
		name string
	}{{unix.O_CREAT, "CREAT"}, {unix.O_TRUNC, "TRUNC"}, {unix.O_APPEND, "APPEND"}} {
		if f&x.bit != 0 {
			parts = append(parts, x.name)
		}
	}
	return strings.Join(parts, "|")
}

func bootTimeEpoch() time.Time {
	var ts unix.Timespec
	_ = unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts)

	return time.Now().Add(-time.Duration(ts.Nano()))
}

func formatText(r Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-7s %s[%d]", r.Time.Format("15:04:05.000"), r.Event, r.Comm, r.PID)
	switch r.Event {
	case "exec":
		fmt.Fprintf(&b, " %s", strings.Join(r.Args, " "))
	case "open":
		fmt.Fprintf(&b, " %s (%s)", r.Path, r.Flags)
	case "connect":
		fmt.Fprintf(&b, " %s", r.Dst)
	}
	fmt.Fprintf(&b, " ret=%d", r.Ret)
	for _, a := range r.Ancestry {
		switch {
		case a.Unknown:
			fmt.Fprintf(&b, " <- ?[%d]", a.PID)
		case a.Exited:
			fmt.Fprintf(&b, " <- %s[%d]†", a.Comm, a.PID)
		default:
			fmt.Fprintf(&b, " <- %s[%d]", a.Comm, a.PID)
		}
	}
	return b.String()
}
