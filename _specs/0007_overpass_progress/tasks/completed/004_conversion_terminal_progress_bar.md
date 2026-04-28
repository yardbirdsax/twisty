# Task 004: Implement Terminal Progress Bar for Conversion

## Summary

Create a terminal progress bar that implements `ConvertProgress` and renders in-place percentage progress during PBF-to-BZ2 conversion. Also implement a log-line fallback for non-terminal stderr. This is the user-facing output for Phase 1.

## Dependencies

Task 001 -- requires the `ConvertProgress` interface.

## Detailed Directions

### 1. Create the terminal conversion progress bar

Add a new type in `main.go` (or a new `progress.go` file at the root level, alongside the existing `termProgressBar`). The type should implement `osmconv.ConvertProgress`.

```go
type termConvertBar struct {
    mu           sync.Mutex
    w            io.Writer
    fileNames    []string
    fileSizes    []int64
    bytesRead    []int64
    totalSize    int64
    totalRead    int64
    objectCount  int
    startTime    time.Time
    lastRender   time.Time
}
```

### 2. Implement the `ConvertProgress` methods

- **`SetFiles(names, sizes)`**: Store file names and sizes. Compute `totalSize` as sum of all sizes. Record `startTime`.
- **`BytesRead(fileIndex, n)`**: Increment `bytesRead[fileIndex]` and `totalRead` by `n`. Call `render()` if >= 100ms since last render.
- **`ObjectsWritten(n)`**: Increment `objectCount` by `n`. (No render needed here -- `BytesRead` drives the display.)
- **`Done()`**: Render a final summary line with a trailing newline:
  ```
  Conversion complete: 3,412,500 objects written, 8m12s
  ```

### 3. Implement the `render()` method

Render an in-place progress bar using carriage return (`\r`):

```
Converting us-northeast.pbf (2/3): [=========>             ] 38% | 1,245,000 objects | 2m31s
```

Components:
- **Label**: Current file name and file index (e.g., `(2/3)`). Determine "current file" as the file with the most recent `BytesRead` call, or the file whose `bytesRead < fileSize` with the lowest index.
- **Bar**: 30-character bar matching the style in the existing `termProgressBar.render()`.
- **Percentage**: `totalRead / totalSize * 100`, capped at 100%.
- **Object count**: Formatted with comma separators.
- **Elapsed time**: Since `startTime`, formatted as `Xm Xs` or `Xs`.

Rate-limit rendering to at most once per 100ms to prevent terminal flicker.

### 4. Implement log-line fallback for non-terminal stderr

Create a second implementation of `ConvertProgress` that prints a line every 100,000 objects (matching current behavior):

```go
type logConvertProgress struct {
    w           io.Writer
    objectCount int
    lastReport  int
}
```

- `ObjectsWritten(n)`: Increment count. If count crosses a 100,000 boundary since last report, print `"  N objects written\n"`.
- `Done()`: Print final count.
- Other methods: no-op.

### 5. Add a factory function

```go
func newConvertProgress(w *os.File) osmconv.ConvertProgress {
    if isTerminal(w) {
        return &termConvertBar{w: w}
    }
    return &logConvertProgress{w: w}
}
```

Use the existing `isTerminal()` helper.

### 6. Write unit tests

- Test `termConvertBar` with a `bytes.Buffer` as writer:
  - Verify `SetFiles` stores sizes correctly.
  - Verify `BytesRead` updates totals.
  - Verify `Done()` output contains "Conversion complete" and object count.
- Test `logConvertProgress`:
  - Verify it prints at 100,000 object intervals.
  - Verify `Done()` prints final count.
- Test the factory returns the correct type based on terminal detection.

## Acceptance Criteria

- [ ] Terminal progress bar renders in-place with percentage, file name, object count, and elapsed time.
- [ ] Log-line fallback prints every 100,000 objects when stderr is not a terminal.
- [ ] Render rate is capped at 100ms.
- [ ] `Done()` prints a summary line with total objects and elapsed time.
- [ ] Both implementations satisfy the `ConvertProgress` interface (compile-time checks).
- [ ] Unit tests pass.
- [ ] `go build ./...` succeeds.

## Notes

- Follow the rendering style of the existing `termProgressBar` in `main.go` for visual consistency (same bar width, same `\r` approach).
- Thread safety via `sync.Mutex` is required since `BytesRead` and `ObjectsWritten` may be called from the conversion goroutine while render timing runs.
- For comma-formatting integers, use `golang.org/x/text/message` or a simple helper function. Check what the project already uses before adding a dependency.

---

# Task 004 Review: Implement Terminal Progress Bar for Conversion

**Reviewer:** Principal Engineer
**Date:** 2026-04-27
**Verdict:** APPROVED

---

## Summary

Implements `termConvertBar` and `logConvertProgress` types satisfying `osmconv.ConvertProgress`, plus a `newConvertProgress` factory, `formatInt`, and `formatElapsed` helpers, all in `main.go`. Unit tests in `main_test.go`.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| Terminal progress bar renders in-place with percentage, file name, object count, elapsed time | PASS |
| Log-line fallback prints every 100,000 objects | PASS |
| Render rate capped at 100ms | PASS |
| `Done()` prints summary with total objects and elapsed time | PASS |
| Both implementations satisfy `ConvertProgress` interface (compile-time checks) | PASS |
| Unit tests pass | PASS |
| `go build ./...` succeeds | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test -short ./...   # PASS -- all packages
go vet ./...           # PASS -- no issues
go build ./...         # PASS -- clean build
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The three issues from the prior review (totalRead bounds check, rate-limit test coverage, logConvertProgress thread safety) have been resolved. Code is clean, idiomatic, and well-tested.
