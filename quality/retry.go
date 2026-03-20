package quality

import (
	"context"
	"errors"
	"time"
)

// nonRetryableError wraps an error to signal that it should not be retried.
type nonRetryableError struct {
	err error
}

func (e *nonRetryableError) Error() string { return e.err.Error() }
func (e *nonRetryableError) Unwrap() error { return e.err }

// NonRetryable wraps err to prevent retryWithBackoff from retrying.
func NonRetryable(err error) error {
	return &nonRetryableError{err: err}
}

func isNonRetryable(err error) bool {
	var nre *nonRetryableError
	return errors.As(err, &nre)
}

// retryWithBackoff calls fn up to maxRetries+1 times (1 initial attempt plus maxRetries retries).
// Between attempts it waits initialDelay, doubling the delay each time. If the context is
// cancelled during a wait, ctx.Err() is returned immediately. If fn returns a NonRetryable
// error, that error is returned without further attempts.
func retryWithBackoff(ctx context.Context, maxRetries int, initialDelay time.Duration, fn func() error) error {
	delay := initialDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if isNonRetryable(lastErr) {
			return lastErr
		}
		if attempt < maxRetries {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				// continue to next attempt
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
			delay *= 2
		}
	}

	return lastErr
}
