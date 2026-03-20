# Task 001: Enrich ScoredWay with Tags

## Summary

Add a `Tags map[string]string` field to the `ScoredWay` struct and update the score cache serialization to persist tags. This is the foundational change that makes stages 5-7 possible — aggregation needs `name` and `highway` tags from each way, and those must survive the cache round-trip.

## Dependencies

None — this is the foundational task.

## Detailed Directions

### 1. Add Tags Field to ScoredWay

- In `quality/curvature.go`, add a `Tags map[string]string` field to the `ScoredWay` struct:

```go
type ScoredWay struct {
	WayID    int64              `json:"way_id"`
	Tags     map[string]string  `json:"tags"`
	Segments []ScoredSegment    `json:"segments"`
}
```

### 2. Update Score Cache Serialization

- In `quality/scorecache.go`, add a `Tags map[string]string` field to the `scoredWayJSON` struct used for cache serialization.
- Update the `Write` method to include tags in the serialized form.
- Update the `Read` method to deserialize tags back into `ScoredWay`.

### 3. Populate Tags During Scoring

- In `quality/scorepipeline.go`, the `RunScorePipeline` function receives `[]Way` (which already has `Tags map[string]string`). Update the pipeline so that when `ScoreWays` produces `ScoredWay` results, the `Tags` from the input `Way` are copied onto the output `ScoredWay`.
- In `quality/curvature.go`, update `ScoreWays` (or `scoreWay`) to accept and propagate tags. The simplest approach: `ScoreWays` takes `[]Way` and copies `way.Tags` onto each `ScoredWay` it produces.

### 4. Update ScoringParamsHash

- The `ScoringParamsHash()` function in `quality/scoring_params.go` does NOT need to change for the tags addition — tags are input data, not scoring parameters. However, the cache format itself changes (new field), so existing cache entries will need to be cleared by users. Document this in a comment.

### 5. Update Tests

- Update `quality/scorecache_test.go`: add tags to sample data, verify round-trip through write/read.
- Update `quality/curvature_test.go` or `quality/scorepipeline_test.go`: verify tags propagate from input `Way` through to `ScoredWay` output.
- Update any test helpers that construct `ScoredWay` values (e.g., `sampleScoredWays()` in test files) to include the new `Tags` field.
- Ensure all existing tests still pass.

## Acceptance Criteria

- [ ] `ScoredWay` struct has a `Tags map[string]string` field with proper JSON tag
- [ ] Score cache round-trip preserves tags (write then read returns same tags)
- [ ] `ScoreWays` / `RunScorePipeline` propagates `Way.Tags` to `ScoredWay.Tags`
- [ ] All existing tests pass
- [ ] New test coverage for tag propagation and cache serialization of tags

## Notes

- This is a backward-incompatible cache change. Existing score cache entries won't have tags and will fail to provide useful data for stages 5-7. Users should clear their score cache after this change. The params hash won't auto-invalidate since tags aren't parameters — consider adding a note to the CLI output or README.
- Only `name` and `highway` tags are strictly needed for stages 5-7, but storing the full tags map is simpler and more future-proof.

---
# Task 001 Review: Enrich ScoredWay with Tags

**Reviewer:** Claude (Sonnet 4.6)
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task adds `Tags map[string]string` to `ScoredWay`, propagates tags through `ScoreWay`/`ScoreWays`, persists tags through the score cache round-trip, and adds test coverage for both propagation and cache serialization.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/curvature.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorecache.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorepipeline.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorecache_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorepipeline_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `ScoredWay` struct has a `Tags map[string]string` field with proper JSON tag | PASS |
| Score cache round-trip preserves tags (write then read returns same tags) | PASS |
| `ScoreWays` / `RunScorePipeline` propagates `Way.Tags` to `ScoredWay.Tags` | PASS |
| All existing tests pass | PASS |
| New test coverage for tag propagation and cache serialization of tags | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test   # all packages: PASS
make lint   # go vet ./...: no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met, all tests pass, linter is clean, and the previously noted SHOULD FIX items (cache format comment in `ScoringParamsHash()` and tag assertions in `TestScoreCache_WriteAndRead`) have been addressed.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
