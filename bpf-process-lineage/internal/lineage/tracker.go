package lineage

import "time"

type Proc struct {
	PID uint32
	PPID uint32
	Comm string
	Exe string
	Args []string
	UID = uint32
	Execs int
	Seeded bool
	ExitedAt time.Time
	ExitCode int
}

func NewTracker(retain time.Duration) *Tracker {
	return &Tracker{
		procs: make(map[uint32]*Proc),
		retain: retain,
		now: time.Now,
	}
}
