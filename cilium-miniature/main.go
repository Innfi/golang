package main

import (
	"os"
	"time"

	"github.com/cilium/hive/cell"
	"github.com/cilium/statedb"
	"github.com/cilium/statedb/reconciler"
	"github.com/spf13/pflag"
	"golang.org/x/time/rate"

	"github.com/cilium/cilium/pkg/hive"
	"github.com/cilium/cilium/pkg/k8s/client"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/cilium/pkg/rate"
)

type Config struct {
	Directory  string
	FailFirstN int
}

func (Config) Flags(fs *pflag.FlagSet) {
	fs.String("directory", "/tmp/miniagent-backends",
		"Directory to write backend files into (stands in for a BPF map)")
	fs.Int("fail-first-n", 2,
		"Fail the first N Update attempts per backend, to demonstrate retries")
}

var backendsCell = cell.Module(
	"backend",
	"desired backend state and its reconciler",

	cell.Config(Config{}),
	cell.ProvidePrivate(
		NewBackendTable,
		newBackendOps,
	),
	cell.Provide(statedb.RWTable[*Backend].ToTable),
	cell.Invoke(
		registerController,
		registerBackendReconciler,
	),
)

func registerBackendReconciler(
	params reconciler.Params,
	ops reconciler.Operations[*Backend],
	tbl statedb.RWTable[*Backend],
) error {
	_, err := reconciler.Register(
		params,
		tbl,
		(*Backend).Clone,
		(*Backend).SetStatus,
		(*Backend).GetStatus,
		ops,
		nil,
		reconciler.WithPruning(30*time.Second),
		reconciler.WithRefreshing(time.Minute, rate.NewLimiter(100.0, 1)),
	)

	return err
}

var app = cell.Module(
	"miniagent",
	"Miniature cilium-agent",
	client.Cell,
	PodsCell,
	ServicesCell,
	backendsCell,
)

func main() {
	h := hive.New(app)
	h.RegisterFlags(pflag.CommandLine)
	if err := pflag.CommandLine.Parse(os.Args[1:]); err != nil {
		panic(err)
	}

	if err := h.Run(logging.DefaultSlogLogger); err != nil {
		panic(err)
	}
}
