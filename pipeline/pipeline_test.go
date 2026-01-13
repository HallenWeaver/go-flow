package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := setupTestContext()
		defer cancel()

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

		synctest.Wait()
		if started.Load() == 0 {
			t.Fatalf("expected some work to start before cancellation")
		}

		cancel()

		synctest.Wait()

		for range results {
		}
	})
}

func TestPerJobTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestStatsBasicAccounting(t *testing.T) {
	ctx := context.Background()

	testHandler := func(ctx context.Context, x int) (int, error) {
		if x%2 == 0 {
			return 0, context.Canceled
		}
		return x, nil
	}

	p := New(2, testHandler, Options{ResultBuffer: 10})

	jobs := make(chan Job[int])
	results := p.Run(ctx, jobs)

	go func() {
		defer close(jobs)
		for i := range 20 {
			jobs <- Job[int]{ID: "id", Payload: i}
		}
	}()

	seen := 0
	for range results {
		seen++
	}

	st := p.Stats()
	if st.ResultsEmitted != uint64(seen) {
		t.Fatalf("expected ResultsEmitted=%d, got %d", seen, st.ResultsEmitted)
	}
	if st.JobsReceived != 20 {
		t.Fatalf("expected JobsReceived=20, got %d", st.JobsReceived)
	}
	if st.JobsCompleted+st.JobsFailed+st.JobsCancelled != st.JobsReceived {
		t.Fatalf("expected succeeded+failed+canceled == received; got %+v", st)
	}
}

func TestZeroWorkers(t *testing.T) {
	handler := func(ctx context.Context, x int) (int, error) {
		return x, nil
	}

	p := New(0, handler)
	if p == nil {
		t.Fatal("pipeline should not be nil")
	}

	ctx := context.Background()
	jobs := make(chan Job[int], 1)
	results := p.Run(ctx, jobs)

	jobs <- Job[int]{ID: "test", Payload: 42}
	close(jobs)

	result := <-results
	if result.Value != 42 {
		t.Errorf("expected 42, got %d", result.Value)
	}
}

func TestNilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil handler")
		}
	}()

	New[int, int](1, nil)
}

func TestNilContext(t *testing.T) {
	handler := func(ctx context.Context, x int) (int, error) {
		return x, nil
	}

	p := New(1, handler)

	t.Run("Run_panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for nil context in Run")
			}
		}()

		jobs := make(chan Job[int])
		p.Run(nil, jobs)
	})

	t.Run("Start_panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for nil context in Start")
			}
		}()

		p.Start(nil)
	})
}

func TestEmptyPipeline(t *testing.T) {
	ctx := context.Background()
	handler := func(ctx context.Context, x int) (int, error) {
		return x, nil
	}

	p := New(3, handler)
	jobs := make(chan Job[int])
	results := p.Run(ctx, jobs)

	close(jobs) // Immediately close without sending

	count := 0
	for range results {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 results, got %d", count)
	}
}

func TestHandlerErrors(t *testing.T) {
	ctx := context.Background()

	testErr := errors.New("test error")
	handler := func(ctx context.Context, x int) (int, error) {
		if x%2 == 0 {
			return 0, testErr
		}
		return x * 2, nil
	}

	p := New(2, handler)
	jobs := make(chan Job[int])
	results := p.Run(ctx, jobs)

	go func() {
		defer close(jobs)
		for i := 0; i < 10; i++ {
			jobs <- Job[int]{ID: fmt.Sprintf("job-%d", i), Payload: i}
		}
	}()

	errorCount := 0
	successCount := 0

	for res := range results {
		if res.Error != nil {
			if !errors.Is(res.Error, testErr) {
				t.Errorf("unexpected error: %v", res.Error)
			}
			errorCount++
		} else {
			successCount++
		}
	}

	if errorCount != 5 {
		t.Errorf("expected 5 errors, got %d", errorCount)
	}
	if successCount != 5 {
		t.Errorf("expected 5 successes, got %d", successCount)
	}

	stats := p.Stats()
	if stats.JobsFailed != 5 {
		t.Errorf("expected JobsFailed=5, got %d", stats.JobsFailed)
	}
	if stats.JobsCompleted != 5 {
		t.Errorf("expected JobsCompleted=5, got %d", stats.JobsCompleted)
	}
}

func TestResultOrdering(t *testing.T) {
	ctx := context.Background()

	handler := func(ctx context.Context, x int) (int, error) {
		return x, nil
	}

	p := New(4, handler)
	jobs := make(chan Job[int])
	results := p.Run(ctx, jobs)

	go func() {
		defer close(jobs)
		for i := 0; i < 20; i++ {
			jobs <- Job[int]{ID: fmt.Sprintf("job-%02d", i), Payload: i}
		}
	}()

	seen := make(map[int]bool)
	for res := range results {
		seen[res.Value] = true
	}

	if len(seen) != 20 {
		t.Errorf("expected 20 unique results, got %d", len(seen))
	}
}

func TestConcurrentStats(t *testing.T) {
	ctx := context.Background()
	handler := func(ctx context.Context, x int) (int, error) {
		return x, nil
	}

	p := New(4, handler, Options{ResultBuffer: 100})
	jobs := make(chan Job[int], 100)
	results := p.Run(ctx, jobs)

	// Concurrently read stats while processing
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = p.Stats()
		}
	}()

	go func() {
		defer close(jobs)
		for i := 0; i < 50; i++ {
			jobs <- Job[int]{ID: fmt.Sprintf("job-%d", i), Payload: i}
		}
	}()

	for range results {
	}

	<-done

	stats := p.Stats()
	if stats.JobsReceived != 50 {
		t.Errorf("expected JobsReceived=50, got %d", stats.JobsReceived)
	}
}

func TestMultipleRuns(t *testing.T) {
	handler := func(ctx context.Context, x int) (int, error) {
		return x * 2, nil
	}

	p := New(2, handler)

	for run := 0; run < 3; run++ {
		run := run // Capture loop variable
		ctx := context.Background()
		jobs := make(chan Job[int])
		results := p.Run(ctx, jobs)

		go func() {
			defer close(jobs)
			for i := 0; i < 5; i++ {
				jobs <- Job[int]{ID: fmt.Sprintf("run%d-job%d", run, i), Payload: i}
			}
		}()

		count := 0
		for range results {
			count++
		}

		if count != 5 {
			t.Errorf("run %d: expected 5 results, got %d", run, count)
		}
	}
}

func setupTestContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
