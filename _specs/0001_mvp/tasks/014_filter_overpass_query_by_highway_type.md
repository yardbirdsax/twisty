---
# Task 014: Filter Overpass Query by Highway Type

## Summary

Narrow the Overpass API query in `quality.FetchWays` to return only motorised-vehicle
highway types, excluding pedestrian and cycle infrastructure that can never match a
driving route. This reduces the number of ways returned, lowers JSON parse time, and
reduces the size of the grid built by Task 013 — giving compounding benefits.

## Dependencies

Task 007 (FetchWays / Overpass integration). Task 013 is independent but complementary:
fewer ways returned means fewer grid cells and faster grid queries.

## Background / Findings

The current Overpass query fetches **all** highway ways inside the bounding box:

```
way["highway"](south,west,north,east); out geom;
```

The `["highway"]` tag alone matches every OSM road type including `footway`, `cycleway`,
`path`, `steps`, `bridleway`, and `track`. These types are already excluded by
`IsDisqualifying` **after** they have been downloaded, parsed, and loaded into the grid.
For a dense urban or suburban bounding box (e.g., the Asheville→Knoxville corridor spans
several towns) these non-motor ways can represent 20–40% of returned elements.

Filtering at query time:
- Reduces Overpass response payload (network bytes and parse time).
- Reduces `ways` count passed to `ApplyQuality` / `BuildSpatialGrid`.
- Eliminates dead work inside `IsDisqualifying` for elements that were never useful.

## Detailed Directions

### 1. Update the Overpass query in `quality/overpass.go`

Replace the current query template:

```go
query := fmt.Sprintf(
    `[out:json][timeout:10];way["highway"](%.6f,%.6f,%.6f,%.6f);out geom;`,
    south, west, north, east,
)
```

With a filtered query that restricts to motor-vehicle-relevant highway types:

```go
const highwayFilter = `["highway"~"^(motorway|motorway_link|trunk|trunk_link|` +
    `primary|primary_link|secondary|secondary_link|` +
    `tertiary|tertiary_link|unclassified|residential|service|living_street)$"]`

query := fmt.Sprintf(
    `[out:json][timeout:10];way`+highwayFilter+`(%.6f,%.6f,%.6f,%.6f);out geom;`,
    south, west, north, east,
)
```

The regex is anchored (`^...$`) to prevent partial matches.

### 2. Keep `IsDisqualifying` in place

`IsDisqualifying` in `quality/overpass.go` must remain unchanged. It acts as a
belt-and-suspenders guard for edge cases where OSM tagging is inconsistent (e.g., a
`service` road with `access=private`). Filtering at the Overpass level is an optimisation,
not a replacement.

### 3. Update the Overpass HTTP client timeout

The query complexity is lower after filtering; reduce `overpassHTTPClient.Timeout` from
10 s to the documented 10 s `[timeout:10]` server-side value. No change needed — they
already match. However, update the `[timeout:10]` in the query string to `[timeout:25]`
to give the server more room on very large bounding boxes, preventing spurious timeouts
on cross-state routes.

```go
`[out:json][timeout:25];way` + highwayFilter + `(...);out geom;`
```

Also update `overpassHTTPClient.Timeout` from 10 s to 30 s to match:

```go
var overpassHTTPClient = &http.Client{Timeout: 30 * time.Second}
```

### 4. Update tests in `quality/overpass_test.go`

The existing test server fixtures use the current query format. Update the fixture
assertions (if any) to match the new query string, or make the test server accept any
well-formed POST body and validate only the response parsing path.

If the test currently asserts the exact query string sent, add the filter pattern to the
expected string.

### 5. Verify the reduction

Run with `-v` before and after and compare `fetch-ways` `ways` count:

```
./twisty -origin "Asheville, NC" -dest "Knoxville, TN" -twist 0.8 -v 2>timing_filtered.log
```

The `ways` count on the `fetch-ways done` line should be measurably lower than the
unfiltered baseline.

## Acceptance Criteria

- [ ] The Overpass query in `fetchWaysFromURL` includes the motorway-type regex filter.
- [ ] `[timeout:10]` is updated to `[timeout:25]` in the query string.
- [ ] `overpassHTTPClient.Timeout` is updated to 30 s.
- [ ] All existing `quality` tests pass with no regressions.
- [ ] `fetch-ways` `ways` count for the Asheville→Knoxville route is at least 15% lower
  than the unfiltered baseline (measured empirically and noted in a PR comment).
- [ ] `go test ./...` passes.

## Trade-offs and Accuracy Implications

- **Accuracy**: The filtered type list covers all highway types that `IsDisqualifying`
  currently passes (i.e., does NOT disqualify). Types removed by the filter
  (`footway`, `cycleway`, `path`, `steps`, `bridleway`, `track`) are already set to
  `disqualified` by `IsDisqualifying`. Removing them from the Overpass response does not
  change any route's `AdjustedScore`.
- **Maintainability**: If new non-disqualifying highway types are added to OSM in the
  future, they would need to be added to both `highwayFilter` and the `IsDisqualifying`
  allow-list. A comment in the code should make this coupling explicit.
- **Query size**: The query string grows by ~120 characters. This is negligible relative
  to the HTTP overhead.

## Notes

This task is complementary to Task 013 (spatial grid). When both are applied:

1. `fetch-ways` returns fewer ways (this task).
2. `BuildSpatialGrid` builds a smaller grid faster (fewer cells).
3. `NearestWayGrid` searches fewer candidates per query cell (Task 013).

The end-to-end `apply-quality` time for Asheville→Knoxville is expected to be well
under 1,000 ms with both optimizations in place.

A live run of `./twisty -origin "Asheville, NC" -dest "Knoxville, TN" -twist 0.8 -v`
was attempted on 2026-03-16 to capture real `fetch-ways ways=` counts (filtered vs
unfiltered baseline), but the built binary was blocked from executing by the macOS
system security policy (approval required for unsigned binaries built in-session). No
empirical ways counts or fetch-ways elapsed_ms were captured. The ≥15% reduction claim
in the acceptance criteria remains unverified by a live run.
