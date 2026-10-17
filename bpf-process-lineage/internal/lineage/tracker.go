package lineage

import (
	"sync"
	"time"
)

type Proc struct {
	PID      uint32
	PPID     uint32
	Comm     string
	Exe      string
	Args     []string
	UID      uint32
	Execs    int
	Seeded   bool
	ExitedAt time.Time
	ExitCode int
}

type Ancestor struct {
	PID     uint32 `json:"pid"`
	Comm    string `json:"comm,omitempty"`
	Exe     string `json:"exe,omitempty"`
	Exited  bool   `json:"exited,omitempty"`
	Unknown bool   `json:"unknown,omitempty"`
}

type Tracker struct {
	mu     sync.Mutex
	procs  map[uint32]*Proc
	retain time.Duration
	now    func() time.Time
}

const MaxDepth = 32

func NewTracker(retain time.Duration) *Tracker {
	return &Tracker{
		procs:  make(map[uint32]*Proc),
		retain: retain,
		now:    time.Now,
	}
}

func (t *Tracker) Seed(p Proc) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.procs[p.PID]; ok {
		return
	}

	p.Seeded = true
	t.procs[p.PID] = &p
}

func (t *Tracker) Fork(pid, ppid, uid uint32, comm string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	np := &Proc{PID: pid, PPID: ppid, UID: uid, Comm: comm}
	if parent, ok := t.procs[ppid]; ok {
		np.Exe = parent.Exe
		np.Args = parent.Args
	}

	t.procs[pid] = np
}

func (t *Tracker) Exec(pid, ppid, uid uint32, comm, exe string, args []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, ok := t.procs[pid]
	if !ok || !p.ExitedAt.IsZero() {
		p = &Proc{PID: pid, PPID: ppid}
		t.procs[pid] = p
	}

	p.Comm, p.Exe, p.Args, p.UID = comm, exe, args, uid
	p.Execs++
}

func (t *Tracker) Exit(pid uint32, code int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if p, ok := t.procs[pid]; ok {
		p.ExitedAt = t.now() // why call now by tracker function...?
		p.ExitCode = code
	}
}

func (t *Tracker) Touch(pid, ppid, uid uint32, comm string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.procs[pid]; ok && p.ExitedAt.IsZero() {
		return
	}

	t.procs[pid] = &Proc{PID: pid, PPID: ppid, UID: uid, Comm: comm}
}

func (t *Tracker) Get(pid uint32) (Proc, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.procs[pid]
	if !ok {
		return Proc{}, false
	}

	return *p, true
}

func (t *Tracker) Ancestry(pid uint32) []Ancestor {
	t.mu.Lock()
	defer t.mu.Unlock()

	var chain []Ancestor
	seen := map[uint32]bool{pid: true}

	cur, ok := t.procs[pid]
	if !ok {
		return nil
	}

	for depth := 0; depth < MaxDepth; depth++ {
		ppid := cur.PPID
		if ppid == 0 || seen[ppid] {
			break
		}
		seen[ppid] = true

		parent, ok := t.procs[ppid]
		if !ok {
			chain = append(chain, Ancestor{PID: ppid, Unknown: true})
			break
		}

		chain = append(chain, Ancestor{PID: parent.PID, Comm: parent.Comm, Exe: parent.Exe, Exited: !parent.ExitedAt.IsZero()})
		cur = parent
	}

	return chain
}

func (t *Tracker) GC() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	referenced := make(map[uint32]bool)
	for _, p := range t.procs {
		if p.ExitedAt.IsZero() {
			for cur, depth := p, 0; cur != nil && depth < MaxDepth; depth++ {
				referenced[cur.PPID] = true
				cur = t.procs[cur.PPID]
			}
		}
	}

	cutoff := t.now().Add(-t.retain)
	removed := 0
	for pid, p := range t.procs {
		if !p.ExitedAt.IsZero() && p.ExitedAt.Before(cutoff) && !referenced[pid] {
			delete(t.procs, pid)
			removed++
		}
	}

	return removed
}

func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.procs)
}
