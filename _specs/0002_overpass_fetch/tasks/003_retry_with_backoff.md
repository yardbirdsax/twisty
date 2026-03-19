# Task 003: Retry Helper with Exponential Backoff

## Summary

Implement a reusable retry-with-exponential-backoff helper function that will be used by the per-tile Overpass fetch. The helper is decoupled from fetch logic, accepts a `context.Context` for future cancellation support, and distinguishes between retryable and non-retryable errors.

## Dependencies

None — this is a standalone utility. Can be done in parallel with Task 002.

## Detailed Directions

### 1. Define a retryable error type

- In `quality/tilefetch.go` (or a new file `quality/retry.go` if preferred for clarity), define a way to distinguish retryable from non-retryable errors:
  ```go
  type nonRetryableError struct {
      err error
  }

  func (e *nonRetryableError) Error() string { return e.err.Error() }
  func (e *nonRetryableError) Unwrap() error { return e.err }

  func NonRetryable(err error) error {
      return &nonRetryableError{err: err}
  }

  func isNonRetryable(err error) bool {
      var nre *nonRetryableError
      return errors.As(err, &nre)
  }
  ```

### 2. Implement `retryWithBackoff`

- Write the retry helper:
  ```go
  func retryWithBackoff(ctx context.Context, maxRetries int, initialDelay time.Duration, fn func() error) error
  ```
- Behavior:
  1. Call `fn()`. If it returns nil, return nil immediately.
  2. If `fn()` returns a `nonRetryableError`, return the error immediately (no retry).
  3. Otherwise, wait for `initialDelay` (respecting context cancellation), then retry.
  4. Double the delay each attempt: 2s → 4s → 8s.
  5. After `maxRetries` failed attempts, return the last error.
  6. If the context is cancelled during a delay, return `ctx.Err()`.
- The function calls `fn` a total of `maxRetries + 1` times (1 initial + maxRetries retries).

### 3. Use `time.After` or `time.NewTimer` with context select

- For the delay between retries, use a select on the context and a timer:
  ```go
  select {
  case <-time.After(delay):
      // continue to retry
  case <-ctx.Done():
      return ctx.Err()
  }
  ```

### 4. Write unit tests

- In `quality/tilefetch_test.go` (or `quality/retry_test.go`), write tests:
  - `TestRetrySucceedsFirstAttempt`: fn succeeds immediately, verify called once.
  - `TestRetrySucceedsAfterTransientFailure`: fn fails twice, succeeds third time.
  - `TestRetryExhaustsAllAttempts`: fn always fails, verify all retries attempted and final error returned.
  - `TestRetryNonRetryableStopsImmediately`: fn returns `NonRetryable(err)`, verify no retries.
  - `TestRetryRespectsContextCancellation`: Cancel context during delay, verify returns `context.Canceled`.
  - `TestRetryBackoffDelays`: Verify that delays approximately double (use short durations like 10ms for tests).
- Use a counter variable and/or timestamps to verify retry behavior.

## Acceptance Criteria

- [ ] `retryWithBackoff` function implemented with exponential backoff
- [ ] Non-retryable errors cause immediate return without retry
- [ ] Context cancellation interrupts the retry loop
- [ ] Delays double on each retry attempt
- [ ] All unit tests pass
- [ ] Function is generic (not coupled to HTTP/Overpass logic)

## Notes

- Use short delays in tests (e.g., 10ms initial) to keep test suite fast.
- The PRD specifies 3 retries with 2s/4s/8s delays. The caller will pass `maxRetries=3, initialDelay=2*time.Second`. The helper itself is parameterized.
- This helper will be used in Task 005 when implementing the per-tile fetch.
