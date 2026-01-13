// Package pipeline provides a concurrent worker pool for processing jobs with generics support.
package pipeline

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Job represents a unit of work to be processed by the pipeline.
// ID uniquely identifies the job, Payload contains the input data,
// and Timeout optionally specifies a per-job timeout duration.
type Job[T any] struct {
	ID      string
	Payload T
	Timeout time.Duration
}

// Result contains the outcome of processing a job.
// JobID matches the original Job.ID, Value holds the result,
// and Error contains any error that occurred during processing.
type Result[R any] struct {
	JobID string
	Value R
	Error error
}

// Options configures pipeline behavior.
// JobBuffer sets the input channel buffer size.
// ResultBuffer sets the output channel buffer size.
type Options struct {
	JobBuffer    int
	ResultBuffer int
}

// Stats provides runtime statistics about pipeline operation.
// JobsReceived counts total jobs received by workers.
// JobsCompleted counts successfully completed jobs.
// JobsFailed counts jobs that returned non-context errors.
// JobsCancelled counts jobs that were cancelled or timed out.
// ResultsEmitted counts total results sent to output channel.
type Stats struct {
	JobsReceived   uint64
	JobsCompleted  uint64
	JobsFailed     uint64
	JobsCancelled  uint64
	ResultsEmitted uint64
}

type counters struct {
	jobsReceived   atomic.Uint64
	jobsCompleted  atomic.Uint64
	jobsFailed     atomic.Uint64
	jobsCancelled  atomic.Uint64
	resultsEmitted atomic.Uint64
}

// Pipeline manages a pool of workers that process jobs concurrently.
// It is parameterized by input type T and output type R.
type Pipeline[T any, R any] struct {
	workers int
	handler func(context.Context, T) (R, error)
	opts    Options
	c       counters
}

// New creates a new Pipeline with the specified number of workers and handler function.
// workers specifies the number of concurrent workers (minimum 1).
// handler is called for each job with the job's payload.
// opts optionally configures buffer sizes for input and output channels.
// Panics if handler is nil.
func New[T any, R any](workers int, handler func(context.Context, T) (R, error), opts ...Options) *Pipeline[T, R] {
	if workers <= 0 {
		workers = 1
	}
	if handler == nil {
		panic("pipeline: handler function cannot be nil")
	}

	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.JobBuffer <= 0 {
		o.JobBuffer = 0
	}
	if o.ResultBuffer <= 0 {
		o.ResultBuffer = 0
	}

	return &Pipeline[T, R]{
		workers: workers,
		handler: handler,
		opts:    o,
	}
}

// Start creates input and output channels and begins processing.
// Returns the input channel for sending jobs and output channel for receiving results.
// The input channel should be closed by the caller when no more jobs will be sent.
// The output channel will be closed automatically when all workers finish.
// Panics if ctx is nil.
func (p *Pipeline[T, R]) Start(ctx context.Context) (chan<- Job[T], <-chan Result[R]) {
	if ctx == nil {
		panic("pipeline: context cannot be nil")
	}
	jobs := make(chan Job[T], p.opts.JobBuffer)
	results := make(chan Result[R], p.opts.ResultBuffer)
	go p.run(ctx, jobs, results)
	return jobs, results
}

// Run starts processing jobs from the provided input channel.
// Returns an output channel for receiving results.
// The output channel will be closed when all workers finish.
// Panics if ctx is nil.
func (p *Pipeline[T, R]) Run(ctx context.Context, jobs <-chan Job[T]) <-chan Result[R] {
	if ctx == nil {
		panic("pipeline: context cannot be nil")
	}
	results := make(chan Result[R], p.opts.ResultBuffer)
	go p.run(ctx, jobs, results)
	return results
}

func (p *Pipeline[T, R]) run(ctx context.Context, jobs <-chan Job[T], results chan<- Result[R]) {
	var wg sync.WaitGroup
	wg.Add(p.workers)

	for range p.workers {
		go func() {
			defer wg.Done()
			p.worker(ctx, jobs, results)
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()
}

func (p *Pipeline[T, R]) worker(ctx context.Context, jobs <-chan Job[T], results chan<- Result[R]) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}

			p.c.jobsReceived.Add(1)

			jobCtx := ctx
			cancel := func() {}
			if job.Timeout > 0 {
				jobCtx, cancel = context.WithTimeout(ctx, job.Timeout)
			}

			val, err := p.handler(jobCtx, job.Payload)
			cancel()

			switch {
			case err == nil:
				p.c.jobsCompleted.Add(1)
			case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
				p.c.jobsCancelled.Add(1)
			default:
				p.c.jobsFailed.Add(1)
			}

			select {
			case <-ctx.Done():
				return
			case results <- Result[R]{JobID: job.ID, Value: val, Error: err}:
				p.c.resultsEmitted.Add(1)
			}
		}
	}
}

// Stats returns a snapshot of the pipeline's runtime statistics.
// Safe to call concurrently from multiple goroutines.
func (p *Pipeline[T, R]) Stats() Stats {
	return Stats{
		JobsReceived:   p.c.jobsReceived.Load(),
		JobsCompleted:  p.c.jobsCompleted.Load(),
		JobsFailed:     p.c.jobsFailed.Load(),
		JobsCancelled:  p.c.jobsCancelled.Load(),
		ResultsEmitted: p.c.resultsEmitted.Load(),
	}
}
