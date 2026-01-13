package main

import (
	"context"
	"fmt"
	"hallenweaver/go-flow/pipeline"
	"log"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	jobs := make(chan pipeline.Job[int])

	handlerFunc := func(ctx context.Context, x int) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return x * x, nil
		}
	}

	options := pipeline.Options{
		JobBuffer:    8,
		ResultBuffer: 8,
	}

	p := pipeline.New(4, handlerFunc, options)

	in, out := p.Start(ctx)

	go func() {
		defer close(jobs)
		for i := range 20 {
			timeout := 0 * time.Millisecond
			if i%5 == 0 {
				timeout = 50 * time.Millisecond
			}

			select {
			case <-ctx.Done():
				return
			case jobs <- pipeline.Job[int]{ID: fmt.Sprintf("job-%02d", i), Payload: i, Timeout: timeout}:
			}
		}
	}()

	go func() {
		defer close(in)
		for i := range 20 {
			timeout := time.Duration(0)
			if i%5 == 0 {
				timeout = 50 * time.Millisecond
			}
			select {
			case <-ctx.Done():
				return
			case in <- pipeline.Job[int]{
				ID:      fmt.Sprintf("job-%02d", i),
				Payload: i,
				Timeout: timeout,
			}:
			}
		}
	}()

	// Consumer
	for r := range out {
		if r.Error != nil {
			log.Printf("job=%s err=%v", r.JobID, r.Error)
			continue
		}
		log.Printf("job=%s value=%d", r.JobID, r.Value)
	}

	log.Printf("stats=%+v", p.Stats())
}
