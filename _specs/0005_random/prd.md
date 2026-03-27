# Product Requirements Document: twisty random

> **Status: Work halted (2026-03-26).** Route calculation produces poor-quality loops due to
> difficulties with waypoint ordering and Valhalla routing. The chain selector's greedy approach
> cannot reliably produce clean single-lobe routes. The command exists but is not functioning well.

## Overview

`twisty random` is a new subcommand that generates a loop route starting and ending at a user-supplied point, constrained by a target ride duration, and biased toward twisty roads. It delegates all route calculation to Valhalla, injecting scored waypoints to steer the route toward high-curvature roads rather than implementing its own path-finding logic.

## Problem Statement

The existing `twisty route` command finds the twistiest path between two fixed points, but it requires the user to already know where they want to go. Riders who want to explore from their current location — filling a Saturday afternoon without a predetermined destination — have no good tool for generating a loop ride that emphasizes twisty roads and fits a time budget.

1. **No loop generation.** `twisty route` requires a distinct origin and destination. There is no way to ask "give me a two-hour twisty loop from my driveway."

2. **No time-budget control.** Even with A→B routing, there is no mechanism to target a specific duration. The rider must iterate manually on destinations and twist factors to find something that fits their available time.

3. **Complex route optimization is out of scope.** Implementing a true time-budgeted loop optimizer (e.g., orienteering-style coverage) is a hard combinatorial problem. The system must produce a good-enough route without bespoke graph traversal logic.

## Users

Motorcyclists and drivers who ride from a fixed starting point (home, hotel, trailhead) and want to fill a defined time window with twisty roads, without having to plan a specific destination in advance. They are comfortable with CLI tools and GPX files, and will load the output into a phone GPS or Google Earth for navigation.

## Design Principles

### 1. Valhalla Owns Route Calculation

The system's job is to choose *where* Valhalla should go, not *how* to get there. All path-finding — road-network traversal, turn restrictions, travel time estimation — is Valhalla's responsibility. `twisty random` only injects waypoints derived from scoring data.

### 2. Waypoint Strategy Is a Pluggable Seam

The algorithm for selecting and ordering waypoints is isolated behind an interface. The first implementation (score-weighted random sampling) is a starting hypothesis, not a permanent design. New strategies must be addable without touching the pipeline or CLI.

### 3. Derived Configuration Over Manual Tuning

Parameters the user should not have to think about — search radius, number of waypoints — are derived automatically from the time budget and average speed. Sensible defaults make the command work well out of the box; flags allow overrides for advanced use.

### 4. Maximize Time in the Twists

The route shape should get the rider to twisty roads quickly, keep them there as long as the time budget allows, and then work them home gradually. A route that spends 30 minutes on a boring highway to reach curves and 30 minutes coming back wastes an hour of ride time. The waypoint ordering — nearest twisty road first on the way out, farthest at the apex, gradual return — is designed around this principle.

### 5. Honest Output Over Silent Failure

If the route Valhalla returns falls outside the requested time window, the command writes the GPX anyway and reports the actual duration clearly. The user is never left with a silent wrong answer.

## Feature 1: Search Region Derivation

### Behavioral / Functional Goal

The user specifies only a time budget. The system automatically determines the geographic search radius for road scoring so the user does not need to reason about speed and distance.

### Trigger / Entry Point

Computed once at startup from the `-time` flag and the average speed setting before the scoring pipeline runs.

### Core Behavior

The expected total route distance is estimated as:

```
total_km = avg_speed_mph × 1.60934 × time_hours
```

The search radius is then the radius of a circle whose circumference equals the total expected distance:

```
radius_km = total_km / (2π)
```

**Example:** A 2-hour ride at 35 mph → total distance ≈ 112.7 km → radius ≈ 17.9 km.

The derived radius is used as the input to the existing tiled fetch and score pipeline, identical to `twisty score -radius`.

### Rules and Constraints

- Average speed defaults to 35 mph; overridable via `-avg-speed` flag (mph).
- The derived radius is logged to stderr in verbose mode.
- There is no hard cap on radius; users who supply a very long duration or high speed get a larger search area.
- The derived radius feeds directly into `FetchTiledWays` using the same tile cache infrastructure as `twisty score`.

### Data Model

No new data model; reuses `TileFetchConfig` and `ScorePipelineResult` from the `quality` package.

## Feature 2: Pluggable Waypoint Selection

### Behavioral / Functional Goal

