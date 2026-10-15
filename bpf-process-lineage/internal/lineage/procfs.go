package lineage

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func SeedFromProc(t *Tracker, root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	n := 0

	for _, e := range entries {
		pid, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil {
			continue
		}
		p, ok := readProc(filepath.Join(root, e.Name()), uint32(pid))
		if !ok {
			continue
		}
		t.Seed(p)
		n++
	}

	return n, nil
}

func readProc(dir string, pid uint32) (Proc, bool) {
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return Proc{}, false
	}

	comm, ppid, ok := parseStat(stat)
	if !ok {
		return Proc{}, false
	}

	p := Proc{PID: pid, PPID: ppid, Comm: comm}
	if exe, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
		p.Exe = exe
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		p.Args = SplitArgs(raw)
	}

	return p, true
}

func parseStat(b []byte) (comm string, ppid uint32, ok bool) {
	open := bytes.IndexByte(b, '(')
	closeIdx := bytes.LastIndexByte(b, ')')
	if open < 0 || closeIdx < open {
		return "", 0, false
	}

	comm = string(b[open+1 : closeIdx])
	fields := strings.Fields(string(b[closeIdx+1:]))
	if len(fields) < 2 {
		return "", 0, false
	}

	v, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return "", 0, false
	}

	return comm, uint32(v), true
}

func SplitArgs(raw []byte) []string {
	raw = bytes.TrimRight(raw, "\x00")
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte{0})
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = string(p)
	}

	return out
}
