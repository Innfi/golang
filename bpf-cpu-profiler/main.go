package main

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpfel -type key_t profile profile.bpf.c -- -I./headers

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"
)

const maxStackDepth = 127

func main() {
	freq := flag.Uint64("freq", 99, "sampling frequency per CPU (Hz)")
	interval := flag.Duration("interval", 10*time.Second, "flush interval")
	pid := flag.Uint("pid", 0, "only profile this tgid (0 = all processes)")
	outDir := flag.String("out", ".", "directory for .folded output")
	flag.Parse()

	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("remove memlock: %v", err)
	}

	spec, err := loadProfile()
	if err != nil {
		log.Fatalf("load spec: %v", err)
	}
	if *pid != 0 {
		if err := spec.Variables["target_pid"].Set(uint32(*pid)); err != nil {
			log.Fatalf("set target_pid: %v", err)
		}
	}

	var objs profileObjects
	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			log.Fatalf("verifier:\n%+v", ve)
		}
		log.Fatalf("load objects: %v", err)
	}
	defer objs.Close()

	fds, err := attachPerfEvents(objs.DoSample, *freq)
	defer func() {
		for _, fd := range fds {
			unix.Close(fd)
		}
	}()
	if err != nil {
		log.Fatalf("attach: %v", err)
	}
	log.Printf("sampling %d CPUs at %d Hz, flushing every %s", len(fds), *freq, *interval)

	sym, err := NewSymbolizer()
	if err != nil {
		log.Fatalf("symbolizer: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case now := <-ticker.C:
			doFlush(&objs, sym, *outDir, now)
		case <-ctx.Done():
			doFlush(&objs, sym, *outDir, time.Now())
			return
		}
	}
}

// attachPerfEvents opens one CPU-clock perf event per CPU in frequency mode
// and attaches the BPF program to each.
func attachPerfEvents(prog *ebpf.Program, freq uint64) ([]int, error) {
	ncpu, err := ebpf.PossibleCPU()
	if err != nil {
		return nil, err
	}
	var fds []int
	for cpu := 0; cpu < ncpu; cpu++ {
		attr := unix.PerfEventAttr{
			// Software CPU clock works everywhere, including EC2 instances
			// without PMU passthrough. PERF_TYPE_HARDWARE/CPU_CYCLES needs a PMU.
			Type:   unix.PERF_TYPE_SOFTWARE,
			Config: unix.PERF_COUNT_SW_CPU_CLOCK,
			Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
			Sample: freq,             // interpreted as Hz because of PerfBitFreq
			Bits:   unix.PerfBitFreq, // frequency mode, not period mode
		}
		fd, err := unix.PerfEventOpen(&attr, -1 /* all tasks */, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
		if err != nil {
			if errors.Is(err, unix.ENODEV) { // offline CPU
				continue
			}
			return fds, fmt.Errorf("perf_event_open cpu %d: %w", cpu, err)
		}
		fds = append(fds, fd)
		if err := unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_SET_BPF, prog.FD()); err != nil {
			return fds, fmt.Errorf("PERF_EVENT_IOC_SET_BPF cpu %d: %w", cpu, err)
		}
		if err := unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_ENABLE, 0); err != nil {
			return fds, fmt.Errorf("PERF_EVENT_IOC_ENABLE cpu %d: %w", cpu, err)
		}
	}
	return fds, nil
}

func doFlush(objs *profileObjects, sym *Symbolizer, dir string, ts time.Time) {
	folded, err := drain(objs, sym)
	if err != nil {
		log.Printf("drain: %v", err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("cpu-%s.folded", ts.UTC().Format("20060102T150405Z")))
	if err := writeFolded(path, folded); err != nil {
		log.Printf("write %s: %v", path, err)
		return
	}
	log.Printf("wrote %d unique stacks to %s", len(folded), path)
}

// drain reads and clears the counts map, resolves stacks, and returns
// folded-stack lines ("comm;root;...;leaf") -> sample count.
func drain(objs *profileObjects, sym *Symbolizer) (map[string]uint64, error) {
	sym.ResetProcs() // re-read /proc/<pid>/maps each window (dlopen, restarts)

	var (
		k    profileKeyT
		v    uint64
		keys []profileKeyT
	)
	it := objs.Counts.Iterate()
	for it.Next(&k, &v) {
		keys = append(keys, k)
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("iterate counts: %w", err)
	}

	folded := make(map[string]uint64, len(keys))
	var buf [maxStackDepth]uint64

	for i := range keys {
		k := keys[i]
		// Atomic read+delete so increments between iterate and delete aren't lost
		// (hash map LookupAndDelete needs kernel >= 5.14).
		if err := objs.Counts.LookupAndDelete(&k, &v); err != nil {
			continue
		}

		frames := []string{commString(k.Comm)}

		if k.UserStackId >= 0 {
			ips := readStack(objs.Stacks, k.UserStackId, &buf)
			for j := len(ips) - 1; j >= 0; j-- { // root first
				frames = append(frames, sym.User(k.Pid, ips[j]))
			}
		}
		if k.KernelStackId >= 0 {
			ips := readStack(objs.Stacks, k.KernelStackId, &buf)
			for j := len(ips) - 1; j >= 0; j-- {
				frames = append(frames, sym.Kernel(ips[j])+"_[k]")
			}
		}
		if len(frames) == 1 {
			frames = append(frames, "[no stack]")
		}
		folded[strings.Join(frames, ";")] += v
	}

	clearStacks(objs.Stacks)
	return folded, nil
}

func readStack(m *ebpf.Map, id int32, buf *[maxStackDepth]uint64) []uint64 {
	*buf = [maxStackDepth]uint64{}
	if err := m.Lookup(uint32(id), buf); err != nil {
		return nil
	}
	n := 0
	for n < maxStackDepth && buf[n] != 0 {
		n++
	}
	return buf[:n]
}

func clearStacks(m *ebpf.Map) {
	var (
		id  uint32
		ids []uint32
	)
	it := m.Iterate()
	var val [maxStackDepth]uint64
	for it.Next(&id, &val) {
		ids = append(ids, id)
	}
	for _, id := range ids {
		_ = m.Delete(id)
	}
}

func writeFolded(path string, folded map[string]uint64) error {
	lines := make([]string, 0, len(folded))
	for stack := range folded {
		lines = append(lines, stack)
	}
	sort.Strings(lines)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, s := range lines {
		fmt.Fprintf(w, "%s %d\n", s, folded[s])
	}
	return w.Flush()
}

// bpf2go maps `char comm[16]` to [16]int8.
func commString(c [16]int8) string {
	b := make([]byte, 0, len(c))
	for _, ch := range c {
		if ch == 0 {
			break
		}
		b = append(b, byte(ch))
	}
	return string(b)
}
