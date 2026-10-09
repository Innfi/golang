package main

import (
	"bufio"
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type symbol struct {
	addr, size uint64
	name       string
}

type mapping struct {
	start, end, off uint64
	path            string
}

type loadSeg struct{ off, vaddr, filesz uint64 }

type elfInfo struct {
	syms  []symbol
	loads []loadSeg
}

type fileID struct{ dev, ino uint64 }

type Symbolizer struct {
	ksyms []symbol
	procs map[uint32][]mapping
	elfs  map[fileID]*elfInfo
}

func NewSymbolizer() (*Symbolizer, error) {
	ks, err := loadKallsyms()
	if err != nil {
		return nil, err
	}

	return &Symbolizer{
		ksyms: ks,
		procs: map[uint32][]mapping{},
		elfs:  map[fileID]*elfInfo{},
	}, nil
}

func (s *Symbolizer) ResetProcs() { s.procs = map[uint32][]mapping{} }

func (s *Symbolizer) Kernel(addr uint64) string {
	if sym, ok := lookup(s.ksyms, addr); ok {
		return sym.name
	}

	return fmt.Sprintf("0x%x", addr)
}

func (s *Symbolizer) User(pid uint32, addr uint64) string {
	maps, ok := s.procs[pid]

	if !ok {
		maps, _ = readMaps(pid)
		s.procs[pid] = maps
	}

	for _, m := range maps {
		if addr < m.start || addr >= m.end {
			continue
		}

		fileOff := addr - m.start + m.off
		if ei := s.elfFor(pid, m.path); ei != nil {
			for _, seg := range ei.loads {
				if fileOff >= seg.off && fileOff < seg.off+seg.filesz {
					vaddr := fileOff - seg.off + seg.vaddr
					if sym, ok := lookup(ei.syms, vaddr); ok {
						return sym.name
					}

					break
				}
			}
		}

		return fmt.Sprintf("%s+0x%x", filepath.Base(m.path), fileOff)
	}

	return "[unknown]"
}

func (s *Symbolizer) elfFor(pid uint32, path string) *elfInfo {
	full := fmt.Sprintf("/proc/%d/root%s", pid, path)
	st, err := os.Stat(full)
	if err != nil {
		return nil
	}

	sys := st.Sys().(*syscall.Stat_t)
	id := fileID{uint64(sys.Dev), sys.Ino}
	if ei, ok := s.elfs[id]; ok {
		return ei
	}

	ei, _ := loadELF(full)
	s.elfs[id] = ei

	return ei
}

func loadELF(path string) (*elfInfo, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	raw, err := f.Symbols()
	if err != nil || len(raw) == 0 {
		raw, _ = f.DynamicSymbols()
	}

	ei := &elfInfo{}
	for _, sy := range raw {
		if elf.ST_TYPE(sy.Info) == elf.STT_FUNC && sy.Value != 0 {
			ei.syms = append(ei.syms, symbol{addr: sy.Value, size: sy.Size, name: sy.Name})
		}
	}
	sort.Slice(ei.syms, func(i, j int) bool { return ei.syms[i].addr < ei.syms[j].addr })

	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD && p.Flags&elf.PF_X != 0 {
			ei.loads = append(ei.loads, loadSeg{p.Off, p.Vaddr, p.Filesz})
		}
	}

	return ei, nil
}

func readMaps(pid uint32) ([]mapping, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []mapping
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 6 || len(fl[1]) < 3 || fl[1][2] != 'x' || !strings.HasPrefix(fl[5], "/") {
			continue
		}

		rng := strings.SplitN(fl[0], "-", 2)
		start, _ := strconv.ParseUint(rng[0], 16, 64)
		end, _ := strconv.ParseUint(rng[1], 16, 64)
		off, _ := strconv.ParseUint(fl[2], 16, 64)
		out = append(out, mapping{start, end, off, fl[5]})
	}

	return out, sc.Err()
}

func loadKallsyms() ([]symbol, error) {
	f, err := os.Open("/proc/kallsyms")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var syms []symbol
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 3 {
			continue
		}

		switch fl[1] {
		case "t", "T", "w", "W":
		default:
			continue
		}

		addr, err := strconv.ParseUint(fl[0], 16, 64)
		if err != nil || addr == 0 {
			continue
		}

		syms = append(syms, symbol{addr: addr, name: fl[2]})
	}

	if len(syms) == 0 {
		return nil, fmt.Errorf("kallsyms address are hidden")
	}

	sort.Slice(syms, func(i, j int) bool { return syms[i].addr < syms[j].addr })

	return syms, nil
}

func lookup(syms []symbol, target uint64) (symbol, bool) {
	i := sort.Search(len(syms), func(i int) bool { return syms[i].addr > target }) - 1
	if i < 0 {
		return symbol{}, false
	}

	s := syms[i]
	if s.size != 0 && target >= s.addr+s.size {
		return symbol{}, false
	}

	return s, true
}
