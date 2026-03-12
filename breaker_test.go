package breaker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/goware/superr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogHandler is a custom slog.Handler that captures log records for testing
type TestLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func NewTestLogHandler() *TestLogHandler {
	return &TestLogHandler{
		records: make([]slog.Record, 0),
	}
}

func (h *TestLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *TestLogHandler) Handle(ctx context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record)
	return nil
}

func (h *TestLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *TestLogHandler) WithGroup(name string) slog.Handler {
	return h
}

// CountLevel returns the number of log records at the specified level
func (h *TestLogHandler) CountLevel(level slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for _, record := range h.records {
		if record.Level == level {
			count++
		}
	}
	return count
}

// Reset clears all captured log records
func (h *TestLogHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = h.records[:0]
}

func TestBreakerDo(t *testing.T) {
	handler := NewTestLogHandler()
	logger := slog.New(handler)

	br := New(logger, 100*time.Millisecond, 2, 3)

	t.Run("FailsEachTime", func(t *testing.T) {
		handler.Reset()

		err := br.Do(context.Background(), func() error {
			return fmt.Errorf("error")
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrHitMaxRetries))

		// Should have 3 warning messages (one for each retry) and 1 error message (max retries reached)
		assert.Equal(t, 3, handler.CountLevel(slog.LevelWarn))
		assert.Equal(t, 1, handler.CountLevel(slog.LevelError))
	})

	t.Run("FailsEachTimeWithLongWait", func(t *testing.T) {
		handler.Reset()

		err := br.Do(context.Background(), func() error {
			time.Sleep(200 * time.Millisecond)
			return fmt.Errorf("error")
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrHitMaxRetries))

		// Should have 3 warning messages (one for each retry) and 1 error message (max retries reached)
		assert.Equal(t, 3, handler.CountLevel(slog.LevelWarn))
		assert.Equal(t, 1, handler.CountLevel(slog.LevelError))
	})

	t.Run("DoesntFail", func(t *testing.T) {
		handler.Reset()

		err := br.Do(context.Background(), func() error {
			return nil
		})
		require.NoError(t, err)

		// Should have no log messages since it succeeded on first try
		assert.Equal(t, 0, handler.CountLevel(slog.LevelWarn))
		assert.Equal(t, 0, handler.CountLevel(slog.LevelError))
	})

	t.Run("FailsOneTime", func(t *testing.T) {
		handler.Reset()

		count := 0
		err := br.Do(context.Background(), func() error {
			defer func() { count++ }()

			if count == 0 {
				return fmt.Errorf("error")
			}
			return nil
		})
		require.NoError(t, err)

		// Should have 1 warning message (for the first failure) and no error messages
		assert.Equal(t, 1, handler.CountLevel(slog.LevelWarn))
		assert.Equal(t, 0, handler.CountLevel(slog.LevelError))
	})

	t.Run("FailsContextCanceled", func(t *testing.T) {
		handler.Reset()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := br.Do(ctx, func() error {
			return nil
		})
		require.Error(t, err)

		// Should have no log messages since context was canceled before any retries
		assert.Equal(t, 0, handler.CountLevel(slog.LevelWarn))
		assert.Equal(t, 0, handler.CountLevel(slog.LevelError))
	})

	t.Run("FailsErrFatal", func(t *testing.T) {
		handler.Reset()

		err := br.Do(context.Background(), func() error {
			return superr.Wrap(ErrFatal, fmt.Errorf("error"))
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrFatal))

		// Should have no log messages since fatal errors don't trigger retries
		assert.Equal(t, 0, handler.CountLevel(slog.LevelWarn))
		assert.Equal(t, 0, handler.CountLevel(slog.LevelError))
	})

	t.Run("WithNilLogger", func(t *testing.T) {
		// Test that breaker works fine with nil logger
		brNil := New(nil, 100*time.Millisecond, 2, 2)

		err := brNil.Do(context.Background(), func() error {
			return fmt.Errorf("error")
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrHitMaxRetries))
	})
}

