package insight

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencyStaysWithinTheLimit(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	running, peak := 0, 0
	steps := make([]func(context.Context) error, 0, 12)
	for index := 0; index < 12; index++ {
		steps = append(steps, func(context.Context) error {
			mu.Lock()
			running++
			if running > peak {
				peak = running
			}
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			return nil
		})
	}
	if err := RunParallel(context.Background(), 3, steps...); err != nil {
		t.Fatalf("runParallel: %v", err)
	}
	if peak > 3 {
		t.Fatalf("peak concurrency = %d, want at most 3", peak)
	}
	if peak < 2 {
		t.Fatalf("peak concurrency = %d, want the steps to actually overlap", peak)
	}
}

func TestFirstErrorIsReturnedAndTheRestAreCancelled(t *testing.T) {
	t.Parallel()

	failure := errors.New("query failed")
	var cancelled atomic.Int32
	slow := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			cancelled.Add(1)
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	}
	steps := []func(context.Context) error{
		func(context.Context) error { return failure },
		slow, slow, slow,
	}
	start := time.Now()
	err := RunParallel(context.Background(), 4, steps...)
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the first failure", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("elapsed = %v, want the remaining steps to be cancelled promptly", elapsed)
	}
	if cancelled.Load() == 0 {
		t.Fatal("the remaining steps were never cancelled")
	}
}

func TestAlreadyCancelledParentDoesNotLookSuccessful(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran atomic.Int32
	err := RunParallel(ctx, 2, func(context.Context) error {
		ran.Add(1)
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestEmptyAndNilStepsAreSafe(t *testing.T) {
	t.Parallel()

	if err := RunParallel(context.Background(), 4); err != nil {
		t.Fatalf("no steps: %v", err)
	}
	if err := RunParallel(context.Background(), 0, nil, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("nil step with a zero limit: %v", err)
	}
}
