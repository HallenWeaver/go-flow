package pipeline_test

import (
	"context"
	"fmt"
	"time"

	"github.com/HallenWeaver/go-flow/pipeline"
)

// ExampleNew demonstrates creating a basic pipeline with 3 workers.
func ExampleNew() {
	handler := func(ctx context.Context, x int) (int, error) {
		return x * 2, nil
	}

	_ = pipeline.New(3, handler)
	fmt.Println("Pipeline created")
	// Output: Pipeline created
}

// ExamplePipeline_Start demonstrates the simplest way to use the pipeline.
func ExamplePipeline_Start() {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	handler := func(ctx context.Context, x int) (int, error) {
		return x * x, nil
	}

	p := pipeline.New(1, handler) // Use 1 worker for deterministic ordering
	jobs, results := p.Start(ctx)

	// Send jobs
	go func() {
		defer close(jobs)
		for i := 1; i <= 3; i++ {
			jobs <- pipeline.Job[int]{
				ID:      fmt.Sprintf("job-%d", i),
				Payload: i,
			}
		}
	}()

	// Receive results
	for result := range results {
		if result.Error == nil {
			fmt.Printf("%s: %d\n", result.JobID, result.Value)
		}
	}
	// Output:
	// job-1: 1
	// job-2: 4
	// job-3: 9
}

// ExamplePipeline_Run demonstrates using an existing job channel.
func ExamplePipeline_Run() {
	ctx := context.Background()

	handler := func(ctx context.Context, s string) (int, error) {
		return len(s), nil
	}

	p := pipeline.New(2, handler)

	jobs := make(chan pipeline.Job[string], 3)
	results := p.Run(ctx, jobs)

	// Send jobs
	jobs <- pipeline.Job[string]{ID: "1", Payload: "hello"}
	jobs <- pipeline.Job[string]{ID: "2", Payload: "world"}
	close(jobs)

	// Receive results
	for result := range results {
		fmt.Printf("Job %s: length %d\n", result.JobID, result.Value)
	}
	// Output:
	// Job 1: length 5
	// Job 2: length 5
}

// ExampleJob_timeout demonstrates using per-job timeouts.
func ExampleJob_timeout() {
	ctx := context.Background()

	handler := func(ctx context.Context, x int) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return x, nil
		}
	}

	p := pipeline.New(1, handler)
	jobs, results := p.Start(ctx)

	go func() {
		defer close(jobs)
		// This job will timeout
		jobs <- pipeline.Job[int]{
			ID:      "fast-timeout",
			Payload: 1,
			Timeout: 10 * time.Millisecond,
		}
		// This job will succeed
		jobs <- pipeline.Job[int]{
			ID:      "no-timeout",
			Payload: 2,
			Timeout: 0,
		}
	}()

	for result := range results {
		if result.Error != nil {
			fmt.Printf("%s: timeout\n", result.JobID)
		} else {
			fmt.Printf("%s: success\n", result.JobID)
		}
	}
	// Output:
	// fast-timeout: timeout
	// no-timeout: success
}

// ExamplePipeline_Stats demonstrates collecting pipeline statistics.
func ExamplePipeline_Stats() {
	ctx := context.Background()

	handler := func(ctx context.Context, x int) (int, error) {
		if x%2 == 0 {
			return 0, fmt.Errorf("even number")
		}
		return x, nil
	}

	p := pipeline.New(2, handler)
	jobs, results := p.Start(ctx)

	go func() {
		defer close(jobs)
		for i := 1; i <= 4; i++ {
			jobs <- pipeline.Job[int]{
				ID:      fmt.Sprintf("job-%d", i),
				Payload: i,
			}
		}
	}()

	// Consume all results
	for range results {
	}

	stats := p.Stats()
	fmt.Printf("Received: %d\n", stats.JobsReceived)
	fmt.Printf("Completed: %d\n", stats.JobsCompleted)
	fmt.Printf("Failed: %d\n", stats.JobsFailed)
	// Output:
	// Received: 4
	// Completed: 2
	// Failed: 2
}

// ExampleOptions demonstrates configuring buffer sizes.
func ExampleOptions() {
	ctx := context.Background()

	handler := func(ctx context.Context, x int) (int, error) {
		return x, nil
	}

	opts := pipeline.Options{
		JobBuffer:    10,
		ResultBuffer: 20,
	}

	p := pipeline.New(4, handler, opts)
	jobs, results := p.Start(ctx)

	go func() {
		defer close(jobs)
		jobs <- pipeline.Job[int]{ID: "1", Payload: 42}
	}()

	result := <-results
	fmt.Printf("Result: %d\n", result.Value)
	// Output: Result: 42
}

// ExamplePipeline_cancellation demonstrates graceful cancellation.
func ExamplePipeline_cancellation() {
	ctx, cancel := context.WithCancel(context.Background())

	handler := func(ctx context.Context, x int) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return x, nil
		}
	}

	p := pipeline.New(2, handler)
	jobs, results := p.Start(ctx)

	go func() {
		defer close(jobs)
		for i := range 100 {
			select {
			case <-ctx.Done():
				return
			case jobs <- pipeline.Job[int]{ID: fmt.Sprintf("%d", i), Payload: i}:
			}
		}
	}()

	// Cancel after receiving a few results
	count := 0
	for range results {
		count++
		if count == 3 {
			break
		}
	}

	// Cancel the context
	cancel()

	// Drain remaining results after cancellation
	for range results {
	}

	fmt.Println("Pipeline cancelled gracefully")
	// Output: Pipeline cancelled gracefully
}