func TestBreakerJitter(t *testing.T) {
	t.Run("NoJitterIsDeterministic", func(t *testing.T) {
		br := New(nil, 100*time.Millisecond, 1, 3) // factor=1, no jitter

		var sleepDurations []time.Duration
		start := time.Now()
		count := 0

		br.Do(context.Background(), func() error {
			now := time.Now()
			if count > 0 {
				sleepDurations = append(sleepDurations, now.Sub(start))
			}
			start = now
			count++
			return fmt.Errorf("fail")
		})

		// With factor=1 and no jitter, all delays should be ~100ms
		for _, d := range sleepDurations {
			assert.InDelta(t, 100*time.Millisecond, d, float64(30*time.Millisecond),
				"without jitter, delay should be close to backoff")
		}
	})

	t.Run("WithJitterAddsRandomisation", func(t *testing.T) {
		br := New(nil, 200*time.Millisecond, 1, 10, WithJitter(0.5)) // ±50% jitter

		var sleepDurations []time.Duration
		start := time.Now()
		count := 0

		br.Do(context.Background(), func() error {
			now := time.Now()
			if count > 0 {
				sleepDurations = append(sleepDurations, now.Sub(start))
			}
			start = now
			count++
			return fmt.Errorf("fail")
		})

		// With 50% jitter on 200ms, delays should be in [100ms, 300ms]
		for _, d := range sleepDurations {
			assert.GreaterOrEqual(t, d, 80*time.Millisecond, "jittered delay should be >= lower bound (with tolerance)")
			assert.LessOrEqual(t, d, 340*time.Millisecond, "jittered delay should be <= upper bound (with tolerance)")
		}

		// Verify that not all delays are identical (statistical: extremely unlikely with ±50%)
		allSame := true
		if len(sleepDurations) > 1 {
			first := sleepDurations[0].Truncate(10 * time.Millisecond)
			for _, d := range sleepDurations[1:] {
				if d.Truncate(10*time.Millisecond) != first {
					allSame = false
					break
				}
			}
		}
		assert.False(t, allSame, "with 50%% jitter, delays should vary")
	})

	t.Run("JitterClampedToValidRange", func(t *testing.T) {
		// Negative jitter should be clamped to 0
		brNeg := New(nil, 100*time.Millisecond, 1, 1, WithJitter(-0.5))
		assert.Equal(t, float64(0), brNeg.jitter)

		// Jitter > 1 should be clamped to 1
		brHigh := New(nil, 100*time.Millisecond, 1, 1, WithJitter(2.0))
		assert.Equal(t, float64(1), brHigh.jitter)

		// Valid jitter should pass through
		brOk := New(nil, 100*time.Millisecond, 1, 1, WithJitter(0.25))
		assert.Equal(t, 0.25, brOk.jitter)
	})
}

func TestBreakerContextAwareSleep(t *testing.T) {
	t.Run("CancelDuringSleep", func(t *testing.T) {
		br := New(nil, 5*time.Second, 1, 3) // long backoff so we can cancel mid-sleep

		ctx, cancel := context.WithCancel(context.Background())

		count := 0
		start := time.Now()
		err := br.Do(ctx, func() error {
			count++
			if count == 1 {
				// First call fails — breaker will sleep 5s. Cancel after 100ms.
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()
				return fmt.Errorf("transient")
			}
			return nil
		})

		elapsed := time.Since(start)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Less(t, elapsed, 1*time.Second, "should have returned quickly after cancel, not waited full backoff")
		assert.Equal(t, 1, count, "fn should have been called only once before cancel took effect")
	})

	t.Run("TimeoutDuringSleep", func(t *testing.T) {
		br := New(nil, 5*time.Second, 1, 3)
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()

		count := 0
		err := br.Do(ctx, func() error {
			count++
			return fmt.Errorf("transient")
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 1, count)
	})
}

func TestDoWithOutcome(t *testing.T) {
	t.Run("SuccessOnFirstAttempt", func(t *testing.T) {
		br := New(nil, 100*time.Millisecond, 2, 3)

		out := br.DoWithOutcome(context.Background(), func() error {
			return nil
		})

		assert.NoError(t, out.Err)
		assert.Equal(t, 1, out.Attempts)
		assert.False(t, out.Retried)
		assert.Greater(t, out.Latency, time.Duration(0))
	})

	t.Run("SuccessAfterRetries", func(t *testing.T) {
		br := New(nil, 50*time.Millisecond, 1, 3)

		count := 0
		out := br.DoWithOutcome(context.Background(), func() error {
			count++
			if count < 3 {
				return fmt.Errorf("transient")
			}
			return nil
		})

		assert.NoError(t, out.Err)
		assert.Equal(t, 3, out.Attempts)
		assert.True(t, out.Retried)
		assert.GreaterOrEqual(t, out.Latency, 100*time.Millisecond) // at least 2 backoff sleeps
	})

	t.Run("ExhaustedRetries", func(t *testing.T) {
		br := New(nil, 50*time.Millisecond, 1, 2)

		out := br.DoWithOutcome(context.Background(), func() error {
			return fmt.Errorf("fail")
		})

		assert.Error(t, out.Err)
		assert.ErrorIs(t, out.Err, ErrHitMaxRetries)
		assert.Equal(t, 3, out.Attempts) // 1 initial + 2 retries
		assert.True(t, out.Retried)
	})

	t.Run("FatalError", func(t *testing.T) {
		br := New(nil, 50*time.Millisecond, 1, 5)

		out := br.DoWithOutcome(context.Background(), func() error {
			return superr.Wrap(ErrFatal, fmt.Errorf("bad request"))
		})

		assert.Error(t, out.Err)
		assert.ErrorIs(t, out.Err, ErrFatal)
		assert.Equal(t, 1, out.Attempts)
		assert.False(t, out.Retried)
	})

	t.Run("ContextCanceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		br := New(nil, 50*time.Millisecond, 1, 3)
		out := br.DoWithOutcome(ctx, func() error {
			return nil
		})

		assert.ErrorIs(t, out.Err, context.Canceled)
		assert.Equal(t, 0, out.Attempts)
	})

	t.Run("LatencyTracksFullDuration", func(t *testing.T) {
		br := New(nil, 100*time.Millisecond, 1, 1)

		count := 0
		out := br.DoWithOutcome(context.Background(), func() error {
			count++
			if count == 1 {
				return fmt.Errorf("transient")
			}
			return nil
		})

		assert.NoError(t, out.Err)
		assert.GreaterOrEqual(t, out.Latency, 90*time.Millisecond)
	})
}