After road scoring, a set of geographic waypoints is selected from the scored road data. These waypoints are passed to Valhalla to steer the route toward twisty sections. The selection strategy is designed to be swapped without changing the rest of the pipeline.

### Trigger / Entry Point

Invoked once after the scoring pipeline completes, receiving the full set of `RoadCollection` results.

### Core Behavior

A `WaypointSelector` interface is defined in a new `waypoint` package:

```go
// WaypointSelector selects waypoints from a set of scored road collections
// to use as intermediate stops in a Valhalla loop route.
type WaypointSelector interface {
    Select(collections []quality.RoadCollection, count int) []geo.Coord
}
```

Two implementations are provided. The **sector lobe selector** is the default. The **weighted random selector** is retained as an alternative, unused by default but available for swap-in if the lobe strategy underperforms.

#### Default: Sector Lobe Selector

The guiding principle is **get to the twists soon, stay in them as long as possible, then work your way home gradually**. Rather than spreading waypoints around a full circle, the route punches out in one direction, enters the twisty roads quickly, threads through them, and curves back.

1. **Pick a random outbound bearing** from the start point (uniform 0–360°).
2. **Define a sector** centered on that bearing (default: 90° arc, i.e. ±45° from the bearing). All eligible `RoadCollection` centroids within the current effective radius and within this angular sector are candidates.
3. **If the sector is empty or has too few candidates**, widen the arc by 30° increments (up to 180°) until enough candidates are found, or fall back to the full circle.
4. **Score-weighted sample** `count` collections from the sector candidates, with selection probability proportional to `PenalizedScore` (same weighting as the weighted random selector).
5. **Order waypoints for a lobe shape:**
   - Sort selected waypoints by distance from start (nearest → farthest).
   - The outbound leg visits them nearest-first, so Valhalla reaches the first twisty road quickly rather than routing around it.
   - The farthest waypoint becomes the apex of the lobe.
   - For the return leg, any waypoints on the "other side" of the sector (opposite half of the arc from the outbound path) are appended in farthest-to-nearest order, creating a gradual return.
   - If all waypoints fall on one side (narrow cluster), the ordering is simply nearest → farthest → back to start, producing an out-and-back lobe.

The number of waypoints defaults to 5; overridable via `-waypoints` flag.

#### Retained: Weighted Random Selector (unused by default)

The original full-circle strategy is kept as an alternative implementation:

1. Assign each `RoadCollection` a selection weight equal to its `PenalizedScore`.
2. Sample `count` collections without replacement, with probability proportional to weight.
3. For each selected collection, extract the midpoint coordinate of its segment list as the waypoint.
4. Sort the selected waypoints by their bearing from the start point (clockwise).

This strategy produces large circular loops and is less effective at concentrating ride time in twisty areas. It is retained behind the interface for fallback or future experimentation.

### Rules and Constraints

- Collections with `PenalizedScore == 0` or `TotalLength < MinRoadLengthM` are excluded from selection in both strategies.
- If fewer scored collections exist in the sector than the requested waypoint count, all eligible collections are used (after arc widening).
- The `WaypointSelector` interface must be the only coupling point between the `random` command and the selection strategy; no strategy-specific logic may appear in the pipeline or CLI layer.
- The sector bearing is random per invocation; no flag is provided to fix it (the `-seed` future consideration covers reproducibility).

### Data Model

```go
// WaypointSelector is the interface all selection strategies implement.
type WaypointSelector interface {
    Select(collections []quality.RoadCollection, count int) []geo.Coord
}

// SectorLobeSelector is the default implementation.
// It picks a random outbound direction and concentrates waypoints
// in an angular sector to produce a lobe-shaped route.
type SectorLobeSelector struct {
    Start    geo.Coord  // origin point for bearing/distance calculations
    Radius   float64    // current effective radius in km (for contraction filtering)
    ArcWidth float64    // sector width in degrees (default: 90)
    Rand     *rand.Rand // injectable for testing
}

// WeightedRandomSelector is the retained alternative (full-circle).
type WeightedRandomSelector struct {
    Start  geo.Coord
    Radius float64
    Rand   *rand.Rand
}
```

## Feature 3: Loop Route Construction

### Behavioral / Functional Goal

A single Valhalla request is issued with the start point, the selected waypoints (in order), and the start point again as the final destination, producing a routable loop.

### Trigger / Entry Point

Invoked after waypoint selection, with the ordered waypoint list.

### Core Behavior

