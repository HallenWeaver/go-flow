package pipeline

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Job[T any] struct {
	ID      string
	Payload T
	Timeout time.Duration
}

type Result[R any] struct {
	JobID string
	Value R
	Error error
}

type Options struct {
	JobBuffer    int
	ResultBuffer int
}

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

type Pipeline[T any, R any] struct {
	workers int
	handler func(context.Context, T) (R, error)
	opts    Options
	c       counters
}

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

func (p *Pipeline[T, R]) Start(ctx context.Context) (chan<- Job[T], <-chan Result[R]) {
	jobs := make(chan Job[T], p.opts.JobBuffer)
	results := make(chan Result[R], p.opts.ResultBuffer)
	p.run(ctx, jobs, results)
	return jobs, results
}

func (p *Pipeline[T, R]) Run(ctx context.Context, jobs <-chan Job[T]) <-chan Result[R] {
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

func (p *Pipeline[T, R]) Stats() Stats {
	return Stats{
		JobsReceived:   p.c.jobsReceived.Load(),
		JobsCompleted:  p.c.jobsCompleted.Load(),
		JobsFailed:     p.c.jobsFailed.Load(),
		JobsCancelled:  p.c.jobsCancelled.Load(),
		ResultsEmitted: p.c.resultsEmitted.Load(),
	}
}
