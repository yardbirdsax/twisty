# Task 002: Implement Counting Reader

## Summary

Create a `countingReader` that wraps an `io.Reader` and reports bytes read to a `ConvertProgress` interface. This is the mechanism by which byte-based progress percentage is computed during conversion, without modifying the `PBFScanner` internals.

## Dependencies

Task 001 -- requires the `ConvertProgress` interface.

## Detailed Directions

### 1. Create `osmconv/counting_reader.go`

Implement a small struct that wraps an `io.Reader`:

```go
type countingReader struct {
    r         io.Reader
    fileIndex int
    progress  ConvertProgress
}

func newCountingReader(r io.Reader, fileIndex int, progress ConvertProgress) *countingReader {
    return &countingReader{r: r, fileIndex: fileIndex, progress: progress}
}

func (cr *countingReader) Read(p []byte) (int, error) {
    n, err := cr.r.Read(p)
    if n > 0 {
        cr.progress.BytesRead(cr.fileIndex, int64(n))
    }
    return n, err
}
```

### 2. Write unit tests in `osmconv/counting_reader_test.go`

- Verify that bytes read from the underlying reader are correctly reported to the `ConvertProgress` implementation.
- Use a mock/spy `ConvertProgress` that records calls to `BytesRead`.
- Test with multiple reads of varying sizes.
- Test that `BytesRead` is not called when `n == 0`.
- Test that errors from the underlying reader are passed through correctly.
- Test that `io.EOF` is passed through.

### 3. Verify compile and test

Run `go build ./osmconv/...` and `go test ./osmconv/...`.

## Acceptance Criteria

- [ ] `osmconv/counting_reader.go` exists with the `countingReader` type.
- [ ] `countingReader` implements `io.Reader`.
- [ ] Bytes read are reported to the `ConvertProgress` via `BytesRead(fileIndex, n)`.
- [ ] Errors from the underlying reader pass through unchanged.
- [ ] Unit tests cover normal reads, zero-byte reads, and error propagation.
- [ ] All existing tests continue to pass.

## Notes

- The `countingReader` is unexported -- it is an internal implementation detail of the `osmconv` package. It will be used by the updated `Convert` function or by the caller when constructing scanners.
- Report every `Read()` call rather than batching. The terminal renderer (Task 004) will handle rate-limiting display updates.

---

# Task 002 Review: Implement Counting Reader

**Reviewer:** Principal Engineer
**Date:** 2026-04-27
**Verdict:** APPROVED

---

## Summary

Implements `countingReader`, an internal `io.Reader` wrapper that reports byte counts to `ConvertProgress.BytesRead` on each read.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/osmconv/counting_reader.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/osmconv/counting_reader_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `osmconv/counting_reader.go` exists with `countingReader` type | PASS |
| `countingReader` implements `io.Reader` | PASS |
| Bytes reported via `BytesRead(fileIndex, n)` | PASS |
| Errors from underlying reader pass through unchanged | PASS |
| Tests cover normal reads, zero-byte reads, error propagation | PASS |
| All existing tests continue to pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## Good Practices Observed

(Omitted per review instructions.)

---

## Verification Commands Run

```bash
make test   # all packages pass including osmconv
make lint   # sandbox permission error unrelated to this task's code; go vet itself not run
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met. Tests pass. Implementation matches the spec exactly.
