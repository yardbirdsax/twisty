---
# Task 011: Add Pipeline Observability with slog Timing Logs

## Summary

Instrument each stage of the main pipeline with structured, human-readable timing logs using
Go's `log/slog` package. The goal is to capture per-stage elapsed time and key data-size
metrics (route count, point counts, way count) so that the slowest stage can be identified
empirically. Logs should be emitted to stderr and should not alter the existing stdout output.

## Dependencies

Task 010 (pipeline integration) — this task instruments the existing pipeline.

## Detailed Directions

### 1. Add a `-v` verbose flag to `main.go`

Add a boolean flag `-v` that enables debug-level slog output. When `-v` is not set, no
timing logs are emitted (existing `fmt.Print*` output is unchanged).

```go
verbose := flag.Bool("v", false, "Enable verbose timing logs to stderr")
```

### 2. Initialize a slog logger after flag parsing

After `flag.Parse()`, construct a `TextHandler` targeting `os.Stderr`. Use
`slog.LevelDebug` when `-v` is set.

```go
var logger *slog.Logger
if *verbose {
    handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })
    logger = slog.New(handler)
} else {
    logger = slog.New(slog.NewTextHandler(io.Discard, nil))
}
```

Add `"io"` to the import block.

### 3. Create a helper `stageTimer` in `main.go`

Add a small helper that records the start time of a stage and returns a closure that logs
the elapsed duration when called. Keep it local to `main.go`.

```go
func stageTimer(logger *slog.Logger, stage string, fields ...any) func(...any) {
    start := time.Now()
    logger.Debug("stage start", append([]any{"stage", stage}, fields...)...)
    return func(extra ...any) {
        args := append([]any{"stage", stage, "elapsed_ms", time.Since(start).Milliseconds()}, extra...)
        logger.Debug("stage done", args...)
    }
}
```

### 4. Instrument each pipeline stage in `main.go`

Wrap each stage with `stageTimer`. Pass data-size fields on the "done" call so the log
line includes context about how much work was done. The pattern for each stage is:

```go
done := stageTimer(logger, "stage-name", /* optional start-time fields */)
// ... existing stage code ...
done(/* optional result fields, e.g., "routes", len(routes) */)
```

Apply this to all eight stages:

| Stage | "stage" label | Extra fields on done |
|-------|--------------|----------------------|
| Resolve origin | `"geocode-origin"` | `"result", originResult.Display` |
| Resolve destination | `"geocode-dest"` | `"result", destResult.Display` |
| Fetch routes (OSRM) | `"fetch-routes"` | `"routes", len(routes)` |
| Score all routes | `"score-routes"` | `"routes", len(routes)` |
| Fetch ways (Overpass) | `"fetch-ways"` | `"ways", len(ways)` (log 0 on error) |
| Apply quality | `"apply-quality"` | `"routes", len(routes)` |
| Select route | `"select-route"` | `"selected_idx", selectedIdx` |
| Write GPX | `"write-gpx"` | `"path", *out` |

For the "fetch-routes" done call, also log the total point count across all routes:

```go
totalPts := 0
for _, r := range routes {
    totalPts += len(r.Points)
}
done("routes", len(routes), "total_points", totalPts)
```

For the "apply-quality" start call, include the way count and total point count so the
log shows the input size of the expensive operation:

```go
totalPts := 0
for _, r := range routes {
    totalPts += len(r.Points)
}
done := stageTimer(logger, "apply-quality", "routes", len(routes), "ways", len(ways), "total_points", totalPts)
quality.ApplyQuality(routes, ways)
done()
```

### 5. Add a total pipeline elapsed log

At the very top of `main()` (after logger initialization), capture `pipelineStart := time.Now()`.
After `printSummary`, add:

```go
logger.Debug("pipeline done", "total_elapsed_ms", time.Since(pipelineStart).Milliseconds())
```

### 6. Verify output format

When run with `-v`, log lines should look like:

```
time=2024-01-15T10:23:01.123Z level=DEBUG msg="stage start" stage=fetch-ways
time=2024-01-15T10:23:11.456Z level=DEBUG msg="stage done" stage=fetch-ways elapsed_ms=10333 ways=4821
time=2024-01-15T10:23:11.457Z level=DEBUG msg="stage start" stage=apply-quality routes=3 ways=4821 total_points=12045
time=2024-01-15T10:23:41.892Z level=DEBUG msg="stage done" stage=apply-quality elapsed_ms=30435
```

No changes should appear in stdout when `-v` is omitted.

## Acceptance Criteria

- [ ] `go build ./...` succeeds with no new compilation errors or warnings
- [ ] Running without `-v` produces identical stdout/stderr to the pre-task behavior
- [ ] Running with `-v` emits slog `DEBUG` lines to stderr for every pipeline stage
- [ ] Each "stage done" log line includes `elapsed_ms` and relevant size fields
- [ ] The "pipeline done" line reports total wall-clock time
- [ ] `go test ./...` passes

## Notes

- All timing logs go to stderr; stdout output (`fmt.Print*`) is unchanged.
- `slog.NewTextHandler` produces `key=value` pairs — human-readable without JSON.
- Do not add slog calls inside package-level functions (`ScoreAll`, `ApplyQuality`, etc.) yet;
  keep all instrumentation in `main.go` for this task. Per-function instrumentation may be
  added in a later optimization task if the stage granularity is insufficient.
- The `io.Discard` logger when `-v` is false means zero overhead in the non-verbose path.

---
# Task 011 Review: Add Pipeline Observability with slog Timing Logs

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

Task 011 instruments each stage of the main pipeline in `main.go` with structured timing logs using `log/slog`. A `-v` flag gates debug output to stderr; without it the logger discards all output. A `stageTimer` helper captures per-stage elapsed time and a `pipelineStart` variable captures total wall-clock time.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go build ./...` succeeds with no new compilation errors or warnings | PASS |
| Running without `-v` produces identical stdout/stderr to pre-task behavior | PASS |
| Running with `-v` emits slog `DEBUG` lines to stderr for every pipeline stage | PASS |
| Each "stage done" log line includes `elapsed_ms` and relevant size fields | PASS |
| The "pipeline done" line reports total wall-clock time | PASS |
| `go test ./...` passes | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Spec discrepancy resolved correctly:** The spec table listed `originResult.Display` and `destResult.Display`, but the actual struct field is `DisplayName`. The implementation correctly uses `DisplayName` rather than blindly following the erroneous table entry.

---

## Verification Commands Run

```bash
go build ./...        # clean, no output
make test             # all packages pass
make lint             # go vet clean, no output
```

---

## Final Verdict

**APPROVED**

All six acceptance criteria pass. Build, tests, and vet are clean. The implementation matches the authoritative code blocks in the spec.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
