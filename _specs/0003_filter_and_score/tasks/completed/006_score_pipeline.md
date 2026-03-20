# Task 006: Score Pipeline Orchestration

## Summary

Wire together hard filter, curvature scoring, and deflection filter into a single pipeline function that processes all ways for a tile. This is the integration point that the score command and score cache will call.

## Dependencies

- Task 003 (hard filter — `HardFilter`)
- Task 004 (segment scoring — `ScoreWays`)
- Task 005 (deflection filter — `ApplyDeflectionFilter`)

## Detailed Directions

### 1. Create `quality/scorepipeline.go`

- Create the file `quality/scorepipeline.go` with package `quality`.
- Implement:

```go
// ScorePipelineResult holds the output of the scoring pipeline along with
// summary statistics.
type ScorePipelineResult struct {
    ScoredWays       []ScoredWay
    InputWays        int // total ways before filtering
    FilteredWays     int // ways that passed the hard filter
    TotalSegments    int // total scored segments
    ZeroedByDeflection int // segments zeroed by deflection filter
}

// RunScorePipeline executes stages 2–4 on a set of ways:
// hard filter → curvature scoring → deflection filter.
func RunScorePipeline(ways []Way) ScorePipelineResult
```

- Implementation:
  1. Record `InputWays = len(ways)`.
  2. Run `HardFilter(ways)` to get filtered ways. Record `FilteredWays`.
  3. Run `ScoreWays(filteredWays)` to get scored ways.
  4. Count total non-zero scored segments before deflection.
  5. Run `ApplyDeflectionFilter(scoredWays)`.
  6. Count segments that were zeroed by deflection (compare before/after counts).
  7. Return the result.

### 2. Write unit tests in `quality/scorepipeline_test.go`

- **End-to-end with mixed ways**: Provide a slice of ways including:
  - A way with `surface=gravel` (should be filtered out)
  - A way with `access=private` (should be filtered out)
  - A straight way with 5+ nodes (should score near zero)
  - A curvy way with 5+ nodes (should score > 0)
- Verify:
  - `InputWays` matches the input count.
  - `FilteredWays` excludes the filtered ways.
  - The curvy way has non-zero scored segments.
  - The straight way has zero-score segments.
  - Statistics are consistent (e.g., `TotalSegments` matches actual segment count).

- **Empty input**: returns zero-valued result with no scored ways.

- **All filtered**: all input ways are disqualified → `FilteredWays = 0`, no scored ways.

## Acceptance Criteria

- [ ] `quality/scorepipeline.go` exists with `RunScorePipeline` and `ScorePipelineResult`.
- [ ] `quality/scorepipeline_test.go` passes with `go test ./quality/...`.
- [ ] The pipeline executes hard filter → scoring → deflection in order.
- [ ] Summary statistics are accurate.
- [ ] Empty and all-filtered inputs are handled gracefully.

## Notes

- This function is intentionally simple — it just sequences the three stages and collects stats. Keeping it thin makes testing and debugging easier.
- The `ScorePipelineResult` stats will be used by the `score` subcommand (Task 008) for its console summary.

---
# Task 006 Review: Score Pipeline Orchestration

**Reviewer:** Senior Software Engineer Agent
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

Implements `RunScorePipeline` and `ScorePipelineResult` in `quality/scorepipeline.go`, wiring `HardFilter` → `ScoreWays` → `ApplyDeflectionFilter` into a single orchestration function with summary statistics. Tests cover the end-to-end mixed case, empty input, and all-filtered input.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorepipeline.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorepipeline_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/hardfilter.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/curvature.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/deflection.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `quality/scorepipeline.go` exists with `RunScorePipeline` and `ScorePipelineResult` | PASS |
| `quality/scorepipeline_test.go` passes with `go test ./quality/...` | PASS |
| Pipeline executes hard filter → scoring → deflection in order | PASS |
| Summary statistics are accurate | PASS |
| Empty and all-filtered inputs are handled gracefully | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test
# ok  	github.com/yardbirdsax/twisty/quality	9.055s
# All packages pass.

make lint
# go vet ./...
# No issues reported.
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. Tests pass, lint is clean, the pipeline stages execute in the correct order, all statistics are accurate, and edge cases (empty input, all-filtered) are handled correctly.