A new function in the `route` package constructs and issues a Valhalla `/route` request with N+2 locations: `[start, wp1, wp2, …, wpN, start]`. Costing options mirror those in `FetchRoutes` (`auto`, `use_highways=0.3`).

Valhalla returns one leg per pair of consecutive waypoints. Each leg's shape is encoded in polyline6 format, which is delta-encoded: every coordinate is stored as an offset from the previous coordinate in the same leg, with the first coordinate offset from zero. This means each leg is a self-contained, independently-decodable blob — not a continuation of the prior leg's encoding.

The existing `tripToRoute` function (noted at `valhalla.go:79`) concatenates the raw encoded strings before decoding, which works accidentally for single-leg (two-point) routes but produces garbage coordinates for any route with more than one leg, because the decoder misinterprets leg 2's zero-relative deltas as continuing from leg 1's last coordinate.

The fix is straightforward: decode each leg independently to get a `[]geo.Coord` slice, then concatenate the slices, dropping the one duplicate boundary point shared between consecutive legs. No bespoke decoding logic is required — `geo.DecodePolyline` is called once per leg, same as today. The existing `tripToRoute` single-leg path is left unchanged; `FetchLoopRoute` uses the corrected multi-leg path.

The returned `Route` struct is identical in shape to those returned by `FetchRoutes`.

### Rules and Constraints

- No alternate routes are requested (`alternates: 0`) for the random loop call; only the primary route is used.
- Each Valhalla leg is decoded separately via `geo.DecodePolyline`, then the coordinate slices are concatenated (with deduplication of shared boundary points between legs).
- If Valhalla returns an error or an empty response, the command exits with a non-zero status and a clear error message.
- The loop Valhalla call is a separate exported function (e.g., `FetchLoopRoute`) and does not modify or replace `FetchRoutes`.

### Data Model

Reuses `route.Route`:

```json
{
  "Points": [{"Lat": 35.5, "Lon": -82.5}, "..."],
  "Duration": 7020.0,
  "Distance": 112400.0,
  "Stats": {
    "AngularDensity": 42.1,
    "Indirectness": 0.31,
    "AdjustedScore": 87.4
  }
}
```

## Feature 4: Time Budget Enforcement with Incremental Retry

### Behavioral / Functional Goal

When the returned route is shorter than the minimum acceptable duration (i.e., more than `-min-under` minutes below target), the system automatically expands the search radius and retries — reusing all previously scored road data and only processing the new outer annular ring of tiles. If the route is within the window or over the maximum, no retry is attempted.

### Trigger / Entry Point

Applied after each `FetchLoopRoute` call. The retry loop continues until the route falls within the acceptable window, the maximum number of attempts is reached, or the route exceeds the upper bound.

### Core Behavior

After the first Valhalla call, the route duration is compared against the tolerance window:

```
min_duration = target - min_under   (default: 15 min)
max_duration = target + max_over    (default: 5 min)
```

**If duration is within `[min_duration, max_duration]`:** accept and continue to output.

**If duration is below `min_duration` (too short):** expand the radius and retry.

- The radius is increased by a fixed step on each attempt (default: 25%, configurable via `-radius-step` flag).
- Only the tiles in the new outer annular ring — those within the expanded radius but outside the previous radius — are fetched and scored. Previously scored collections are retained and merged with the new results before re-running waypoint selection.
- The retry is announced to stderr: `Attempt 2: route too short (1h31m), expanding radius to 22.4km…`

**If duration exceeds `max_duration` (too long):** contract the radius and retry.

- The effective radius is reduced by the same step factor.
- No re-scoring is needed. All road data was already collected at the larger radius. The waypoint selector is simply constrained to only draw from collections whose centroids fall within the smaller radius. No Overpass or scoring work is performed.
- The retry is announced to stderr: `Attempt 2: route too long (2h18m), contracting radius to 14.3km…`

**Oscillation detection:** If the adjustment direction reverses between attempts (i.e., one attempt expanded and the next would contract, or vice versa), the system has bracketed the target and further adjustment is unlikely to converge. In this case, the retry loop stops immediately and the result closest to the target duration across all attempts is accepted.

**If max attempts are exhausted without oscillation:** accept the result closest to the target duration across all attempts and emit a warning.

The final summary always shows the actual route duration and the attempt count if more than one attempt was made.

### Rules and Constraints

