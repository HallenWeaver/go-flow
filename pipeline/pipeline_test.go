package pipeline

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessAllJobs(t *testing.T) {
	ctx, cancel := setupTestContext()
	defer cancel()

	jobs := make(chan Job[int])

	p := New(3, func(ctx context.Context, x int) (int, error) {
		return x + 1, nil
	})

	results := p.Run(ctx, jobs)

	go func() {
		defer close(jobs)

		for i := range 50 {
			jobs <- Job[int]{ID: fmt.Sprintf("job-%02d", i), Payload: i}
		}
	}()

	count := 0
	for res := range results {
		if res.Error != nil {
			t.Fatalf("unexpected error %v", res.Error)
		}
		count++
	}

	if count != 50 {
		t.Fatalf("expected 50 results, got %d", count)
	}
}

func TestCancellationStopsPipeline(t *testing.T) {
	ctx, cancel := setupTestContext()

	jobs := make(chan Job[int])
	var started atomic.Int32

	p := New(4, func(ctx context.Context, x int) (int, error) {
		started.Add(1)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return x, nil
		}
	})

	results := p.Run(ctx, jobs)

	go func() {
		defer close(jobs)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- Job[int]{ID: fmt.Sprintf("job-%02d", i), Payload: i}:
			}
		}
	}()

	time.Sleep(60 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		for range results {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("results did not close after cancellation (possible goroutine leak/block)")
	}

	if started.Load() == 0 {
		t.Fatalf("expected some work to start before cancellation")
	}
}

func TestPerJobTimeout(t *testing.T) {
	ctx := context.Background()

	jobs := make(chan Job[int])

	p := New(1, func(ctx context.Context, x int) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return x, nil
		}
	})

	results := p.Run(ctx, jobs)

	go func() {
		defer close(jobs)
		jobs <- Job[int]{ID: "fast-timeout", Payload: 1, Timeout: 20 * time.Millisecond}
		jobs <- Job[int]{ID: "no-timeout", Payload: 2, Timeout: 0}
	}()

	var gotTimeout, gotSuccess bool
	for res := range results {
		switch res.JobID {
		case "fast-timeout":
			if res.Error == nil {
				t.Fatalf("expected timeout error for job 'fast-timeout'")
			}
			gotTimeout = true
		case "no-timeout":
			if res.Error != nil {
				t.Fatalf("unexpected error for job 'no-timeout': %v", res.Error)
			}
			gotSuccess = true
		}
	}

	if !gotTimeout || !gotSuccess {
		t.Fatalf("expected both results; gotTimeout=%v gotSuccess=%v", gotTimeout, gotSuccess)
	}
}

func setupTestContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
