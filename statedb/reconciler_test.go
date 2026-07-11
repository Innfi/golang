package bumblebee_test

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"iter"

	"github.com/cilium/hive"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/hivetest"
	"github.com/cilium/hive/job"
	"github.com/cilium/statedb"
	"github.com/cilium/statedb/index"
	"github.com/cilium/statedb/reconciler"
	"github.com/stretchr/testify/require"
)

type RBackend struct {
	Name   string
	Port   uint16
	Status reconciler.Status
}

func (b *RBackend) Clone() *RBackend {
	b2 := *b
	return &b2
}

func (b *RBackend) TableHeader() []string { return []string{"Name", "Port", "Status"} }
func (b *RBackend) TableRow() []string {
	return []string{b.Name, strconv.Itoa(int(b.Port)), b.Status.String()}
}

func (b *RBackend) getStatus() reconciler.Status { return b.Status }

func setStatus(b *RBackend, s reconciler.Status) *RBackend {
	b.Status = s
	return b
}

func getStatus(b *RBackend) reconciler.Status { return b.getStatus() }

var RBackendNameIndex = statedb.Index[*RBackend, string]{
	Name: "name",
	FromObject: func(b *RBackend) index.KeySet {
		return index.NewKeySet(index.String(b.Name))
	},
	FromKey:    index.String,
	FromString: index.FromString,
	Unique:     true,
}

type mockTarget struct {
	mu                       sync.Mutex
	realized                 map[string]uint16
	faulty                   bool
	updates, deletes, prunes int
}

func newMockTarget() *mockTarget {
	return &mockTarget{realized: map[string]uint16{}}
}

func (m *mockTarget) setFaulty(f bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.faulty = f
}

func (m *mockTarget) get(name string) (uint16, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	port, ok := m.realized[name]

	return port, ok
}

func (m *mockTarget) put(name string, port uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.realized[name] = port
}

func (m *mockTarget) Update(ctx context.Context, txn statedb.ReadTxn, rev statedb.Revision, b *RBackend) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates++
	if m.faulty {
		return errors.New("target unavailable")
	}
	m.realized[b.Name] = b.Port

	return nil
}

func (m *mockTarget) Delete(ctx context.Context, txn statedb.ReadTxn, rev statedb.Revision, b *RBackend) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletes++
	if m.faulty {
		return errors.New("target unavailable")
	}
	delete(m.realized, b.Name)

	return nil
}

func (m *mockTarget) Prune(ctx context.Context, txn statedb.ReadTxn, desired iter.Seq2[*RBackend, statedb.Revision]) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prunes++
	want := map[string]struct{}{}
	for b := range desired {
		want[b.Name] = struct{}{}
	}
	for name := range m.realized {
		if _, ok := want[name]; !ok {
			delete(m.realized, name)
		}
	}

	return nil
}

var _ reconciler.Operations[*RBackend] = &mockTarget{}

type harness struct {
	db       *statedb.DB
	table    statedb.RWTable[*RBackend]
	rec      reconciler.Reconciler[*RBackend]
	target   *mockTarget
	markInit func(statedb.WriteTxn)
}

func newHarness(t *testing.T, opts ...reconciler.Option) *harness {
	t.Helper()
	h := &harness{target: newMockTarget()}

	hv := hive.New(
		statedb.Cell,
		job.Cell,
		cell.Provide(
			cell.NewSimpleHealth,
			func(r job.Registry, health cell.Health) job.Group {
				return r.NewGroup(health)
			},
		),
		cell.Invoke(func(db *statedb.DB) error {
			h.db = db
			tbl, err := statedb.NewTable(db, "backends", RBackendNameIndex)
			if err != nil {
				return err
			}
			h.table = tbl
			wtxn := db.WriteTxn(tbl)
			h.markInit = tbl.RegisterInitializer(wtxn, "test-init")
			wtxn.Commit()

			return nil
		}),
		cell.Invoke(func(params reconciler.Params) error {
			rec, err := reconciler.Register(
				params,
				h.table,
				(*RBackend).Clone,
				setStatus,
				getStatus,
				h.target,
				nil,
				opts...,
			)
			h.rec = rec

			return err
		}),
	)

	log := hivetest.Logger(t, hivetest.LogLevel(slog.LevelError))
	if err := hv.Start(log, context.Background()); err != nil {
		t.Fatalf("hive start: %v", err)
	}
	t.Cleanup(func() {
		if err := hv.Stop(log, context.Background()); err != nil {
			t.Fatalf("hive stop: %v", err)
		}
	})

	return h
}

func (h *harness) insertPending(t *testing.T, name string, port uint16) statedb.Revision {
	t.Helper()
	wtxn := h.db.WriteTxn(h.table)
	defer wtxn.Abort()
	_, _, err := h.table.Insert(wtxn, &RBackend{
		Name:   name,
		Port:   port,
		Status: reconciler.StatusPending(),
	})

	require.NoError(t, err)

	rev := h.table.Revision(wtxn)
	wtxn.Commit()

	return rev
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) status(name string) reconciler.Status {
	obj, _, found := h.table.Get(h.db.ReadTxn(), RBackendNameIndex.Query(name))
	if !found {
		return reconciler.Status{}
	}

	return obj.Status
}

func TestReconcilUpdate(t *testing.T) {
	h := newHarness(t)

	rev := h.insertPending(t, "web-1", 80)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := h.rec.WaitUntilReconciled(ctx, rev)
	require.NoError(t, err)

	waitFor(t, "status Done", func() bool {
		return h.status("web-1").Kind == reconciler.StatusKindDone
	})
	port, ok := h.target.get("web-1")
	require.True(t, ok)
	require.Equal(t, port, uint16(80))

	h.insertPending(t, "web-1", 8080)
	waitFor(t, "target updated to 8080", func() bool {
		port, _ = h.target.get("web-1")
		return port == 8080 && h.status("web-1").Kind == reconciler.StatusKindDone
	})
}
