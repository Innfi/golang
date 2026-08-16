package main

import (
	"context"
	"log"

	"github.com/cilium/cilium/pkg/hive"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/job"

	config "playground/config"
)

func cellInvoker(jg job.Group) {
	jg.Add(job.OneShot(
		"initial",
		func(ctx context.Context, _ cell.Health) error {
			log.Println("cellInvoker: OneShot function")

			return nil
		},
	))
}

func main() {
	log.Println("main")
	reader := config.NewConfigReader()
	configSet := reader.Read()
	log.Printf("configSet: %v\n", configSet)

	hive.New(
		cell.Module("tester", "cell initial",
			cell.Invoke(cellInvoker),
		),
	).Run(logging.DefaultSlogLogger)
}
