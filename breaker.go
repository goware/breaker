package breaker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/goware/superr"
)

var (
	ErrFatal         = errors.New("breaker: fatal error")
	ErrHitMaxRetries = errors.New("breaker: hit max retries")
)

// Breaker is an exponential-backoff-retry caller with optional jitter.
type Breaker struct {
	log      *slog.Logger
	backoff  time.Duration
	factor   float64
	jitter   float64 // jitter fraction in [0, 1]; 0 means no jitter (deterministic)
	maxTries int
}

// Option configures a Breaker.
type Option func(*Breaker)

// WithJitter sets the jitter fraction applied to each backoff delay.
// A value of 0.1 means ±10% randomisation around the computed delay.
// The value is clamped to the range [0, 1].
func WithJitter(fraction float64) Option {
	return func(b *Breaker) {
		if fraction < 0 {
			fraction = 0
		}
		if fraction > 1 {
			fraction = 1
		}
		b.jitter = fraction
	}
}

// Result holds the outcome of a single attempt. The caller decides whether the
// error is retryable by setting Retryable to true. Used with Run/RunWithOutcome.
type Result struct {
	Err       error
	Retryable bool
}

// OK returns a successful (non-retryable) Result.
func OK() Result { return Result{} }

// Fail returns a non-retryable error Result that stops the retry loop immediately.
func Fail(err error) Result { return Result{Err: err} }

// Retry returns a retryable error Result that signals the operation should be retried.
func Retry(err error) Result { return Result{Err: err, Retryable: true} }

// Outcome holds metadata about a Do or Run invocation.
type Outcome struct {
	Err      error
	Attempts int           // total number of fn invocations
	Retried  bool          // true if fn was called more than once
	Latency  time.Duration // wall-clock time from start to return
}

// Default returns a Breaker with sensible defaults: 1s backoff, 2x factor,
// 15 retries, no jitter. An optional logger may be provided for retry events.
func Default(optLog ...*slog.Logger) *Breaker {
	var log *slog.Logger
	if len(optLog) > 0 {
		log = optLog[0]
	}
	return &Breaker{
		log:      log,
		backoff:  1 * time.Second, // backoff for 1 second to start,
		factor:   2,               // and for each attempt multiply backoff by factor,
		maxTries: 15,              // until number of maxTries before giving up
	}
}

