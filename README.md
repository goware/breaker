breaker
=======

Exponential-backoff retry for Go with optional jitter, context-aware sleeps, and outcome metadata.

## Features

- **Exponential backoff** — configurable base delay and growth factor
- **Optional jitter** — randomise delays to avoid thundering-herd problems
- **Context-aware sleeps** — cancellation/timeout interrupts backoff immediately
- **Fatal errors** — wrap with `breaker.ErrFatal` to stop retrying at once
- **Outcome metadata** — `DoWithOutcome` returns attempt count, latency, and retry flag
- **Structured logging** — optional `slog.Logger` for retry/exhaustion events

## Install

```
go get github.com/goware/breaker
```

## Usage

### Basic retry

```go
br := breaker.New(logger, 500*time.Millisecond, 2.0, 3) // 500ms, x2, up to 3 retries

err := br.Do(ctx, func() error {
    resp, err := http.Get("https://api.example.com/data")
    if err != nil {
        return err // retryable
    }
    if resp.StatusCode == http.StatusBadRequest {
        return fmt.Errorf("bad request: %w", breaker.ErrFatal) // non-retryable
    }
    return nil
})
```

### With jitter

```go
br := breaker.New(logger, 1*time.Second, 2.0, 5, breaker.WithJitter(0.2)) // ±20% jitter
```

`WithJitter(0)` (the default) keeps delays fully deterministic for backward compatibility.

### Outcome metadata

```go
out := br.DoWithOutcome(ctx, func() error {
    return callAPI()
})

fmt.Printf("attempts=%d retried=%v latency=%v err=%v\n",
    out.Attempts, out.Retried, out.Latency, out.Err)
```

### Package-level convenience

```go
err := breaker.Do(ctx, fn, logger, 1*time.Second, 2.0, 3, breaker.WithJitter(0.1))
```

## API

| Function / Type | Description |
|----------------|-------------|
| `New(log, backoff, factor, maxTries, ...Option)` | Create a configured Breaker |
| `Default(optLog...)` | Breaker with sensible defaults (1s, x2, 15 retries, no jitter) |
| `(*Breaker).Do(ctx, fn)` | Run fn with retries, return error |
| `(*Breaker).DoWithOutcome(ctx, fn)` | Run fn with retries, return Outcome |
| `Do(ctx, fn, log, backoff, factor, maxTries, ...Option)` | Stateless convenience function |
| `WithJitter(fraction)` | Option: set jitter fraction in [0, 1] |
| `ErrFatal` | Sentinel: wrap to stop retrying immediately |
| `ErrHitMaxRetries` | Sentinel: returned when all retries are exhausted |
| `Outcome` | Struct with Err, Attempts, Retried, Latency |

## LICENSE

MIT
