package bumblebee_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/cilium/hive"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/hivetest"
	"github.com/cilium/hive/job"
	"github.com/cilium/stream"
	"github.com/stretchr/testify/assert"
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

func TestOneShotRuntimeAdd(t *testing.T) {
	var group job.Group
	h := newJobHive(t, func(g job.Group) {
		group = g
	})

	stop := startHive(t, h)
	defer stop()

	var ran counter
	group.Add(job.OneShot("runtime-job", func(ctx context.Context, health cell.Health) error {
		ran.inc()
		return nil
	}))

	waitFor(t, "runtime job to run without hive restart", func() bool {
		return ran.get() == 1
	})
}

func TestOneShotRetry(t *testing.T) {
	var attempts counter
	succeeded := make(chan struct{})

	h := newJobHive(t, func(g job.Group) {
		g.Add(job.OneShot("flaky", func(ctx context.Context, health cell.Health) error {
			if attempts.inc() < 3 {
				return errors.New("transient failure")
			}
			close(succeeded)
			return nil
		},
			job.WithRetry(3, job.ConstantBackoff(5*time.Millisecond)),
		))
	})
	stop := startHive(t, h)
	defer stop()

	select {
	case <-succeeded:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not succeed within retry budget")
	}
	got := attempts.get()
	assert.Equal(t, got, 3)
}

func TestOneShotWithShutdown(t *testing.T) {
	var attempts counter

	h := newJobHive(t, func(g job.Group) {
		g.Add(job.OneShot("critical",
			func(ctx context.Context, health cell.Health) error {
				attempts.inc()
				return errors.New("unrecoverable")
			},
			job.WithRetry(2, job.ConstantBackoff(time.Millisecond)),
			job.WithShutdown(),
		))
	})

	done := make(chan error, 1)
	go func() {
		done <- h.Run(hivetest.Logger(t, hivetest.LogLevel(slog.LevelError)))
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("hive did not shutdown after critical job failure")
	}

	got := attempts.get()
	assert.Equal(t, got, 3)
}

func TestTimerPeriodic(t *testing.T) {
	var ticks counter
	h := newJobHive(t, func(g job.Group) {
		g.Add(job.Timer("periodic", func(ctx context.Context) error {
			ticks.inc()
			return nil
		}, 10*time.Millisecond))
	})
	stop := startHive(t, h)
	defer stop()

	waitFor(t, "at least 3 periodic ticks", func() bool { return ticks.get() >= 3 })
}

func TestTimerTrigger(t *testing.T) {
	var runs counter
	trig := job.NewTrigger()

	h := newJobHive(t, func(g job.Group) {
		g.Add(job.Timer("sync", func(ctx context.Context) error {
			runs.inc()
			return nil
		},
			time.Hour,
			job.WithTrigger(trig),
		))
	})
	stop := startHive(t, h)
	defer stop()

	require.Zero(t, runs.get())

	trig.Trigger()
	waitFor(t, "triggered invocation", func() bool { return runs.get() >= 1 })

	before := runs.get()
	for range 100 {
		trig.Trigger()
	}
	waitFor(t, "coaleased invocation", func() bool { return runs.get() > before })
	time.Sleep(50 * time.Millisecond)
	got := runs.get() - before
	assert.LessOrEqual(t, got, 5)
}

func TestObserver(t *testing.T) {
	events, emit, complete := stream.Multicast[string]()

	var (
		mu   sync.Mutex
		seen []string
	)

	h := newJobHive(t, func(g job.Group) {
		g.Add(job.Observer("watcher", func(ctx context.Context, event string) error {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, event)
			return nil
		},
			events,
		))
	})
	stop := startHive(t, h)
	defer stop()

	waitFor(t, "observer subscription", func() bool {
		emit("probe")
		mu.Lock()
		defer mu.Unlock()
		return len(seen) > 0
	})

	emit("a")
	emit("b")
	emit("c")
	waitFor(t, "all events observed in order", func() bool {
		mu.Lock()
		defer mu.Unlock()
		n := len(seen)
		assert.GreaterOrEqual(t, n, 3)
		assert.Equal(t, seen[n-3], "a")
		assert.Equal(t, seen[n-2], "b")
		assert.Equal(t, seen[n-1], "c")
		return true
	})

	complete(nil)
}

func TestScopedGroup(t *testing.T) {
	var ran counter
	h := newJobHive(t, func(g job.Group) {
		sub := g.Scoped("datapath-sync")
		sub.Add(job.OneShot("scoped-job", func(ctx context.Context, health cell.Health) error {
			health.OK("done")
			ran.inc()
			return nil
		}))
	})
	stop := startHive(t, h)
	defer stop()

	waitFor(t, "scoped job to run", func() bool { return ran.get() == 1 })
}
