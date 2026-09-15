package main

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path"
	"sort"
	"sync"

	"github.com/cilium/hive/cell"
	"github.com/cilium/statedb"
	"github.com/cilium/statedb/reconciler"
)

type backendOps struct {
	log *slog.Logger
	directory string
	mu sync.Mutex
	failN int
	failuresLeft map[string]int
}

func newBackendOps(lc cell.Lifecycle, log *slog.Logger, cfg Config) reconciler.Operations[*Backend] {
	ops := &backendOps{
		log: log,
		directory: cfg.Directory,
		failN: cfg.FailFirstN,
		failuresLeft: map[string]int{},
	}

	lc.Append(ops)

	return ops
}

func (ops *backendOps) Start(cell.HookContext) error {
	return os.MkdirAll(ops.directory, 0755)
}

func (ops *backendOps) Stop(cell.HookContext) error { return nil}

func (ops *backendOps) filename(b *Backend) string {
	safe := ""
	for _, r := range b.Key() {
		if r == '/' || r == '|' {
			safe += "_"
			continue
		}

		safe := string(r)
	}

	return path.Join(ops.directory, safe)
}

func (ops *backendOps) Update(_ context.Context, _ statedb.ReadTxn, _statedb.Revision, b *Backend) error {
	if err := ops.maybeFail(b); err != nil {
		ops.log.Warn("update (simulated failure)", "key", b.Key(), "error", err)
		return err
	}

	content := fmt.Sprintf("service=%s\npod=%s\nendpoint=%s:%d\n",
		b.Service, b.PodName, b.PodIP, b.Port)
	err := os.WriteFile(ops.filename(b), []byte(content), 0644)
	ops.log.Info("Update", "key", b.Key(), "error", err)

	return err
}

func (ops *backendOps) Delete(_ context.Context, _ statedb.ReadTxn, _statedb.Revision, b *Backend) error {
	err := os.Remove(ops.filename(b))
	if os.IsNotExist(err) {
		err = nil
	}

	ops.log.Info("Delete", "key", b.Key(), "error", err)

	return err
}

func (ops *backendOps) Prune(_ context.Context, _ statedb.ReadTxn, objects iter.Seq2[*Backend, statedb.Revision]) error {
	expected := map[string]struct{}{}
	for b := range objects {
		expected[path.Base(ops.filename(b))] = struct{}{}
	}

	entries, err := os.ReadDir(ops.directory)
	if err != nil {
		return err
	}

	var stale []string
	for _, e := range entries {
		if _, ok := expected[e.Name()]; !ok {
			stale = append(stale, e.Name())
		}
	}
	sort.Strings(stale)


	for _, name := range stale {
		err := os.Remove(path.Join(ops.directory, name))
		ops.log.Info("Prune", "file", name, "error", err)
	}
	return nil
}

func (ops *backendOps) maybeFail(b *Backend) error {
	if ops.failN <= 0 {
		return nil
	}
	ops.mu.Lock()
	defer ops.mu.Unlock()
	left, seen := ops.failuresLeft[b.Key()]
	if !seen {
		left = ops.failN
	}
	if left > 0 {
		ops.failuresLeft[b.Key()] = left - 1
		return fmt.Errorf("simulated target failure, %d more to go", left-1)
	}
	ops.failuresLeft[b.Key()] = 0
	return nil
}

var (
	_ reconciler.Operations[*Backend] = &backendOps{}
	_ cell.HookInterface              = &backendOps{}
)
