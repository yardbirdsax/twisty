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
**Verdict:** APPROVED WITH CHANGES

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

### 1. Missing Comment on Cache Format Change in `scoring_params.go`

**File:** `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go`
**Line:** 96-112

**Observation:** The task spec (Direction 4) explicitly requires documenting that the cache format changed due to the new `Tags` field, and that users must clear their score cache. No such comment was added to `ScoringParamsHash()` or anywhere in the file.

**Current:**
```
// ScoringParamsHash returns a SHA-256 hash of all scoring constants...
```

**Recommended:**
```
// ScoringParamsHash returns a SHA-256 hash of all scoring constants...
// NOTE: The cache format changed in Task 001 (added Tags field to ScoredWay).
// Existing cache entries without tags will silently omit tags on read.
// Users upgrading from a pre-Task-001 cache should run `twisty score --clear-cache`
// or manually delete the score cache directory.
```

**Rationale:** The spec explicitly called this out. Without the comment, future maintainers have no in-code record of why old caches are incompatible.

### 2. `TestScoreCache_WriteAndRead` Does Not Assert Tags

**File:** `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorecache_test.go`
**Line:** 52-99

**Observation:** `TestScoreCache_WriteAndRead` uses `sampleScoredWays()` which now includes tags, but the test body never asserts that `got[0].Tags` matches `ways[0].Tags`. Tag verification is only in the separate `TestScoreCache_TagsRoundTrip` test. This means the primary write/read smoke test does not catch a regression where tags are silently dropped.

**Rationale:** The primary round-trip test should be self-contained and comprehensive. A missing tag assertion here means a tag-drop regression would not be caught by the most obvious test.

---

## Good Practices Observed

1. **Atomic write preserved:** The existing temp-file-then-rename write pattern was kept intact; no regression introduced.
2. **Tag propagation at the lowest level:** Tags are set directly in `ScoreWay` (the leaf function) rather than patched on afterward in the pipeline, which is the correct place to own this invariant.
3. **Dedicated tag round-trip test:** `TestScoreCache_TagsRoundTrip` and `TestScoreCache_JSONRoundTrip` both assert tag values individually by key, not just by length, which would catch partial-tag regressions.

---

## Verification Commands Run

```bash
make test   # all packages: PASS
make lint   # go vet ./...: no issues
```

---

## Final Verdict

**APPROVED WITH CHANGES**

All acceptance criteria are met and tests pass. Two non-blocking items should be addressed: add the in-code comment about cache format incompatibility (explicitly required by the spec), and add a tag assertion to the primary `TestScoreCache_WriteAndRead` test.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.

---

## SHOULD FIX Follow-up (2026-03-20)

Both SHOULD FIX items addressed:

1. Added cache format change comment to `ScoringParamsHash()` in `scoring_params.go` noting the Task 001 backward-incompatible cache change and directing users to clear the score cache.
2. Added tag assertions to `TestScoreCache_WriteAndRead` in `scorecache_test.go` verifying `got[0].Tags` matches `ways[0].Tags`.

All `quality` package tests pass.
