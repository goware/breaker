package breaker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/goware/logger"
	"github.com/goware/superr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) With(args ...interface{}) logger.Logger {
	return nil
}

func (m *MockLogger) Debug(v ...interface{}) {
	m.Called(v)
}

func (m *MockLogger) Debugf(format string, v ...interface{}) {
	m.Called(format, v)
}

func (m *MockLogger) Info(v ...interface{}) {
	m.Called(v)
}

func (m *MockLogger) Infof(format string, v ...interface{}) {
	m.Called(format, v)
}

func (m *MockLogger) Warn(v ...interface{}) {
	m.Called(v)
}

func (m *MockLogger) Warnf(format string, v ...interface{}) {
	m.Called(format, v)
}

func (m *MockLogger) Error(v ...interface{}) {
	m.Called(v)
}

func (m *MockLogger) Errorf(format string, v ...interface{}) {
	m.Called(format, v)
}

func (m *MockLogger) Fatal(v ...interface{}) {
	m.Called(v)
}

func (m *MockLogger) Fatalf(format string, v ...interface{}) {
	m.Called(format, v)
}

func TestBreakerDo(t *testing.T) {
	mLog := &MockLogger{}

	br := New(mLog, 100*time.Millisecond, 2, 3)

	t.Run("FailsEachTime", func(t *testing.T) {
		// fails each time
		mLog.On("Warnf", mock.Anything, mock.Anything).Return().Times(3)
		mLog.On("Errorf", mock.Anything, mock.Anything).Return().Times(1)

		err := br.Do(context.Background(), func() error {
			return fmt.Errorf("error")
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrHitMaxRetries))

		mLog.AssertExpectations(t)
	})

	t.Run("FailsEachTimeWithLongWait", func(t *testing.T) {
		mLog.On("Warnf", mock.Anything, mock.Anything).Return().Times(3)
		mLog.On("Errorf", mock.Anything, mock.Anything).Return().Times(1)

		err := br.Do(context.Background(), func() error {
			time.Sleep(200 * time.Millisecond)
			return fmt.Errorf("error")
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrHitMaxRetries))

		mLog.AssertExpectations(t)
	})

	t.Run("DoesntFail", func(t *testing.T) {
		err := br.Do(context.Background(), func() error {
			return nil
		})
		require.NoError(t, err)

		mLog.AssertExpectations(t)
	})

	t.Run("FailsOneTime", func(t *testing.T) {
		mLog.On("Warnf", mock.Anything, mock.Anything).Return().Once()

		count := 0
		err := br.Do(context.Background(), func() error {
			defer func() { count++ }()

			if count == 0 {
				return fmt.Errorf("error")
			}
			return nil
		})
		require.NoError(t, err)

		mLog.AssertExpectations(t)
	})

	t.Run("FailsContextCanceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := br.Do(ctx, func() error {
			return nil
		})
		require.Error(t, err)

		mLog.AssertExpectations(t)
	})

	t.Run("FailsErrFatal", func(t *testing.T) {
		err := br.Do(context.Background(), func() error {
			return superr.Wrap(ErrFatal, fmt.Errorf("error"))
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrFatal))

		mLog.AssertExpectations(t)
	})
}
