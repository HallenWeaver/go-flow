# GoFlow

**GoFlow** is a lightweight, production-ready Go library that shows you how to build **bounded, cancellable, backpressure-aware worker pipelines** using standard Go concurrency patterns.

It's intentionally kept small, but we don't compromise on correctness — no goroutine leaks, no unbounded concurrency, and deterministic tests for cancellation.

---

## Why this exists

In real-world Go systems (HTTP services, Kafka consumers, batch jobs), concurrency bugs rarely come from syntax issues. Instead, they come from **lifecycle mistakes** such as:

- goroutines that never stop
- channels that are never closed
- producers that overwhelm consumers
- shutdown paths that work "most of the time"

GoFlow is here to show you a **clean, repeatable pattern** for solving these problems.

---

## What you get

GoFlow gives you a few solid guarantees:

1. **Bounded concurrency** — Worker count is fixed and explicit. No surprise goroutine explosions.

2. **Backpressure by default** — Unbuffered channels enforce strict producer/consumer coordination. You can enable optional buffering if you need to absorb bursts.

3. **Graceful shutdown** — Context cancellation flows through the entire pipeline. Every goroutine knows when to exit.

4. **Deterministic termination** — The results channel closes exactly once, after all workers exit. No send-on-closed-channel panics, no leaks.

5. **Testable concurrency** — Cancellation and timeout behavior is tested deterministically using `testing/synctest` (Go 1.25+), without `time.Sleep` hacks.

## Design tradeoffs

GoFlow makes a few deliberate tradeoffs to prioritize correctness, predictability, and operational safety.

### Bounded concurrency over dynamic scaling

The pipeline uses a fixed-size worker pool instead of dynamically spawning goroutines for each job.

This gives you:
- predictable resource usage
- protection against load spikes
- no goroutine explosions under backpressure

You can always add dynamic scaling on top if you need it, but unbounded concurrency is almost always a liability in production systems.

---

### Channels for coordination, not data structures

We use channels as **coordination primitives**, not as queues or storage.

- Unbuffered channels enforce strict backpressure.
- Buffered channels are optional and explicitly bounded.
- There are no unbounded queues anywhere in the pipeline.

Buffers delay backpressure; they don't remove it. Making buffering explicit helps you make conscious design decisions.

---

### Context as the single source of truth for lifetimes

We use `context.Context` consistently to control:
- cancellation
- shutdown
- per-job deadlines

Workers never rely on implicit signals or global state. If work is no longer relevant, it stops.

This mirrors how real Go systems handle HTTP requests, background jobs, and message consumers.

---

### Results emission guarded by context

Workers guard sends to the results channel with a context check.

This prevents a common shutdown bug where:
- workers finish their work
- but block forever trying to emit results after cancellation

Getting shutdown right means treating result emission as a cancellable operation.

---

### Atomic counters instead of channels for metrics

Pipeline statistics use atomic counters instead of channels.

This helps us avoid:
- additional goroutines
- contention on shared channels
- interference with the main data flow

Since metrics are observational and not part of control flow, lock-free counters are simpler and safer.

---

### Deterministic concurrency tests over timing assumptions

Concurrency tests use `testing/synctest` instead of `time.Sleep`.

This lets tests verify that:
- goroutines terminate
- channels close
- cancellation propagates

All without relying on wall-clock timing or arbitrary delays.

If a goroutine leaks or blocks indefinitely, the test fails deterministically. 

## How it works

```
Producer
   ↓
[ jobs channel ]  ← bounded / unbuffered (backpressure)
   ↓
Worker Pool (N goroutines)
   ↓
[ results channel ]
   ↓
Consumer
```

### Who does what

- **Producer** — Sends jobs and owns closing the jobs channel

- **Pipeline** — Starts and manages workers, owns closing the results channel

- **Workers** — Process jobs and exit when context is cancelled or input closes

Ownership is clear and one-directional, which helps prevent races and panics.

## Using the API

### Job and Result

```go
type Job[T any] struct {
    ID      string
    Payload T
    Timeout time.Duration // optional per-job deadline
}

type Result[R any] struct {
    JobID string
    Value R
    Err   error
}
```

### Creating a pipeline

```go
p := pipeline.New[int, int](
    4,
    handlerFunc,
    pipeline.Options{
        JobBuffer:    0, // strict backpressure
        ResultBuffer: 0,
    },
)
```

### Running with internally managed channels

```go
in, out := p.Start(ctx)

go func() {
    defer close(in)
    for i := 0; i < 10; i++ {
        in <- pipeline.Job[int]{ID: "job", Payload: i}
    }
}()

for r := range out {
    // consume results
}
```

### Running with your own channels

```go
results := p.Run(ctx, jobs)
```

## Handling cancellation & timeouts

GoFlow uses `context.Context` as the single source of truth for managing lifetimes.

- When you cancel the parent context, it stops:
  - job intake
  - in-flight work
  - result emission

- Each job can also have its own timeout:
  ```go
  Job{Payload: x, Timeout: 50 * time.Millisecond}
  ```

Per-job timeouts work by deriving a child context from the pipeline context. All timers are properly cleaned up to avoid resource leaks.

## Understanding backpressure

Backpressure is a deliberate design choice here, not an accident.

- `JobBuffer = 0` — Producers block until a worker is ready. Strict coordination.

- `JobBuffer > 0` — Short bursts get absorbed, but pressure is still bounded.

Buffers are **not infinite queues** — they only delay blocking, they don't eliminate it.

## Metrics

GoFlow tracks lightweight, lock-free counters:

- jobs received
- jobs succeeded
- jobs failed
- jobs canceled
- results emitted

```go
stats := p.Stats()
```

These are meant for observability and sanity checks, not heavy analytics.

## Testing concurrent code deterministically

Concurrency tests often rely on flaky `time.Sleep` calls. GoFlow avoids this by using `testing/synctest` (Go 1.25+).

Here's an example of what we test:

> After context cancellation, all workers must exit and the results channel must close.

We verify this by checking that the system reaches **quiescence**. If any goroutine leaks or blocks forever, the test fails with a deadlock—no timeouts needed.

This makes shutdown behavior reliable and CI-friendly.

## What we intentionally left out

- Unbounded queues
- Hidden goroutine creation
- Implicit retries
- Complex orchestration logic

Those belong at higher layers. GoFlow focuses on getting the **concurrency foundations right**.

## When to use this pattern

- HTTP request fan-out with cancellation
- Kafka / message queue consumers
- Background job processors
- Batch pipelines
- CLI tools with concurrent work

This pattern scales well from small tools to large services.

## Requirements

- Go **1.25+** (for `testing/synctest`)

## Final thoughts

This project is deliberately small.

Every line is here to demonstrate:
- ownership
- lifetimes
- backpressure
- termination

Those are the tricky parts of concurrent systems.
