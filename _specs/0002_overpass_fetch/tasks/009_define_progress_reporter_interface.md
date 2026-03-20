---
# Task 009: Define ProgressReporter Interface

## Summary

Define a `ProgressReporter` interface in the `quality` package that represents a lifecycle-aware progress sink. Provide a no-op implementation so callers that don't need progress reporting have a safe default.

## Dependencies

None — this is a foundational task that other tasks depend on.

## Detailed Directions

### 1. Add the interface and no-op to the `quality` package

Create a new file `quality/progress.go` with the following content:

```go
package quality

// ProgressReporter is implemented by types that want to receive progress
// events during a tiled fetch. All methods must be safe to call on a nil
// receiver or a no-op implementation.
type ProgressReporter interface {
    // SetTotal is called once, before the fetch loop begins, with the total
    // number of tiles to process.
    SetTotal(n int)

    // Tick is called once per tile after it has been processed (whether from
    // cache or from a live fetch). cached is true when the tile was served
    // from the local cache.
    Tick(cached bool)

    // Done is called after all tiles have been processed. Implementations
    // should use this to finalize any output (e.g., print a trailing newline).
    Done()
}

// NoopProgressReporter is a ProgressReporter that discards all events.
// It is the safe default when no progress output is desired.
type NoopProgressReporter struct{}

func (NoopProgressReporter) SetTotal(_ int)    {}
func (NoopProgressReporter) Tick(_ bool)       {}
func (NoopProgressReporter) Done()             {}
```

### 2. Verify the file compiles

Run the following to confirm the new file compiles cleanly with the rest of the package:

```
go build ./quality/...
```

## Acceptance Criteria

- [ ] `quality/progress.go` exists and is part of `package quality`.
- [ ] `ProgressReporter` interface has exactly three methods: `SetTotal(int)`, `Tick(bool)`, `Done()`.
- [ ] `NoopProgressReporter` implements `ProgressReporter` (verified by compiler).
- [ ] `go build ./quality/...` passes with no errors.
- [ ] No existing tests are broken (`go test ./quality/...`).

## Notes

- Keep this file small and focused — no rendering logic belongs here.
- The `bool` parameter on `Tick` (rather than separate `TickCached`/`TickFetched` methods) keeps the interface minimal while still giving renderers enough information to display cache-hit vs. live-fetch counts.

---