func TestPackageLevelDo(t *testing.T) {
	t.Run("WithJitter", func(t *testing.T) {
		count := 0
		err := Do(context.Background(), func() error {
			count++
			if count < 2 {
				return fmt.Errorf("transient")
			}
			return nil
		}, nil, 50*time.Millisecond, 1, 3, WithJitter(0.1))

		assert.NoError(t, err)
		assert.Equal(t, 2, count)
	})
}

func TestApplyJitter(t *testing.T) {
	t.Run("ZeroJitter", func(t *testing.T) {
		br := &Breaker{jitter: 0}
		d := 100 * time.Millisecond
		assert.Equal(t, d, br.applyJitter(d))
	})

	t.Run("FullJitter", func(t *testing.T) {
		br := &Breaker{jitter: 1.0}
		base := 100 * time.Millisecond

		// Run many times to check bounds
		for range 1000 {
			got := br.applyJitter(base)
			assert.GreaterOrEqual(t, got, time.Duration(0), "with jitter=1.0, min is 0")
			assert.LessOrEqual(t, got, 2*base, "with jitter=1.0, max is 2x base")
		}
	})

	t.Run("SmallJitter", func(t *testing.T) {
		br := &Breaker{jitter: 0.1}
		base := 1000 * time.Millisecond

		for range 1000 {
			got := br.applyJitter(base)
			assert.GreaterOrEqual(t, got, 900*time.Millisecond)
			assert.LessOrEqual(t, got, 1100*time.Millisecond)
		}
	})
}

func TestSleepContext(t *testing.T) {
	t.Run("FullSleep", func(t *testing.T) {
		ok := sleepContext(context.Background(), 50*time.Millisecond)
		assert.True(t, ok)
	})

	t.Run("CanceledBeforeSleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ok := sleepContext(ctx, 5*time.Second)
		assert.False(t, ok)
	})

	t.Run("CanceledDuringSleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		ok := sleepContext(ctx, 5*time.Second)
		elapsed := time.Since(start)

		assert.False(t, ok)
		assert.Less(t, elapsed, 1*time.Second)
	})
}

func TestDefaultBreaker(t *testing.T) {
	t.Run("WithLogger", func(t *testing.T) {
		handler := NewTestLogHandler()
		logger := slog.New(handler)
		br := Default(logger)
		assert.NotNil(t, br.log)
	})

	t.Run("WithoutLogger", func(t *testing.T) {
		br := Default()
		assert.Nil(t, br.log)
		assert.Equal(t, 1*time.Second, br.backoff)
		assert.Equal(t, float64(2), br.factor)
		assert.Equal(t, 15, br.maxTries)
		assert.Equal(t, float64(0), br.jitter)
	})
}
