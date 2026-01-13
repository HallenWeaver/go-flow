package pipeline

import (
	"context"
	"sync"
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

type Pipeline[T any, R any] struct {
	workers int
	handler func(context.Context, T) (R, error)
}

func New[T any, R any](workers int, handler func(context.Context, T) (R, error)) *Pipeline[T, R] {
	if workers <= 0 {
		workers = 1
	}
	if handler == nil {
		panic("pipeline: handler function cannot be nil")
	}
	return &Pipeline[T, R]{
		workers: workers,
		handler: handler,
	}
}

func (p *Pipeline[T, R]) Run(ctx context.Context, jobs <-chan Job[T]) <-chan Result[R] {
	results := make(chan Result[R])

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

	return results
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

			jobCtx := ctx
			cancel := func() {}
			if job.Timeout > 0 {
				jobCtx, cancel = context.WithTimeout(ctx, job.Timeout)
			}

			val, err := p.handler(jobCtx, job.Payload)
			cancel()

			select {
			case <-ctx.Done():
				return
			case results <- Result[R]{JobID: job.ID, Value: val, Error: err}:
			}
		}
	}
}
