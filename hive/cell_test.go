package bumblebee_test

import (
	"context"
	"errors"
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

func TestLifecycle(t *testing.T) {
	var started, stopped bool

	testCell := cell.Module(
		"lifecycle-test",
		"test lifecycle",
		cell.Invoke(func(lc cell.Lifecycle) {
			lc.Append(cell.Hook{
				OnStart: func(cell.HookContext) error {
					started = true
					return nil
				},
				OnStop: func(cell cell.HookContext) error {
					stopped = true
					return nil
				},
			})
		}),
	)

	log := hivetest.Logger(t)
	h := hive.New(testCell)

	require.NoError(t, h.Start(log, context.TODO()))
	assert.True(t, started, "OnStart called")
	assert.False(t, stopped, "OnStop not called yet")

	require.NoError(t, h.Stop(log, context.TODO()))
	assert.True(t, stopped, "OnStop called")
}

func TestShutdownNormal(t *testing.T) {
	h := hive.New(
		cell.Invoke(func(lc cell.Lifecycle, shutdowner hive.Shutdowner) {
			lc.Append(cell.Hook{
				OnStart: func(cell.HookContext) error {
					shutdowner.Shutdown()
					return nil
				},
			})
		}),
	)

	assert.NoError(t, h.Run(hivetest.Logger(t)))
}

func TestShutdownWithError(t *testing.T) {
	fatalErr := errors.New("fatal error")

	h := hive.New(
		cell.Invoke(func(lc cell.Lifecycle, shutdowner hive.Shutdowner) {
			lc.Append(cell.Hook{
				OnStart: func(cell.HookContext) error {
					shutdowner.Shutdown(hive.ShutdownWithError(fatalErr))
					return nil
				},
			})
		}),
	)

	assert.ErrorIs(t, h.Run(hivetest.Logger(t)), fatalErr)
}

func TestDecorate(t *testing.T) {
	type Counter struct{ N int }
	var outerN, innerN int
	decoratedCell := cell.Decorate(
		func(c *Counter) *Counter {
			return &Counter{N: c.N + 10}
		},
		cell.Invoke(func(c *Counter) {
			innerN = c.N
		}),
	)

	h := hive.New(
		cell.Provide(func() *Counter { return &Counter{N: 1} }),
		cell.Invoke(func(c *Counter) { outerN = c.N }),
		decoratedCell,
		cell.Invoke(func(lc cell.Lifecycle, s hive.Shutdowner) {
			lc.Append(cell.Hook{OnStart: func(cell.HookContext) error {
				s.Shutdown()
				return nil
			}})
		}),
	)

	require.NoError(t, h.Run(hivetest.Logger(t), func(h *hive.Hive) {
		h.PrintDotGraph()
	}))
	assert.Equal(t, 1, outerN)
	assert.Equal(t, 11, innerN)
}

func TestProvidePrivate(t *testing.T) {
	type Secret struct{ Token string }

	moduleCell := cell.Module(
		"private-test",
		"test private provide",
		cell.ProvidePrivate(func() *Secret {
			return &Secret{Token: "abc123"}
		}),
		cell.Invoke(func(s *Secret) {
			assert.Equal(t, "abc123", s.Token)
		}),
	)

	h := hive.New(moduleCell, cell.Invoke(func(lc cell.Lifecycle, s hive.Shutdowner) {
		lc.Append(cell.Hook{OnStart: func(cell.HookContext) error {
			s.Shutdown()
			return nil
		}})
	}))
	assert.NoError(t, h.Run(hivetest.Logger(t)))

	h2 := hive.New(
		moduleCell,
		cell.Invoke(func(s *Secret) {}),
	)
	err := h2.Start(hivetest.Logger(t), context.TODO())
	assert.ErrorContains(t, err, "missing type")
}
