# Task 003: Aggregation — Group by Name and Find Connected Components

## Summary

Implement the first half of stage 5: grouping scored ways by their `name` tag and finding connected components within each name group using endpoint proximity. This ensures that same-named roads in different geographic areas are kept as separate collections.

## Dependencies

Task 001, Task 002 — requires `ScoredWay` with tags and `RoadCollection` data model.

## Detailed Directions

### 1. Implement `GroupWaysByName`

- In `quality/aggregate.go`, add a function:

```go
// GroupWaysByName groups scored ways by their "name" tag.
// Ways without a name tag are excluded.
func GroupWaysByName(ways []ScoredWay) map[string][]ScoredWay
```

- Iterate through ways, read `Tags["name"]`, skip ways with empty or missing name.
- Collect into a `map[string][]ScoredWay`.

### 2. Implement Endpoint Extraction

- Add a helper to extract the start and end coordinates of a `ScoredWay`:

```go
func wayEndpoints(w ScoredWay) (start, end geo.Coord, ok bool)
```

- Start = first segment's `Start`, End = last segment's `End`.
- Handle edge case: way with zero segments — return zero coords and `ok = false`.

### 3. Implement Connected Component Discovery

- Add a function that finds connected components within a slice of `ScoredWay`:

```go
// FindConnectedComponents groups ways by endpoint proximity.
// Two ways are connected if any endpoint of one is within proximityM meters
// of any endpoint of the other.
func FindConnectedComponents(ways []ScoredWay, proximityM float64) [][]ScoredWay
```

- Use a union-find (disjoint set) algorithm or simple iterative merging:
  - For each pair of ways, check if any endpoint of way A is within `proximityM` of any endpoint of way B.
  - Use the Haversine distance function from `geo.HaversineDistance` (or the equivalent in the `geo` package).
  - Group connected ways into components.
- The proximity threshold constant should be defined in `scoring_params.go`:

```go
const ConnectedEndpointProximityM = 100.0 // meters
```

### 4. Implement Way Ordering Within a Component

- Add a function to order ways within a connected component into a continuous chain:

```go
// OrderWays arranges ways in a connected component into a continuous path
// by chaining endpoints.
func OrderWays(ways []ScoredWay) []ScoredWay
```

- Start with any way. Repeatedly find the next way whose start endpoint is closest to the current chain's end endpoint (within the proximity threshold). Reverse a way's segments if its end is closer to the chain's current end than its start is.
- This is a greedy nearest-neighbor chain. It doesn't need to be perfect — it just needs to produce a reasonable geographic ordering for splitting.

### 5. Write Unit Tests

- In `quality/aggregate_test.go`, add tests:
  - `TestGroupWaysByName`: ways with same name grouped, unnamed ways excluded
  - `TestFindConnectedComponents`: two clusters of same-named ways far apart produce two components; nearby ways produce one component
  - `TestOrderWays`: a shuffled set of chainable ways is ordered correctly
  - Edge cases: single way, way with no segments, all ways disconnected

## Acceptance Criteria

- [ ] `GroupWaysByName` correctly groups and excludes unnamed ways
- [ ] `FindConnectedComponents` correctly separates geographically distant same-named roads
- [ ] `FindConnectedComponents` merges nearby same-named roads into one component
- [ ] `OrderWays` produces a continuous chain from unordered ways
- [ ] Proximity constant defined in `scoring_params.go`
- [ ] All unit tests pass

## Notes

- Use the existing `geo` package for distance calculations. Check what's available — there should be a Haversine or similar function.
- The connected component algorithm is O(n²) in the number of ways per name group. This is fine — name groups rarely exceed a few hundred ways.
- Way ordering doesn't need to handle complex topologies (branches, loops). A simple greedy chain is sufficient.

---
# Task 003 Review: Aggregation — Group by Name and Find Connected Components

**Reviewer:** Senior Software Engineer Agent
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task implements `GroupWaysByName`, `FindConnectedComponents`, `OrderWays`, and the `wayEndpoints`/`reverseWay` helpers in `quality/aggregate.go`, with unit tests in `quality/aggregate_test.go` and the `ConnectedEndpointProximityM` constant in `quality/scoring_params.go`. All three issues flagged in the prior review cycle have been resolved.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/aggregate.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/aggregate_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `GroupWaysByName` correctly groups and excludes unnamed ways | PASS |
| `FindConnectedComponents` correctly separates geographically distant same-named roads | PASS |
| `FindConnectedComponents` merges nearby same-named roads into one component | PASS |
| `OrderWays` produces a continuous chain from unordered ways | PASS |
| Proximity constant defined in `scoring_params.go` | PASS |
| All unit tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test   # All packages pass, including quality (9.2s)
make lint   # go vet only, no issues reported
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass, tests are green, lint is clean, and all prior-cycle SHOULD FIX items have been addressed: `ConnectedEndpointProximityM` is now included in `ScoringParamsHash`, `TestFindConnectedComponents_AllDisconnected` covers the fully-disconnected-valid-ways path, and the `wayEndpoints` implementation correctly returns the `ok bool` third value.

---
