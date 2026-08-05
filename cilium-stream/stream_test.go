package bumblebee_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/cilium/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestSourcesAnsSinks(t *testing.T) {
	ctx := testCtx(t)

	got, err := stream.ToSlice(ctx, stream.FromSlice([]int{1, 2, 3}))
	require.NoError(t, err)
	require.True(t, slices.Equal(got, []int{1, 2, 3}))

	one, _ := stream.ToSlice(ctx, stream.Just("x"))
	none, _ := stream.ToSlice(ctx, stream.Empty[string]())
	require.Equal(t, len(one), 1)
	require.Equal(t, len(none), 0)

	boom := errors.New("boom")
	_, err = stream.ToSlice(ctx, stream.Error[int](boom))
	require.True(t, errors.Is(err, boom))

	first, err := stream.First(ctx, stream.Range(10, 20))
	require.NoError(t, err)
	require.Equal(t, first, 10)

	_, err = stream.First(ctx, stream.Empty[int]())
	require.True(t, errors.Is(err, io.EOF))

	last, err := stream.Last(ctx, stream.Range(0, 5))
	require.NoError(t, err)
	require.Equal(t, last, 4)

	ch := make(chan int, 3)
	ch <- 7
	ch <- 8
	close(ch)

	got, _ = stream.ToSlice(ctx, stream.FromChannel(ch))
	assert.Equal(t, got, []int{7, 8})
}
