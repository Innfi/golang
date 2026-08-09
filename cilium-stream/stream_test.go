package bumblebee_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync/atomic"
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

func TestSourcesAndSinks(t *testing.T) {
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

func TestToChannel(t *testing.T) {
	ctx := testCtx(t)

	boom := errors.New("stream failed")
	errCh := make(chan error, 1)

	src := stream.Concat(
		stream.FromSlice([]int{1, 2, 3}),
		stream.Error[int](boom),
	)

	items := []int{}
	for x := range stream.ToChannel(ctx, src,
		stream.WithBufferSize(16),
		stream.WithErrorChan(errCh),
	) {
		items = append(items, x)
	}

	assert.Equal(t, items, []int{1, 2, 3})
	err := <-errCh
	assert.True(t, errors.Is(err, boom))
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	stream.Stuck[int]().Observe(ctx,
		func(int) { t.Error("Stuck must not emit") },
		func(err error) { errCh <- err },
	)

	select {
	case err := <-errCh:
		t.Fatalf("completed before cancel: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errCh:
		assert.True(t, errors.Is(err, context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not complete the subscription")
	}
}

func TestOperstors(t *testing.T) {
	ctx := testCtx(t)

	doubledEvens, _ := stream.ToSlice(ctx,
		stream.Map(
			stream.Filter(stream.Range(0, 8), func(x int) bool { return x%2 == 0 }),
			func(x int) string { return string(rune('a' + x)) },
		))
	require.Equal(t, doubledEvens, []string{"a", "c", "e", "g"})

	sums, _ := stream.ToSlice(ctx,
		stream.Reduce(stream.Range(1, 5), 0, func(acc, x int) int { return acc + x }))
	require.Equal(t, sums, []int{10})

	flat, _ := stream.ToSlice(ctx,
		stream.FlatMap(stream.FromSlice([]int{1, 2}), func(x int) stream.Observable[int] {
			return stream.FromSlice([]int{x, x * 10})
		}))
	require.Equal(t, flat, []int{1, 10, 2, 20})

	dis, _ := stream.ToSlice(ctx, stream.Distinct(stream.FromSlice([]int{1, 1, 2, 2, 3, 1})))
	require.Equal(t, dis, []int{1, 2, 3, 1}) // not 1, 2, 3

	var runs atomic.Int64
	cold := stream.Map(stream.Just(1), func(x int) int {
		runs.Add(1)
		return x
	})

	assert.Equal(t, runs.Load(), int64(0))

	stream.ToSlice(ctx, cold)
	stream.ToSlice(ctx, cold)
	assert.Equal(t, runs.Load(), int64(2))
}
