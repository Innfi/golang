package main

import (
	"context"
	"log"

	"github.com/cilium/cilium/pkg/hive"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/job"
	"github.com/cilium/statedb"

	config "playground/config"
	pods "playground/pods"

	v1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
)

func cellInvoker(jg job.Group, db *statedb.DB, table statedb.Table[*v1.Pod]) {
	jg.Add(job.OneShot(
		"initial",
		func(ctx context.Context, _ cell.Health) error {
			log.Println("cellInvoker: OneShot function")

			wtxn := db.WriteTxn(table)
			changeIterator, err := table.Changes(wtxn)
			wtxn.Commit()

			// exactly who fetches pod data...?

			if err != nil {
				return err
			}

			for {

			}
		},
	))
	// jg.Add(job.Timer("timer test", func(ctx context.Context) error {
	// 	log.Println("inside timer")

	// 	return nil
	// }, 1*time.Second))
}

func main() {
	log.Println("main")
	reader := config.NewConfigReader()
	configSet := reader.Read()
	log.Printf("configSet: %v\n", configSet)

	app := cell.Module("tester", "cell initial",
		pods.PodsCell,
		cell.Invoke(cellInvoker),
	)

	hive.New(app).Run(logging.DefaultSlogLogger)
}
