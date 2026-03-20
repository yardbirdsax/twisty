package quality

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTransient = errors.New("transient error")

func TestRetrySucceedsFirstAttempt(t *testing.T) {
	calls := 0
	err := retryWithBackoff(context.Background(), 3, 10*time.Millisecond, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected fn called once, got %d", calls)
	}
}

func TestRetrySucceedsAfterTransientFailure(t *testing.T) {
	calls := 0
	err := retryWithBackoff(context.Background(), 3, 10*time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return errTransient
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected fn called 3 times, got %d", calls)
	}
}

func TestRetryExhaustsAllAttempts(t *testing.T) {
	calls := 0
	err := retryWithBackoff(context.Background(), 3, 10*time.Millisecond, func() error {
		calls++
		return errTransient
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, errTransient) {
		t.Errorf("expected errTransient, got %v", err)
	}
	// 1 initial + 3 retries = 4 total calls
	if calls != 4 {
		t.Errorf("expected fn called 4 times, got %d", calls)
	}
}

func TestRetryNonRetryableStopsImmediately(t *testing.T) {
	calls := 0
	sentinel := errors.New("permanent error")
	err := retryWithBackoff(context.Background(), 3, 10*time.Millisecond, func() error {
		calls++
		return NonRetryable(sentinel)
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error via Unwrap, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected fn called once (no retries), got %d", calls)
	}
}

func TestRetryRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	// Cancel the context after the first call so the wait is interrupted.
	err := retryWithBackoff(ctx, 3, 200*time.Millisecond, func() error {
		calls++
		if calls == 1 {
			// Cancel context while we're about to wait for the delay.
			cancel()
		}
		return errTransient
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected fn called once before cancellation, got %d", calls)
	}
}

func TestRetryBackoffDelays(t *testing.T) {
	initialDelay := 20 * time.Millisecond
	calls := 0
	timestamps := make([]time.Time, 0, 4)

	retryWithBackoff(context.Background(), 3, initialDelay, func() error { //nolint:errcheck
		timestamps = append(timestamps, time.Now())
		calls++
		return errTransient
	})

	if calls != 4 {
		t.Fatalf("expected 4 calls, got %d", calls)
	}

	// Verify that delays approximately double.
	// delay[0] ≈ 20ms, delay[1] ≈ 40ms, delay[2] ≈ 80ms
	for i := 1; i < len(timestamps); i++ {
		elapsed := timestamps[i].Sub(timestamps[i-1])
		expected := initialDelay * time.Duration(1<<uint(i-1))
		// Allow up to 50% over the expected delay to accommodate scheduling jitter.
		lower := expected * 8 / 10 // 80% of expected
		upper := expected * 4      // generous upper bound for slow CI
		if elapsed < lower || elapsed > upper {
			t.Errorf("delay %d: elapsed %v, expected ~%v (range [%v, %v])", i, elapsed, expected, lower, upper)
		}
	}
}
