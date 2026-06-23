package bumblebee_test

import (
	"context"
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
