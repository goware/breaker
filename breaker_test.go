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
