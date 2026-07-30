package bumblebee_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cilium/stream"
	"github.com/stretchr/testify/assert"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestObservableContract(t *testing.T) {
	ctx := testCtx(t)

	src := stream.FuncObservable[int](
		func(ctx context.Context, next func(int), complete func(error)) {
			go func() {
				defer complete(nil)
				for i := 1; i <= 3; i++ {
					if ctx.Err() != nil {
						return
					}
					next(i)
				}
			}()
		})

	var (
		items       []int
		completions int
		completeErr = errors.New("not called")
		done        = make(chan struct{})
	)

	src.Observe(ctx,
		func(x int) { items = append(items, x) },
		func(err error) {
			completions++
			completeErr = err
			close(done)
		})
	<-done

	assert.Equal(t, slices.Equal(items, []int{1, 2, 3}), true)
	assert.Equal(t, completions, 1)
	assert.NoError(t, completeErr)
}
