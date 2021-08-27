package breaker

import (
	"context"
	"errors"
	"time"

	"github.com/goware/superr"
)

var ErrExhaustedRetries = errors.New("breaker: exhausted all retry attempts")

// ExpBackoffRetry will wait `backoff*factor**retry` up to `maxTries`
// `maxTries = 1` means retry only once when an error occurs.
func ExpBackoffRetry(ctx context.Context, fn func(ctx context.Context) error, log Logger, backoff time.Duration, factor float64, maxTries int) error {
	delay := float64(backoff)
	try := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		t := time.Now()
		err := fn(ctx)
		if err == nil {
			return nil
		}

		// If we failed for some reason, exp backoff and retry.

		// Reset try counter if some time has passed.
		if time.Now().Sub(t) > 2*backoff {
			delay = float64(backoff)
			try = 0
		}

		if log != nil {
			log.Warnf("breaker: fn failed: '%v' - backing off for %v and trying again (retry #%d)", err, time.Duration(int64(delay)).String(), try+1)
		}

		// Move on if we have tried a few times.
		if try >= maxTries {
			if log != nil {
				log.Errorf("breaker: exhausted after max number of retries %d. fail :(", maxTries)
			}
			return superr.New(ErrExhaustedRetries, err)
		}

		// Sleep and try again.
		time.Sleep(time.Duration(int64(delay)))
		delay *= factor
		try++
	}
}