- `-min-under` flag: acceptable shortfall in minutes (default: 15). Must be ≥ 0.
- `-max-over` flag: acceptable overage in minutes (default: 5). Must be ≥ 0.
- `-radius-step` flag: radius adjustment factor per retry attempt, as a fraction (default: 0.25, i.e. 25%). Applied symmetrically for both expansion and contraction. Must be > 0.
- `-max-attempts` flag: maximum number of Valhalla calls before accepting best result (default: 3). Must be ≥ 1.
- **Expansion** fetches and scores only the new outer annular ring of tiles. Inner tiles are already cached and incur no Overpass fetch cost.
- **Contraction** requires no tile fetching or scoring. The existing `scoredCollections` slice is unchanged; only the radius bound passed to waypoint selection changes.
- Each retry re-runs waypoint selection with the updated effective radius and a fresh random sample, so the route is not simply a subset of the previous attempt's path.
- Oscillation stops the loop immediately; the best-so-far result is accepted rather than attempting a smaller step.

## Feature 5: GPX Output and Summary

### Behavioral / Functional Goal

The loop route is written as a GPX file and a summary line is printed to stdout, consistent with the `twisty route` output style.

### Trigger / Entry Point

Final step of the pipeline, after the tolerance check.

### Core Behavior

GPX output uses the existing `gpx.WriteGPX` function with track name `"twisty random"`.

Summary printed to stdout:

```
Route: distance=112.4km  duration=1h52m  angular=38.2/km  score=74.3  points=2841
GPX written to random_loop.gpx
```

### Rules and Constraints

- Default output path: `random_loop.gpx`; overridable via `-out` flag.
- Duration is formatted as `XhYm` (e.g., `1h52m`, `45m`).
- If the tolerance warning was triggered, the warning line appears before the summary line.

## Technical Direction

### Valhalla Multi-Leg Decoding Fix

The existing `tripToRoute` function in `route/valhalla.go` contains a known limitation (line 79): it concatenates raw polyline6 strings across legs, which produces garbled coordinates for any route with more than two waypoints (more than one leg). The new `FetchLoopRoute` function must decode each leg independently using `geo.DecodePolyline`, then stitch the resulting `[]geo.Coord` slices, dropping the duplicate boundary coordinate between consecutive legs.

This fix must not change the behavior of the existing `FetchRoutes` / `tripToRoute` path, which is single-leg only.

### Waypoint Selector Interface

The `WaypointSelector` interface lives in a new `waypoint` package, keeping it independent of both the `quality` and `route` packages. The `random` command wires together the pipeline, the selector, and Valhalla — but neither the selector nor the Valhalla layer depends on each other.

### Incremental Radius Adjustment

The retry loop maintains two pieces of state across attempts: `currentRadius float64` and `scoredCollections []quality.RoadCollection`. These are updated differently depending on the adjustment direction.

**On expansion:**
1. Compute tiles for `expandedRadius` using `quality.ComputeTiles`.
2. Subtract the tile set already processed at `currentRadius` (set difference by tile key).
3. Run `FetchTiledWays` and the score pipeline only on the new outer-ring tiles.
4. Aggregate and apply penalties to the new ways.
5. Append new collections to `scoredCollections`; update `currentRadius = expandedRadius`.
6. Re-run waypoint selection on the full `scoredCollections`, constrained to `currentRadius`.

**On contraction:**
1. Reduce `currentRadius` by the step factor. `scoredCollections` is not modified.
2. Re-run waypoint selection on the existing `scoredCollections`, but constrained to collections whose centroids fall within the new smaller `currentRadius`.
3. Issue a new Valhalla call with the updated waypoints.

No tile fetching or scoring occurs on contraction. The `WaypointSelector.Select` call receives the full collection set; the radius constraint is applied as a pre-filter before selection, keeping the selector interface unchanged.

**Oscillation detection** is handled by tracking the direction of the last adjustment (`expanded` or `contracted`). If the current result requires the opposite direction, stop the loop and return the best result seen so far (minimum `|duration - target|`).

### Radius and Waypoint Derivation

Both computations are pure functions with no I/O, making them easily testable. They live in the `waypoint` package alongside the selector interface.

### Integration with Existing System

`twisty random` uses the existing scoring pipeline unchanged: `geocode.Resolve` → `quality.FetchTiledWays` → tile/score cache → `quality.Aggregate` → `quality.ApplyPenalties`. The only new inputs to the pipeline are the derived radius and the center coordinate (same as `twisty score`).

New code lives in:
- `waypoint/` — `WaypointSelector` interface, `WeightedRandomSelector`, radius/waypoint-count derivation helpers
- `route/valhalla_loop.go` (or similar) — `FetchLoopRoute` and corrected multi-leg decoding
- `main.go` — `runRandom` function, wired into the `switch` dispatch

