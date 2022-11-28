package breaker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/goware/superr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockLogger struct {
	mock.Mock
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

	// fails each time
	mLog.On("Warnf", mock.Anything, mock.Anything).Return().Times(3)
	mLog.On("Errorf", mock.Anything, mock.Anything).Return().Times(1)

	err := br.Do(context.Background(), func() error {
		return fmt.Errorf("error")
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHitMaxRetries))

	mLog.AssertExpectations(t)

	// fails each time with long pause
	mLog.On("Warnf", mock.Anything, mock.Anything).Return().Times(3)
	mLog.On("Errorf", mock.Anything, mock.Anything).Return().Times(1)

	err = br.Do(context.Background(), func() error {
		time.Sleep(200 * time.Millisecond)
		return fmt.Errorf("error")
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHitMaxRetries))

	mLog.AssertExpectations(t)

	// doesn't fail
	err = br.Do(context.Background(), func() error {
		return nil
	})
	require.NoError(t, err)

	mLog.AssertExpectations(t)

	// fails one time
	mLog.On("Warnf", mock.Anything, mock.Anything).Return().Once()

	count := 0
	err = br.Do(context.Background(), func() error {
		defer func() { count++ }()

		if count == 0 {
			return fmt.Errorf("error")
		}
		return nil
	})
	require.NoError(t, err)

	mLog.AssertExpectations(t)

	// context canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = br.Do(ctx, func() error {
		return nil
	})
	require.Error(t, err)

	mLog.AssertExpectations(t)

	// ErrFatal break circuit right away
	err = br.Do(context.Background(), func() error {
		return superr.Wrap(ErrFatal, fmt.Errorf("error"))
	})
	require.Error(t, err)

	mLog.AssertExpectations(t)
}
