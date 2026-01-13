package pipeline

import (
	"context"
	"testing"
	"time"
)

// BenchmarkPipeline_Throughput measures raw throughput with minimal work
func BenchmarkPipeline_Throughput(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, x int) (int, error) {
		return x + 1, nil
	}

	for _, workers := range []int{1, 2, 4, 8, 16} {
		b.Run(string(rune(workers))+" workers", func(b *testing.B) {
			p := New(workers, handler)
			jobs := make(chan Job[int], 100)
			results := p.Run(ctx, jobs)

			b.ResetTimer()
			go func() {
				defer close(jobs)
				for i := 0; i < b.N; i++ {
					jobs <- Job[int]{ID: "bench", Payload: i}
				}
			}()

			for range results {
			}
		})
	}
}

// BenchmarkPipeline_WithBuffering compares buffered vs unbuffered channels
func BenchmarkPipeline_WithBuffering(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, x int) (int, error) {
		time.Sleep(10 * time.Microsecond) // Simulate work
		return x * 2, nil
	}

	bufferSizes := []int{0, 10, 100, 1000}
	for _, bufSize := range bufferSizes {
		b.Run("buffer_"+string(rune(bufSize)), func(b *testing.B) {
			p := New(4, handler, Options{
				JobBuffer:    bufSize,
				ResultBuffer: bufSize,
			})

			jobs, results := p.Start(ctx)

			b.ResetTimer()
			go func() {
				defer close(jobs)
				for i := 0; i < b.N; i++ {
					jobs <- Job[int]{ID: "bench", Payload: i}
				}
			}()

			for range results {
			}
		})
	}
}

// BenchmarkPipeline_WithTimeouts measures overhead of per-job timeouts
func BenchmarkPipeline_WithTimeouts(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, x int) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(1 * time.Microsecond):
			return x, nil
		}
	}

	b.Run("no_timeout", func(b *testing.B) {
		p := New(4, handler)
		jobs := make(chan Job[int], 10)
		results := p.Run(ctx, jobs)

		b.ResetTimer()
		go func() {
			defer close(jobs)
			for i := 0; i < b.N; i++ {
				jobs <- Job[int]{ID: "bench", Payload: i, Timeout: 0}
			}
		}()

		for range results {
		}
	})

	b.Run("with_timeout", func(b *testing.B) {
		p := New(4, handler)
		jobs := make(chan Job[int], 10)
		results := p.Run(ctx, jobs)

		b.ResetTimer()
		go func() {
			defer close(jobs)
			for i := 0; i < b.N; i++ {
				jobs <- Job[int]{ID: "bench", Payload: i, Timeout: 10 * time.Millisecond}
			}
		}()

		for range results {
		}
	})
}

// BenchmarkPipeline_WorkerScaling measures how performance scales with workers
func BenchmarkPipeline_WorkerScaling(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, x int) (int, error) {
		// Simulate CPU-bound work
		sum := 0
		for i := 0; i < 1000; i++ {
			sum += i
		}
		return x + sum, nil
	}

	for _, workers := range []int{1, 2, 4, 8, 16, 32} {
		b.Run(string(rune(workers))+"_workers", func(b *testing.B) {
			p := New(workers, handler, Options{ResultBuffer: 100})
			jobs := make(chan Job[int], 100)
			results := p.Run(ctx, jobs)

			b.ResetTimer()
			go func() {
				defer close(jobs)
				for i := 0; i < b.N; i++ {
					jobs <- Job[int]{ID: "bench", Payload: i}
				}
			}()

			for range results {
			}
		})
	}
}

// BenchmarkPipeline_StatsOverhead measures the overhead of statistics tracking
func BenchmarkPipeline_StatsOverhead(b *testing.B) {
	ctx := context.Background()
	handler := func(ctx context.Context, x int) (int, error) {
		return x, nil
	}

	p := New(4, handler, Options{ResultBuffer: 100})
	jobs := make(chan Job[int], 100)
	results := p.Run(ctx, jobs)

	b.ResetTimer()
	go func() {
		defer close(jobs)
		for i := 0; i < b.N; i++ {
			jobs <- Job[int]{ID: "bench", Payload: i}
		}
	}()

	processed := 0
	for range results {
		processed++
		if processed%1000 == 0 {
			_ = p.Stats() // Periodically read stats
		}
	}
}
