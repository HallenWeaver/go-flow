package main

import (
	"context"
	"fmt"
	"hallenweaver/goflow/pipeline"
	"log"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	jobs := make(chan pipeline.Job[int])

	p := pipeline.New(4, func(ctx context.Context, x int) (int, error) {
		time.Sleep(80 * time.Millisecond)
		return x * x, nil
	})

	results := p.Run(ctx, jobs)

	go func() {
		defer close(jobs)
		for i := 0; i < 20; i++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- pipeline.Job[int]{ID: fmt.Sprintf("job-%02d", i), Payload: i}:
			}
		}
	}()

	for r := range results {
		if r.Error != nil {
			log.Printf("job: %s | err: %s", r.JobID, r.Error)
			continue
		}
		log.Printf("job: %s | value: %s", r.JobID, r.Value)
	}
}