// New creates a Breaker with the given backoff, exponential factor, and maximum
// number of retries. Functional options (e.g. WithJitter) can be appended to
// customise behaviour. The logger may be nil to suppress retry log output.
func New(log *slog.Logger, backoff time.Duration, factor float64, maxTries int, opts ...Option) *Breaker {
	b := &Breaker{
		log:      log,
		backoff:  backoff,
		factor:   factor,
		maxTries: maxTries,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Do is an exponential-backoff-retry caller which will wait `backoff*factor**retry` up to `maxTries`.
// `maxTries = 1` means retry only once when an error occurs.
// Backoff sleeps respect context cancellation.
func (b *Breaker) Do(ctx context.Context, fn func() error) error {
	out := b.DoWithOutcome(ctx, fn)
	return out.Err
}

// DoWithOutcome behaves like Do but returns an Outcome with attempt and timing metadata.
func (b *Breaker) DoWithOutcome(ctx context.Context, fn func() error) Outcome {
	start := time.Now()
	delay := float64(b.backoff)
	try := 0

	for {
		select {
		case <-ctx.Done():
			return Outcome{
				Err:      ctx.Err(),
				Attempts: try,
				Retried:  try > 1,
				Latency:  time.Since(start),
			}
		default:
		}

		err := fn()
		try++

		if err == nil {
			return Outcome{
				Attempts: try,
				Retried:  try > 1,
				Latency:  time.Since(start),
			}
		}

		// Check if is fatal error and should stop immediately
		if errors.Is(err, ErrFatal) {
			return Outcome{
				Err:      err,
				Attempts: try,
				Retried:  try > 1,
				Latency:  time.Since(start),
			}
		}

		// Move on if we have tried enough times.
		if try > b.maxTries {
			if b.log != nil {
				b.log.Error(fmt.Sprintf("breaker: exhausted after max number of retries %d. fail :(", b.maxTries))
			}
			return Outcome{
				Err:      superr.New(ErrHitMaxRetries, err),
				Attempts: try,
				Retried:  try > 1,
				Latency:  time.Since(start),
			}
		}

		sleepDur := b.applyJitter(time.Duration(int64(delay)))

		if b.log != nil {
			b.log.Warn(fmt.Sprintf("breaker: fn failed: '%v' - backing off for %v and trying again (retry #%d)", err, sleepDur.String(), try))
		}

		// Sleep with context awareness.
		if !sleepContext(ctx, sleepDur) {
			return Outcome{
				Err:      ctx.Err(),
				Attempts: try,
				Retried:  try > 1,
				Latency:  time.Since(start),
			}
		}

		delay *= b.factor
	}
}

// Run executes fn with exponential-backoff retries. Unlike Do, the caller
// explicitly controls retryability by returning a Result (OK, Fail, or Retry).
// The attempt number (0-indexed) is passed to fn for logging or adaptive logic.
func (b *Breaker) Run(ctx context.Context, fn func(attempt int) Result) error {
	return b.RunWithOutcome(ctx, fn).Err
}

// RunWithOutcome behaves like Run but returns an Outcome with attempt and timing metadata.
func (b *Breaker) RunWithOutcome(ctx context.Context, fn func(attempt int) Result) Outcome {
	start := time.Now()
	delay := float64(b.backoff)
	var lastErr error

	for try := range b.maxTries + 1 {
		// Check for context cancellation before each attempt,
		// including the first — matching DoWithOutcome behaviour.
		select {
		case <-ctx.Done():
			return Outcome{
				Err:      ctx.Err(),
				Attempts: try,
				Retried:  try > 1,
				Latency:  time.Since(start),
			}
		default:
		}

		if try > 0 {
			sleepDur := b.applyJitter(time.Duration(int64(delay)))

			if b.log != nil {
				b.log.Warn(fmt.Sprintf("breaker: fn failed: '%v' - backing off for %v and trying again (retry #%d)", lastErr, sleepDur.String(), try))
			}

			if !sleepContext(ctx, sleepDur) {
				return Outcome{
					Err:      ctx.Err(),
					Attempts: try,
					Retried:  try > 1,
					Latency:  time.Since(start),
				}
			}

			delay *= b.factor
		}

		r := fn(try)

		if r.Err == nil {
			return Outcome{
				Attempts: try + 1,
				Retried:  try > 0,
				Latency:  time.Since(start),
			}
		}

		if !r.Retryable {
			return Outcome{
				Err:      r.Err,
				Attempts: try + 1,
				Retried:  try > 0,
				Latency:  time.Since(start),
			}
		}

		lastErr = r.Err
	}

	if b.log != nil {
		b.log.Error(fmt.Sprintf("breaker: exhausted after max number of retries %d. fail :(", b.maxTries))
	}

	return Outcome{
		Err:      superr.New(ErrHitMaxRetries, lastErr),
		Attempts: b.maxTries + 1,
		Retried:  b.maxTries > 0,
		Latency:  time.Since(start),
	}
}

// applyJitter randomises duration by ±jitter fraction. With jitter=0 it
// returns d unchanged (fully deterministic, backward-compatible).
func (b *Breaker) applyJitter(d time.Duration) time.Duration {
	if b.jitter <= 0 {
		return d
	}
	// multiplier in [1-jitter, 1+jitter]
	multiplier := 1 - b.jitter + 2*b.jitter*cryptoFloat64()
	return time.Duration(float64(d) * multiplier)
}

// cryptoFloat64 returns a cryptographically random float64 in [0, 1).
func cryptoFloat64() float64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return float64(binary.LittleEndian.Uint64(b[:])>>11) / (1 << 53)
}

// sleepContext sleeps for d or until ctx is cancelled. Returns true if the
// full sleep completed.
func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Do is a convenience wrapper that creates a one-shot Breaker and calls Do on it.
func Do(ctx context.Context, fn func() error, log *slog.Logger, backoff time.Duration, factor float64, maxTries int, opts ...Option) error {
	return New(log, backoff, factor, maxTries, opts...).Do(ctx, fn)
}