## Phasing

### Phase 1: Basic Loop Generation

**Hypothesis:** Score-weighted random waypoint sampling produces loop routes that feel meaningfully twistier than a plain Valhalla loop with no waypoints, and fit the time budget within tolerance often enough to be useful.

**Scope:**
- Derive search radius from time budget and average speed
- Run existing fetch+score pipeline on derived radius
- Score-weighted random `WaypointSelector` implementation
- `FetchLoopRoute` with correct multi-leg decoding
- Incremental radius expansion retry loop (under-duration case only)
- Tolerance check with warning for over-duration and exhausted retries
- GPX output and summary
- All flags: `-start`, `-time`, `-avg-speed`, `-waypoints`, `-min-under`, `-max-over`, `-radius-step`, `-max-attempts`, `-out`, `-v`, `-overpass-url`, `-cache-dir`, `-no-cache`

**Success measurement:**
- Qualitative: generated routes visibly favor twisty road sections compared to a naive Valhalla loop
- Qualitative: route duration falls within the tolerance window on a majority of test runs across varied regions
- No regression in `twisty route` behavior (existing tests pass)
- Minimum measurement period: manual test runs across 3–5 different geographic areas

**Exit criteria for Phase 2:** The basic loop is useful enough to ride. The main failure mode (if any) is identified — e.g., waypoints cluster in one area, or duration is consistently off.

### Phase 2: Selector Variants and Tuning

**Hypothesis:** Alternative waypoint selection strategies (e.g., spatially distributed grid-based selection, directional spokes) produce better geographic spread and duration accuracy than pure score-weighted random sampling.

**Scope:**
- Implement one or more additional `WaypointSelector` strategies
- `-selector` flag to choose strategy at runtime
- Optionally: retry with a different waypoint sample if duration is outside tolerance

**Success measurement:**
- Qualitative comparison of route variety and duration accuracy across selector strategies
- Minimum measurement period: 10+ rides or test runs per strategy

**Exit criteria for Phase 2 (terminal):** A clear preferred default strategy is identified, or the Phase 1 default proves sufficient and no further iteration is needed.

## Amendments

### 2026-03-21: Sector Lobe Selector replaces Weighted Random as default

After initial testing, the `WeightedRandomSelector` (full-circle, clockwise-bearing-sorted waypoints) was found to produce large circular loops that spread ride time equally in all directions rather than concentrating it where the twisty roads actually are. The route shape favored circumnavigation over lobe-shaped rides that get to the twists quickly.

**Decision:** Implement a new `SectorLobeSelector` as the default strategy. It picks a random outbound bearing, constrains waypoint selection to an angular sector, and orders waypoints nearest-first on the way out / farthest-first on the way back to maximize time in twisty sections. The `WeightedRandomSelector` is retained in the codebase but unused by default, available for swap-in if the lobe strategy underperforms in some regions.

See Feature 2 for full specification of both strategies.

### 2026-03-21: Chain-Building Selector replaces Sector Lobe as default

After testing the `SectorLobeSelector`, routes exhibited excessive backtracking and road repetition. The root cause: dropping a handful of sparse waypoints and letting Valhalla connect them does not produce the kind of route a rider actually builds mentally. Valhalla fills the gaps between waypoints with whatever path it wants — often backtracking or using boring connectors — because it has no concept of "stay on twisty roads" or "don't repeat roads."

User interview revealed the actual mental model for route building:

1. **Pick a direction** where good roads cluster.
2. **Head to the nearest good road** and start riding it.
3. **Chain together twisty roads continuously** — each next road chosen by best available quality weighted against the connector cost to reach it. Short boring connectors are acceptable if they unlock significantly better twisty roads.
4. **Avoid repeating roads** (strong preference, not absolute).
5. **The chain forms a loop** — outward to a far point, then back on different roads.
6. **Time budget is the hard constraint** — sacrifice road quality to be home on time.

**Decision:** Implement a `ChainSelector` that builds a chain of scored road collections greedily, then extracts dense waypoints along the actual road geometry so Valhalla is forced to follow the twisty roads rather than inventing shortcuts. Previous selectors (`WeightedRandomSelector`, `SectorLobeSelector`) are retained but unused by default.

### 2026-03-21: Bearing sweep + spatial grid penalty to reduce route repetition

