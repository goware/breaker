breaker
=======

Exponential-backoff retry for Go with optional jitter, context-aware sleeps, and outcome metadata.

## Features

- **Exponential backoff** — configurable base delay and growth factor
- **Optional jitter** — randomise delays to avoid thundering-herd problems
- **Context-aware sleeps** — cancellation/timeout interrupts backoff immediately
- **Two retry styles** — `Do` (retry everything, opt-out with `ErrFatal`) or `Run` (explicit `OK`/`Fail`/`Retry` per attempt)
- **Outcome metadata** — `DoWithOutcome`/`RunWithOutcome` return attempt count, latency, and retry flag
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

### Explicit retry predicate (Run)

When the caller needs fine-grained control over which errors are retryable:

```go
out := br.RunWithOutcome(ctx, func(attempt int) breaker.Result {
    resp, err := http.Get("https://api.example.com/data")
    if err != nil {
        return breaker.Retry(err) // network error — retry
    }
    if resp.StatusCode == http.StatusTooManyRequests {
        return breaker.Retry(fmt.Errorf("rate limited"))
    }
    if resp.StatusCode >= http.StatusBadRequest {
        return breaker.Fail(fmt.Errorf("status %d", resp.StatusCode)) // don't retry
    }
    return breaker.OK()
})
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
| `(*Breaker).Do(ctx, fn)` | Retry fn (all errors retried unless `ErrFatal`), return error |
| `(*Breaker).DoWithOutcome(ctx, fn)` | Like Do, return Outcome |
| `(*Breaker).Run(ctx, fn)` | Retry fn with explicit `Result` predicate, return error |
| `(*Breaker).RunWithOutcome(ctx, fn)` | Like Run, return Outcome |
| `Do(ctx, fn, log, backoff, factor, maxTries, ...Option)` | Stateless convenience for Do |
| `WithJitter(fraction)` | Option: set jitter fraction in [0, 1] |
| `OK()` / `Fail(err)` / `Retry(err)` | Result constructors for Run |
| `ErrFatal` | Sentinel: wrap to stop Do retrying immediately |
| `ErrHitMaxRetries` | Sentinel: returned when all retries are exhausted |
| `Result` | Single-attempt outcome with Err and Retryable flag |
| `Outcome` | Invocation metadata: Err, Attempts, Retried, Latency |

## LICENSE

MIT
