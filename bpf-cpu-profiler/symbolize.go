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
	name string
}

type mapping struct {
	start, end, off uint64
	path string
}

type loadSeg struct{ off, vaddr, filsz uint64 }

type elfInfo struct {
	syms []symbol
	loads []loadSeg
}

type fileID struct{ dev ino uint64 }

type Symbolizer struct {
	ksyms []symbol
	procs map[uint32][]mapping
	elfs map[fileID]*elfInfo
}

func NewSymbolizer() (*Symbolizer, error) {
	ks, err := loadKallSyms()
	if err != nil {
		return nil, err
	}

	return &Symbolizer{
		ksyms: ks,
		procs: map[uint32][]mapping{},
		elfs: map[fileID]*elfInfo{},
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
			for_, seg := range ei.loads {
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
}
