---
# Task 012: Analyze Performance and Plan Optimizations

## Summary

Use the observability added in Task 011 to empirically identify the slowest pipeline
stage(s). Based on the findings, write one or more new task files (starting at `013_...`)
that describe concrete, targeted optimizations. This task produces task files as its
primary output, not code changes.

## Dependencies

Task 011 — the `-v` timing logs must be in place before this analysis can be run.

## Detailed Directions

### 1. Build the binary

```
go build -o twisty .
```

### 2. Run with a long-distance test route and capture timing output

Choose a route that is likely to produce a large Overpass response (many ways) and routes
with high point counts. A cross-state or cross-country drive works well. Capture stderr
separately so the timing lines are easy to read:

```
./twisty \
  -origin "Asheville, NC" \
  -dest "Knoxville, TN" \
  -twist 0.8 \
  -v \
  2>timing.log
```

Inspect `timing.log`:

```
cat timing.log
```

### 3. Identify the bottleneck stage(s)

Look at the `elapsed_ms` values in `timing.log`. Pay particular attention to:

| Stage | What to look for |
|-------|-----------------|
| `fetch-routes` | Should be < 3000 ms (network only) |
| `fetch-ways` | Should be < 15000 ms (network + Overpass query time) |
| `apply-quality` | Compare `ways` × `total_points` product against elapsed time |
| `score-routes` | Should be < 500 ms for typical point counts |

Record the `ways` count and `total_points` count from the `apply-quality` start line. If
`apply-quality` elapsed_ms is large relative to other stages, it is the bottleneck. If
`fetch-ways` is the dominant cost, the query or bounding box may need attention instead.

### 4. Read the slow stage's source code

For the most likely bottleneck (`apply-quality` / `NearestWay`), re-read:
- `quality/overpass.go` — `ApplyQuality()` and `NearestWay()`

Note:
- `NearestWay` is called once per route **segment** (i.e., `total_points - routes` times).
- Each call iterates every node of every way: O(ways × avg_nodes_per_way).
- For 3 routes × 4000 points × 5000 ways × 8 nodes/way ≈ **480 million Haversine calls**.

### 5. Research candidate optimizations

For each bottleneck identified, consider and document at least two candidate approaches
before choosing one to spec. Use the guiding questions below:

**For `apply-quality` / `NearestWay` brute-force search:**
- *Spatial grid index*: partition ways into a lat/lon grid; for each query point, only
  search ways in the same and neighboring cells. O(1) lookup + small constant.
- *Point sampling*: instead of checking every route segment midpoint, sample every Nth
  point (e.g., every 10th). Reduces calls by 10× at the cost of some accuracy.
- *Pre-built k-d tree*: index all way nodes into a 2D k-d tree; nearest-neighbor query is
  O(log n). Libraries: none in stdlib — would require either a vendored package or a
  simple hand-rolled 2D tree.
- *Reduce Overpass result size*: narrow the query to only include road types that matter
  (e.g., exclude `footway`, `cycleway`, `path` at query time rather than post-filter).

**For `fetch-ways` slow query:**
- *Tighter bounding box*: reduce the 0.01-degree buffer or clip the bbox to actual route
  extents rather than a rectangle.
- *Overpass filter at query time*: add `["highway"~"^(primary|secondary|tertiary|residential|unclassified|service)$"]`
  to reduce returned elements.

### 6. Choose and spec the optimizations

For each bottleneck, write a new task file describing one concrete optimization approach.
Name and number the files sequentially starting from `013`:

```
_specs/0001_mvp/tasks/013_<optimization_name>.md
```

Each optimization task file must follow the standard task template and include:
- A clear summary of the algorithmic change
- Specific data structures or algorithms to implement
- The acceptance criterion of measurable speedup (e.g., "apply-quality elapsed_ms < 2000
  for the Asheville→Knoxville test route")
- Any trade-offs or accuracy implications

### 7. Commit the new task files

Stage and commit only the new task files:

```
git add _specs/0001_mvp/tasks/013_*.md
git commit -m "spec: add optimization task(s) based on performance analysis"
```

## Acceptance Criteria

- [ ] `timing.log` has been captured for at least one long-distance test run
- [ ] The slowest stage has been identified and its `elapsed_ms` recorded
- [ ] The input size metrics (`ways`, `total_points`) for the slow stage are documented
- [ ] At least one new task file (`013_...md`) has been written to the tasks directory
- [ ] Each new task file follows the standard template and includes a measurable acceptance criterion
- [ ] Findings are summarized in a brief comment in the new task file(s) under "Notes"

## Notes

- The most likely bottleneck based on static analysis is `apply-quality`: `NearestWay` is
  O(ways × wayNodes) and is called for every route segment. For a large cross-state route,
  this is likely hundreds of millions of Haversine calls.
- If `apply-quality` is fast but `fetch-ways` is slow, the fix is in the Overpass query,
  not the Go code.
- Do not write optimization code in this task — only task files. Keep analysis and
  implementation separate so the plan can be reviewed before work begins.
- The `timing.log` file does not need to be committed; it is local working data.

---
# Task 012 Review: Analyze Performance and Plan Optimizations

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

This task required running the binary against the Asheville-to-Knoxville route to capture empirical timing data, then producing new task files documenting optimization plans. Two task files (013, 014) were committed in `67184bd`. Both files contain live timing data from a real pipeline run captured on 2026-03-16 (80,849 ways, 134,944 ms apply-quality, 8,251 ms fetch-ways).

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/_specs/0001_mvp/tasks/013_spatial_grid_index_for_nearest_way.md` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/_specs/0001_mvp/tasks/014_filter_overpass_query_by_highway_type.md` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `timing.log` captured for at least one long-distance test run | PASS — live run data documented in Notes of 013 and 014 |
| Slowest stage identified with `elapsed_ms` recorded | PASS — apply-quality 134,944 ms identified as dominant stage |
| Input size metrics (`ways`, `total_points`) documented | PASS — 80,849 ways, 4,499 total_points recorded |
| At least one new task file (`013_...md`) written | PASS |
| Each new task file follows the standard template with a measurable acceptance criterion | PASS |
| Findings summarized under "Notes" | PASS — live baseline documented in both files |
| Task files committed with required message | PASS — commit `67184bd` |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
git show 67184bd --name-only --format="%H %s"  # 013 and 014 committed with correct message
make test   # all packages pass; root package has no test files (expected)
make lint   # passes with no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. Live timing data was captured (80,849 ways, 134,944 ms apply-quality) and documented in the Notes sections of both new task files. Both tasks include measurable acceptance criteria grounded in empirical measurements. `make test` and `make lint` pass cleanly.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
