package bumblebee

import (
	"fmt"

	"github.com/cilium/hive/cell"
	"github.com/spf13/pflag"
)

type GreeterConfig struct {
	Greeting string
	Loud     bool
}

func (GreeterConfig) Flags(flags *pflag.FlagSet) {
	// what is flag? and its purpose?
	flags.String("greeting", "hello", "greeting word")
	flags.Bool("loud", false, "uppercase output")
}

type Greeter interface {
	Greet(name string) string
}

type greeter struct {
	cfg GreeterConfig
}

func newGreeter(cfg GreeterConfig) Greeter {
	return &greeter{cfg: cfg}
}

func (g *greeter) Greet(name string) string {
	msg := fmt.Sprintf("%s, %s!", g.cfg.Greeting, name)
	if g.cfg.Loud {
		return "[LOUD] " + msg
	}

	return msg
}

type PrintService struct {
	greeter Greeter
	started bool
}

type PrintServiceParams struct {
	cell.In   // what is cell.In?
	Greeter   Greeter
	Lifecycle cell.Lifecycle
}

func NewPrintService(p PrintServiceParams) *PrintService {
	svc := &PrintService{greeter: p.Greeter}

	p.Lifecycle.Append(cell.Hook{
		OnStart: func(ctx cell.HookContext) error {
			svc.started = true
			fmt.Println("PrintService started")
			return nil
		},
		OnStop: func(ctx cell.HookContext) error {
			svc.started = false
			fmt.Println("PrintService stopped")
			return nil
		},
	})

	return svc
}

func (s *PrintService) Print(name string) string {
	if !s.started {
		return "service not started"
	}
	return s.greeter.Greet(name)
}

var Cell = cell.Module(
	"greeter",
	"GreeterModule",
	cell.Config(GreeterConfig{}),
	cell.Provide(newGreeter),
	cell.Provide(NewPrintService),
)
