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

	p := pipeline.New(4, func(ctx context.Context, x int) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(80 * time.Millisecond):
			return x * x, nil
		}
	})

	results := p.Run(ctx, jobs)

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

	for r := range results {
		if r.Error != nil {
			log.Printf("job: %s | err: %s", r.JobID, r.Error)
			continue
		}
		log.Printf("job: %s | value: %d", r.JobID, r.Value)
	}
}
