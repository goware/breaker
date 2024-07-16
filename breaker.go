package breaker

import (
	"context"
	"errors"
	"time"

	"github.com/goware/logger"
	"github.com/goware/superr"
)

var (
	ErrFatal         = errors.New("breaker: fatal error")
	ErrHitMaxRetries = errors.New("breaker: hit max retries")
)

type Breaker struct {
	log      logger.Logger
	backoff  time.Duration
	factor   float64
	maxTries int
}

// use *slog.Logger as logger
func Default(optLog ...logger.Logger) *Breaker {
	var log logger.Logger
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

// use *slog.Logger as logger
func New(log logger.Logger, backoff time.Duration, factor float64, maxTries int) *Breaker {
	return &Breaker{
		log:      log,
		backoff:  backoff,
		factor:   factor,
		maxTries: maxTries,
	}
}

// Do is an exponential-backoff-retry caller which will wait `backoff*factor**retry` up to `maxTries`
// `maxTries = 1` means retry only once when an error occurs.
func (b *Breaker) Do(ctx context.Context, fn func() error) error {
	delay := float64(b.backoff)
	try := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := fn()
		if err == nil {
			return nil
		}

		// If we failed for some reason, exp backoff and retry.

		// Check if is fatal error and should stop immediately
		if errors.Is(err, ErrFatal) {
			return err
		}

		// Move on if we have tried a few times.
		if try >= b.maxTries {
			if b.log != nil {
				b.log.Errorf("breaker: exhausted after max number of retries maxTries=(%d)", b.maxTries)
			}
			return superr.New(ErrHitMaxRetries, err)
		}

		if b.log != nil {
			b.log.Warnf("breaker: fn failed, trying again backOffDelay=(%d), try=(%d), error=(%v)", time.Duration(int64(delay)).String(), try+1, err)
		}

		// Sleep and try again.
		time.Sleep(time.Duration(int64(delay)))
		delay *= b.factor
		try++
	}
}

// use *slog.Logger as logger
func Do(ctx context.Context, fn func() error, log logger.Logger, backoff time.Duration, factor float64, maxTries int) error {
	return New(log, backoff, factor, maxTries).Do(ctx, fn)
}
