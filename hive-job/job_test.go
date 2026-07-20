package bumblebee_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/cilium/hive"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/hivetest"
	"github.com/cilium/hive/job"
	"github.com/stretchr/testify/require"
)

func newJobHive(t *testing.T, configure func(g job.Group)) *hive.Hive {
	t.Helper()
	return hive.New(
		job.Cell,
		cell.Provide(
			cell.NewSimpleHealth,
			func(r job.Registry, h cell.Health) job.Group {
				return r.NewGroup(h)
			},
		),
		cell.Invoke(func(g job.Group) {
			configure(g)
		}),
	)
}

func startHive(t *testing.T, h *hive.Hive) func() {
	t.Helper()
	log := hivetest.Logger(t, hivetest.LogLevel(slog.LevelError))
	if err := h.Start(log, context.Background()); err != nil {
		t.Fatalf("hive start: %v", err)
	}

	return func() {
		if err := h.Stop(log, context.Background()); err != nil {
			t.Fatalf("hive stop: %v", err)
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("timed out waiting for %s", what)
}

type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) inc() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n
}

func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestOneShopLifecycle(t *testing.T) {
	var (
		started  = make(chan struct{})
		returned counter
	)

	h := newJobHive(t, func(g job.Group) {
		g.Add(job.OneShot("blocker", func(ctx context.Context, health cell.Health) error {
			close(started)
			<-ctx.Done()
			returned.inc()
			return nil
		}))
	})

	stop := startHive(t, h)

	<-started

	require.Equal(t, returned.get(), 0)

	stop()

	require.Equal(t, returned.get(), 1)
}
