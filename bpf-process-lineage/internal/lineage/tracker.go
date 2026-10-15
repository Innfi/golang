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

const maxDepth = 32

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
