# Task 009: Integration Tests

## Summary

Write integration tests that exercise the full scoring pipeline end-to-end: tile cache → hard filter → curvature scoring → deflection filter → score cache. These tests verify that all components work together correctly using realistic test data.

## Dependencies

- Task 008 (score subcommand — all components wired together)

## Detailed Directions

### 1. Create `quality/score_integration_test.go`

- Use a build tag or the existing integration test pattern from `tilefetch_integration_test.go`.
- Follow the project's test patterns: `t.TempDir()` for cache directories, table-driven where appropriate.

### 2. Test: Full pipeline with fixture data

- Create a test fixture with a small set of realistic ways:
  - A way with `surface=gravel` (should be filtered).
  - A way that is a straight road (5+ nodes in a line, should score ~0).
  - A way that is a winding road (5+ nodes forming clear curves, should score significantly > 0).
  - A way with an intersection dogleg on an otherwise straight road (should have scores zeroed by deflection).
- Run `RunScorePipeline` on these ways.
- Assert:
  - The gravel way does not appear in the output.
  - The straight way has all-zero scores.
  - The winding way has non-zero total score.
  - The dogleg way has its jog segments zeroed.

### 3. Test: Score cache round-trip

- Set up a `TileCache` and `ScoreCache` with temp directories.
- Write realistic raw tile JSON to the tile cache.
- Run the pipeline and write to the score cache.
- Read back from the score cache — verify it's a cache hit with matching data.
- Modify the raw tile data and verify the score cache returns a miss.

### 4. Test: Score cache invalidation on parameter change

- This test verifies the concept, even though constants can't change at runtime.
- Write a score cache entry.
- Manually edit the cache file to change the `params_hash`.
- Read back — verify it's a cache miss.

### 5. Test: Winding road scores higher than straight road

- Create two ways with similar lengths:
  - A straight road (~1km, nodes in a line).
  - A winding road (~1km, nodes forming S-curves).
- Score both.
- Assert that the winding road's total score (sum of segment scores) is at least 5x higher than the straight road's.

### 6. Test: Known curvature values

- Create a way with a known geometry (e.g., nodes on a circle of radius 50m).
- Score it.
- Verify segments are assigned tier 3 (radius < 60m) with weight 1.6.

### 7. Helper: Build test ways

- Create helper functions for building test ways with specific geometries:
  - `straightWay(startLat, startLon, bearingDeg, lengthM, numNodes)` — nodes evenly spaced along a bearing.
  - `circularArcWay(centerLat, centerLon, radiusM, startAngle, arcAngle, numNodes)` — nodes along a circular arc.
- These helpers use real geographic math (`geo.Haversine`, `geo.Bearing`) to produce realistic coordinates.

## Acceptance Criteria

- [ ] `quality/score_integration_test.go` passes with `go test ./quality/...`.
- [ ] Full pipeline end-to-end test covers filter → score → deflection.
- [ ] Score cache hit/miss behavior is verified.
- [ ] Score cache invalidation on data change is verified.
- [ ] Winding roads demonstrably score higher than straight roads.
- [ ] Test fixtures use realistic geographic coordinates and distances.

## Notes

- Use coordinates in a mid-latitude area (e.g., lat ~42°) where Haversine approximations are well-behaved.
- The circular arc helper is particularly useful — it lets you create ways with a known circumradius to verify tier assignment.
- Keep test ways small (5–20 nodes) to keep tests fast and readable.
- These tests do NOT require network access — all data is synthetic or from fixture files.

---
# Task 009 Review: Integration Tests

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task implements `quality/score_integration_test.go` with five integration tests that exercise the full scoring pipeline (hard filter → curvature scoring → deflection filter → score cache) using synthetic geometric fixtures. All post-review changes from the prior review cycle have been applied: `arcRadius` is 50 m, `sCurveWay` produces a genuine two-arc S-curve, deflection assertions are scoped to the dogleg way at the segment level, and geometry helpers use `geo.DestinationPoint` from the `geo` package.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/score_integration_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorepipeline.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scorecache.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/curvature.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/deflection.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/geo.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `quality/score_integration_test.go` passes with `go test ./quality/...` | PASS |
| Full pipeline end-to-end test covers filter → score → deflection | PASS |
| Score cache hit/miss behavior is verified | PASS |
| Score cache invalidation on data change is verified | PASS |
| Winding roads demonstrably score higher than straight roads | PASS |
| Test fixtures use realistic geographic coordinates and distances | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test          # PASS — all packages, short mode (integration tests skipped via -short)
make lint          # PASS — go vet clean
go test -v ./quality/... -run TestScoreIntegration  # all 5 PASS; winding=288.95, straight=0.00
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass, `go vet` is clean, and all four items from the prior review cycle have been correctly addressed. The test file uses `geo.DestinationPoint`, the known-curvature test uses a 50 m radius with a single-tier assertion, `sCurveWay` produces a genuine S-shape, and the deflection filter is verified at the per-segment level on the dogleg way.