After testing the `ChainSelector`, routes exhibited significant road repetition and backtracking — the same geographic areas were traversed 2-3 times per route. The root cause: the greedy scoring formula (`PenalizedScore / connectorTime`) with a late homeward bias has no concept of directional coherence or spatial memory, allowing the chain to zigzag freely.

**Decision:** Add two complementary mechanisms to the chain scoring: (1) a bearing sweep that rewards candidates progressing around the sector arc and penalizes counter-sweep backtracking, and (2) a spatial grid (500m cells) that tracks visited areas and penalizes connector corridors through already-traversed terrain. Together these enforce a clean lobe shape while keeping the scoring soft enough to still favor high-quality roads. The homeward bias threshold was also lowered from 50% to 40% of the time budget to begin the return leg earlier.

### 2026-03-21: Collection orientation at chain-insertion time to eliminate spur segments

GPX analysis revealed spur segments where the route goes out to a point and retraces the same path back. Root cause: each `RoadCollection` has an ordered list of segments, and the chain always appended collections in their original orientation. When the chain approached a collection from its "end" side, `extractDenseWaypoints` emitted waypoints from the start, forcing Valhalla to route past the collection to reach its start — creating an out-and-back spur.

**Decision:** Orient each collection at chain-insertion time by comparing the current position's distance to both endpoints. If closer to the collection's end, reverse the collection (reverse segment order + swap Start/End in each segment) before appending. This ensures `extractDenseWaypoints` always emits waypoints in the correct traversal direction with no changes to the extraction logic.

### 2026-03-23: Spatial coherence fixes for chain ordering

Visual overlay of input waypoints on Valhalla's route revealed the chain selector produces zigzag paths — going out 5km, returning to origin, going out 9km in another direction, oscillating. Four root causes identified:

1. **No outbound monotonicity.** Before homeward bias (40% of budget), nothing prevents picking collections closer to origin than the current position. High-scoring near-origin collections beat farther ones via `PenalizedScore / connectorTime`.

2. **Origin-relative bearing sweep.** The sweep computes bearing from origin to candidates. Collections near origin have nearly arbitrary origin-relative bearings, so the sweep can't distinguish forward progress from backward movement.

3. **Connector to midpoint, not nearest endpoint.** Connector distance is measured from currentPos to the collection midpoint, not the nearest endpoint (which is the actual entry after orientation). Off by up to half the collection length.

4. **Non-consecutive waypoint duplicates.** `extractDenseWaypoints` deduplicates only against the previous waypoint. When the chain zigzags back to a visited area, duplicates slip through and force Valhalla U-turns.

**Decision:** Four targeted fixes in `waypoint/chain_selector.go`: (a) outbound distance floor — skip candidates closer to origin than 80% of currentPos during outbound leg; (b) trajectory-relative bearing — compute sweep bearing from currentPos, not origin; (c) connector to nearest endpoint — use min(dist-to-start, dist-to-end) instead of dist-to-midpoint; (d) global waypoint deduplication using a spatial grid key set.

## Non-Goals

- Implementing optimal TSP solvers or full path-finding logic outside Valhalla. (Greedy chain-building over scored road collections is in scope; exact optimization is not.)
- Guaranteeing the route duration falls within tolerance on every run (best-effort, not deterministic).
- Generating multiple route candidates for comparison (single route output only in Phase 1).
- Support for non-auto costing profiles (e.g., bicycle, pedestrian) in Phase 1.
- Interactive or web-based interfaces.

## Success Criteria

1. `twisty random -start "Asheville, NC" -time 2h` produces a rideable loop GPX with duration within ±20 minutes of 2 hours on first run.
2. Running the command twice with the same inputs produces different routes (randomness is observable).
3. The `WaypointSelector` interface allows a new strategy to be added without modifying the pipeline, CLI, or Valhalla integration.
4. No regression in `twisty route` behavior; all existing tests pass.
5. The actual route visibly passes through or near high-scoring road sections visible in a corresponding `twisty score` KML for the same area.

## Future Considerations

- **Sub-step convergence:** Binary search or finer-grained radius adjustment after oscillation is detected. The system stops at first oscillation and picks the best result rather than attempting to narrow the bracket further.
- **Bicycle / pedestrian costing:** The same waypoint injection approach applies to non-auto Valhalla costing profiles; the interface is compatible.
- **Seed flag for reproducibility:** A `-seed` flag to fix the random seed, enabling reproducible routes for debugging or sharing with another rider.
- **GPX waypoint annotation:** Embedding the selected waypoints as GPX waypoints in the output file, so the rider can see where the route was steered.
