package bumblebee_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cilium/hive"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/hivetest"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bumblebee "bumblebee"
)

func TestPopulate(t *testing.T) {
	h := hive.New(bumblebee.Cell)

	err := h.Populate(hivetest.Logger(t))
	assert.NoError(t, err)
}

func TestConfigViaFlag(t *testing.T) {
	var captured bumblebee.GreeterConfig

	h := hive.New(
		bumblebee.Cell,
		cell.Invoke(func(cfg bumblebee.GreeterConfig) {
			captured = cfg
		}),
	)

	flags := pflag.NewFlagSet("", pflag.ContinueOnError)
	h.RegisterFlags(flags)
	flags.Set("greeting", "hi")

	log := hivetest.Logger(t)

	require.NoError(t, h.Start(log, context.TODO()))
	require.NoError(t, h.Stop(log, context.TODO()))

	assert.Equal(t, "hi", captured.Greeting)
}

func TestConfigOverri(t *testing.T) {
	var captured bumblebee.GreeterConfig

	h := hive.New(
		bumblebee.Cell,
		cell.Invoke(func(cfg bumblebee.GreeterConfig) {
			captured = cfg
		}),
	)

	hive.AddConfigOverride(h, func(cfg *bumblebee.GreeterConfig) {
		cfg.Greeting = "override"
		cfg.Loud = true
	})

	log := hivetest.Logger(t)
	require.NoError(t, h.Start(log, context.TODO()))
	require.NoError(t, h.Stop(log, context.TODO()))

	assert.Equal(t, "override", captured.Greeting)
	assert.True(t, captured.Loud)
}

func TestInvokeExtract(t *testing.T) {
	var greeter bumblebee.Greeter

	h := hive.New(
		bumblebee.Cell,
		cell.Invoke(func(g bumblebee.Greeter) {
			greeter = g
		}),
	)

	hive.AddConfigOverride(h, func(cfg *bumblebee.GreeterConfig) {
		cfg.Greeting = "hey"
	})

	require.NoError(t, h.Populate(hivetest.Logger(t)))
	require.NotNil(t, greeter)

	result := greeter.Greet("world")
	assert.Equal(t, "hey, world!", result)
}

type mockGreeter struct {
	response string
}

func (m *mockGreeter) Greet(name string) string {
	return m.response
}

func TestMockInjection(t *testing.T) {
	var svc *bumblebee.PrintService
	mockResponse := "mocked"

	h := hive.New(
		// is it possible to include another cell?
		cell.Module(
			"greeter",
			"greeter mock",
			cell.Config(bumblebee.GreeterConfig{}),
			cell.Provide(func() bumblebee.Greeter {
				return &mockGreeter{response: "mocked"}
			}),
			cell.Provide(bumblebee.NewPrintService),
		),
		cell.Invoke(func(g bumblebee.Greeter) {
			fmt.Println("--- Invoke greeter ---")
		}),
		cell.Invoke(func(s *bumblebee.PrintService) {
			svc = s
		}),
	)

	log := hivetest.Logger(t)
	require.NoError(t, h.Start(log, context.TODO()))
	defer h.Start(log, context.TODO())

	result := svc.Print("alice")
	assert.Equal(t, result, mockResponse)
}
