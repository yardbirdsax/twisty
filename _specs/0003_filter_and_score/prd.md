# Product Requirements Document: Filter, Score, and Cache Pipeline

## Overview

This system implements stages 2–4 of the curvature scoring pipeline for the Twisty project: hard filtering of undesirable roads, per-segment curvature scoring using circumradius geometry, and deflection filtering to suppress false positives from intersection doglegs. It also includes a per-tile score cache so that scored segments can be reused across queries without recomputation.

## Problem Statement

Stage 1 (Overpass fetch) produces raw OSM way data for an area. This data contains roads that are irrelevant to discovering twisty driving roads, and the geometry has no curvature information attached. Without processing stages 2–4, the pipeline cannot distinguish a winding mountain pass from a straight highway or a gravel service road.

1. **Noise from non-driveable roads.** The Overpass fetch returns all motor-vehicle-classified highways, including private roads, unpaved surfaces, and restricted-access ways. These inflate results and waste downstream computation.

2. **No curvature information.** Raw OSM ways are just sequences of lat/lon nodes. There is no curvature score, tier classification, or length-weighted metric to rank roads by twistiness.

3. **False positives from intersection geometry.** Many straight roads contain short wiggles at intersections (doglegs) or minor GPS-induced jogs. Without filtering, these produce non-trivial curvature scores on roads that are not actually winding.

4. **Repeated computation.** Scoring is CPU-bound geometry work. Overlapping queries (e.g., shifting a bounding box slightly) would re-score the same tiles, wasting time on data whose inputs have not changed.

## Users

Twisty CLI users who want to discover winding roads in a geographic area. These are drivers or motorcycle riders looking for scenic, curvy routes. They run `twisty fetch` to cache road data for an area, then `twisty score` to produce scored segments. The user profile is a technically comfortable CLI user who is willing to wait for initial computation but expects subsequent runs over the same area to be fast.

## Design Principles

### 1. Tile-Local Processing

All work in stages 2–4 operates on individual tiles independently. A tile's scored output depends only on the ways within that tile and the global scoring parameters — never on neighboring tiles or the query bounding box. This makes the score cache correct across any query that includes a given tile.

### 2. Deterministic Scoring

Given the same input way geometry and the same scoring parameters, the output must be identical. No randomness, no floating-point ordering sensitivity, no dependency on wall-clock time. This is required for cache validity: if inputs match, cached output is guaranteed correct.

### 3. Configurable Parameters as Named Constants

Tier thresholds, tier weights, and deflection filter settings are defined as well-named constants (or a parameter struct) in a single, obvious location in the code. They are not CLI flags. This keeps the interface simple while making it easy for a developer to find and change the values. Each constant should have a comment explaining its role.

### 4. Bias Toward Detecting Curves

When a segment participates in two overlapping three-point triangles, use the minimum circumradius (tightest curve). When in doubt about whether something is a curve, score it — let the deflection filter remove false positives rather than missing real curves at the scoring stage.

## Feature 1: Hard Filter

### Behavioral / Functional Goal

Remove roads from the pipeline that are never desirable for discovering twisty driving roads, so that downstream scoring only operates on driveable, paved, public roads.

### Trigger / Entry Point

Runs automatically on all ways loaded from the tile cache for a given tile, before scoring. Every way must pass or be discarded.

### Core Behavior

Each way's OSM tags are evaluated against a set of disqualification rules. A way that matches any rule is removed entirely — it does not proceed to scoring.

Disqualification rules:

- **Unpaved surface**: `surface` tag is one of: `unpaved`, `gravel`, `dirt`, `mud`, `sand`.
- **Private or restricted access**: `access` tag is `private` or `no`, OR `motor_vehicle` tag is `no` or `private`.
- **Non-motor-vehicle way**: `highway` tag is one of: `track`, `path`, `footway`, `cycleway`, `bridleway`, `steps`.

Note: The Overpass fetch query already excludes most non-motor-vehicle highway types via its regex filter. The hard filter catches any that slip through (e.g., `track` is not in the Overpass filter) and handles tag-based disqualifications (surface, access) that cannot be filtered at query time.

### Rules and Constraints

- A way that matches **any** disqualification rule is removed. Rules are OR'd, not AND'd.
- Ways with no `surface` tag are **not** disqualified — only explicit unpaved values trigger removal.
- The filter is stateless: each way is evaluated independently.
- The existing `IsDisqualifying()` function in `quality/overpass.go` covers access and highway-type checks but does not cover unpaved surfaces. The hard filter for the curvature pipeline must add surface checks.

### Data Model

No new data model. Input: `[]Way`. Output: `[]Way` (subset).

## Feature 2: Segment Curvature Scoring

### Behavioral / Functional Goal

Assign a curvature score to every segment of every way that passes the hard filter, producing a per-segment measure of "how curvy is this piece of road" that is normalized for OSM node density.

### Trigger / Entry Point

Runs on the filtered ways for a tile, after the hard filter.

### Core Behavior

For every way, iterate through consecutive node triples (A, B, C) where A, B, C are three consecutive nodes in the way's geometry:

1. **Circumradius calculation.** Compute the radius of the unique circle passing through A, B, and C using the circumradius formula derived from the law of sines. If the three points are collinear (or nearly so), the radius is infinite — this is a straight segment.

2. **Overlapping triangle handling.** Each interior segment (the segment from B to C in triple A-B-C) participates in two triangles: (A, B, C) and (B, C, D). When both have been computed, assign the **minimum** radius to that segment — biasing toward detecting tighter curves.

3. **Tier assignment.** Bucket the segment's circumradius into a tier:

   | Tier | Radius     | Weight |
   |------|------------|--------|
   | 4    | < 30 m     | 2.0    |
   | 3    | < 60 m     | 1.6    |
   | 2    | < 100 m    | 1.3    |
   | 1    | < 175 m    | 1.0    |
   | 0    | >= 175 m   | 0.0    |

   These thresholds and weights are defined as named constants. Tiering suppresses noise from inconsistent OSM node density — small measurement differences within a tier have no effect.

4. **Segment score.** `segment_length_meters * tier_weight`. The segment length is the Haversine distance between the two endpoints. Multiplying by length normalizes for node density: a 500m curve scores the same whether it has 5 segments or 50. The result represents "distance spent in curves, weighted by tightness."

### Rules and Constraints

- Ways with fewer than 3 nodes cannot form a triangle and receive a score of 0 for all segments.
- Circumradius uses the Haversine-based distances between nodes (meters), not raw lat/lon arithmetic.
- The circumradius formula must handle the degenerate case where the triangle area is zero (collinear points) — return infinity (tier 0, weight 0).
- Tier thresholds are exclusive lower bounds: a radius of exactly 175m is tier 0 (not tier 1).
- All scoring parameters (thresholds, weights) are defined in a single location as named constants with explanatory comments.

### Data Model

```json
{
  "way_id": 12345678,
  "segments": [
    {
      "start": {"lat": 42.35, "lon": -72.60},
      "end": {"lat": 42.351, "lon": -72.601},
      "radius": 85.3,
      "tier": 2,
      "weight": 1.3,
      "length": 124.5,
      "score": 161.85
    }
  ]
}
```

## Feature 3: Deflection Filter

### Behavioral / Functional Goal

Zero out curvature scores for segments that are minor jogs in an otherwise straight road — intersection doglegs, GPS noise, and property-line wiggles — so that straight roads do not receive inflated curvature scores.

### Trigger / Entry Point

Runs on scored segments for each way, after curvature scoring.

### Core Behavior

For each segment with a non-zero score, the filter looks ahead over a window of subsequent segments and checks whether the road's overall heading change is significant relative to the distance traveled. If the road ends up heading roughly the same direction it started, the intermediate "curves" are noise, not real turns — and their scores are zeroed out.

The algorithm, following the Curvature project's approach:

1. From the current segment, accumulate segments forward until the look-ahead distance reaches **2.4 km**.
2. Compute the **overall bearing change**: the absolute difference between the bearing at the start of the window and the bearing at the end of the window.
3. If the overall bearing change is **less than 20°**, zero out the curvature scores for all segments in the window — the road did not meaningfully change direction.

**Important: These specific values (2.4 km look-ahead, 20° threshold) are adopted from the Curvature project.** They are defined as named constants in the same location as the scoring parameters. If future tuning is needed, these are the values to adjust. The look-ahead distance controls how far the filter looks to determine if a curve is "real" (larger = more tolerant of long, gentle curves). The threshold controls how much heading change is required to count as a real curve (higher = more aggressive filtering).

### Rules and Constraints

- Only segments with non-zero scores are candidates for deflection filtering. Tier-0 segments are already zero-scored and are skipped.
- If the look-ahead window extends past the end of the way, use whatever segments remain (the window may be shorter than 2.4 km at the end of a way).
- Zeroing out a segment's score also sets its tier to 0 and weight to 0 — it becomes indistinguishable from a straight segment for downstream stages.
- The filter processes segments sequentially along the way. Once a window's segments are zeroed, the filter advances past the window.

### Data Model

No new data model. Operates on the scored segments from Feature 2, modifying scores in place.

## Feature 4: Score Cache

### Behavioral / Functional Goal

Cache the output of stages 2–4 (hard filter, score, deflection filter) per tile so that subsequent runs over the same area skip recomputation when the underlying data and scoring parameters have not changed.

### Trigger / Entry Point

Before scoring a tile, the `score` command checks the score cache. If a valid cache entry exists, the tile's scored segments are loaded directly. If not, stages 2–4 run and the result is written to the cache.

### Core Behavior

The score cache operates per-tile, matching the existing `TileCache` used for raw Overpass responses. This makes cached scores reusable across overlapping queries — if a user shifts their bounding box slightly, tiles that overlap with a previous query are already scored.

**What is cached:** Pre-aggregation scored segments — the per-way, per-segment scores produced after stages 2–4 (hard filter, score, deflection filter). Aggregation into named roads (stage 5) and soft penalties (stage 6) are applied at query time on top of the cached segments, because those stages depend on the query area boundaries and may change independently of the underlying segment scores.

**Cache read flow:**
1. Compute the tile key (same key format as the raw tile cache).
2. Check if a score cache file exists for that tile.
3. If it exists, read it and validate both hashes (see invalidation below).
4. If both hashes match, return the cached scored segments.
5. If either hash mismatches or the file does not exist, run stages 2–4 and write the result.

**Invalidation:** A cache entry is valid only when both its inputs are unchanged:

1. **Raw tile data** — if the Overpass tile cache entry has been re-fetched (detected by comparing a content hash of the raw tile file against the hash recorded when the score cache entry was written), the score cache entry is stale.
2. **Scoring parameters** — tier thresholds, tier weights, and deflection filter settings are hashed together into a parameters hash. If any scoring parameter changes, all score cache entries are invalidated.

Each score cache file stores both hashes alongside the scored segments. On read, both hashes are recomputed from current inputs and compared. A mismatch triggers re-scoring for that tile.

### Rules and Constraints

- Score cache files are stored in a sibling directory to the raw tile cache: `.cache/scores/` alongside `.cache/tiles/`.
- The cache key format matches the raw tile cache (`tile_<south>_<west>.json`) so there is a 1:1 correspondence.
- Content hashing uses a standard hash function (e.g., SHA-256) over the raw tile file bytes.
- Parameter hashing covers all scoring constants: tier thresholds, tier weights, deflection look-ahead distance, and deflection heading threshold.
- The `score` command should support a `--no-cache` flag to skip score cache reads (still writes), matching the fetch command's behavior.
- The `score` command should support `--clear-score-cache` to delete all score cache entries.

### Data Model

Score cache file structure:

```json
{
  "raw_tile_hash": "sha256:abc123...",
  "params_hash": "sha256:def456...",
  "ways": [
    {
      "way_id": 12345678,
      "segments": [
        {
          "start": {"lat": 42.35, "lon": -72.60},
          "end": {"lat": 42.351, "lon": -72.601},
          "radius": 85.3,
          "tier": 2,
          "weight": 1.3,
          "length": 124.5,
          "score": 161.85
        }
      ]
    }
  ]
}
```

## Feature 5: `score` Subcommand

### Behavioral / Functional Goal

Give the user a single command to run stages 2–4 over a previously fetched area, producing scored and cached segments with a console summary.

### Trigger / Entry Point

User runs `twisty score` with the same address/radius inputs used for `twisty fetch`.

### Core Behavior

1. Geocode the address to a center point (same as `fetch`).
2. Compute tiles for the area (same as `fetch`).
3. For each tile:
   a. Check the score cache — if valid, load cached segments and skip to the next tile.
   b. Load raw tile data from the Overpass tile cache.
   c. Parse ways from the raw data.
   d. Run hard filter (Feature 1).
   e. Run curvature scoring (Feature 2).
   f. Run deflection filter (Feature 3).
   g. Write scored segments to the score cache (Feature 4).
4. Print a summary to the console: total tiles processed, cache hits, ways scored, segments scored, segments zeroed by deflection filter.

If a tile has no raw data in the Overpass cache, it is skipped with a warning — the user should run `twisty fetch` first.

### Rules and Constraints

- The `score` command does **not** fetch from the Overpass API. It only reads from the existing tile cache.
- Flags:
  - `-address` (required): location center.
  - `-radius` [0–50 km]: search radius (default: 25).
  - `-tile-size`: tile edge in degrees (default: 0.05).
  - `-cache-dir`: cache directory (default: `~/.twisty/cache/overpass/`).
  - `-no-cache`: skip score cache reads (still writes).
  - `-clear-score-cache`: delete all score cache entries before running.
  - `-v`: verbose logging.
- The command reuses the same tile computation and cache directory as `fetch` to ensure tiles correspond 1:1.
- If no tiles have raw data (user forgot to run `fetch`), print an error message suggesting they run `twisty fetch` first.

### Data Model

No new persistent data model beyond the score cache (Feature 4). Console output is human-readable text.

## Technical Direction

### Circumradius Geometry

Compute circumradius using the formula: `R = (a * b * c) / (4 * area)` where a, b, c are the Haversine distances between the three points and area is the triangle area computed via the cross-product method. Use the existing `geo.Haversine()` for distances. Handle the degenerate case (area ≈ 0) by returning `math.Inf(1)`.

This computation lives in the `geo` package as a pure geometric function (`geo.Circumradius(a, b, c Coord) float64`), keeping the `quality` package focused on pipeline orchestration.

### Scoring Parameters

All constants live in a single file (e.g., `quality/scoring_params.go`) with clear names and comments:

```go
// --- Tier thresholds (circumradius in meters) ---
// Segments with radius below these thresholds are assigned the corresponding tier.
const (
    TierRadius4 = 30.0   // Tightest curves
    TierRadius3 = 60.0
    TierRadius2 = 100.0
    TierRadius1 = 175.0  // Gentlest curves that still score
)

// --- Tier weights ---
const (
    TierWeight4 = 2.0
    TierWeight3 = 1.6
    TierWeight2 = 1.3
    TierWeight1 = 1.0
    TierWeight0 = 0.0  // Straight segments
)

// --- Deflection filter ---
// These values are adopted from the Curvature project.
// Adjust DeflectionLookAheadM to change how far ahead the filter checks.
// Adjust DeflectionMinHeadingChange to change how much direction change is required.
const (
    DeflectionLookAheadM        = 2400.0  // 2.4 km
    DeflectionMinHeadingChange  = 20.0    // degrees
)
```

### Content Hashing

Use `crypto/sha256` from the Go standard library. Hash the raw tile file bytes for the raw-tile hash. For the parameters hash, serialize all scoring constants into a deterministic string and hash that. This avoids any external dependencies.

### Integration with Existing System

The score pipeline reads from the existing `TileCache` and reuses the `Way` type, `ComputeTiles()`, tile key format, and geocoding infrastructure. No changes to existing code are required — stages 2–4 are additive.

New code lives in:
- `geo/circumradius.go` — circumradius computation (pure geometry)
- `quality/scoring_params.go` — all scoring and filtering constants
- `quality/hardfilter.go` — hard filter implementation
- `quality/curvature.go` — per-segment curvature scoring
- `quality/deflection.go` — deflection filter implementation
- `quality/scorecache.go` — score cache read/write/invalidation
- `main.go` — `score` subcommand registration and CLI wiring

## Phasing

### Phase 1: Hard Filter + Curvature Scoring

**Hypothesis:** The circumradius-based scoring with tier bucketing produces meaningful curvature scores that differentiate winding roads from straight roads, even before deflection filtering.

**Scope:**
- Hard filter implementation (Feature 1)
- Circumradius computation in `geo` package
- Per-segment curvature scoring with tier assignment (Feature 2)
- Scoring parameters as named constants
- Unit tests for all of the above

**Success measurement:**
- Hard filter removes known-undesirable ways (unpaved, private, non-motor-vehicle) from test fixtures.
- Circumradius computation matches expected values for known triangle geometries.
- A known winding road scores significantly higher than a known straight road in a real-data test.

**Exit criteria for Phase 2:** All unit tests pass. Manual inspection of scored output for a real area shows plausible tier assignments.

### Phase 2: Deflection Filter

**Hypothesis:** The deflection filter meaningfully reduces false-positive curvature scores on straight roads without removing real curves.

**Scope:**
- Deflection filter implementation (Feature 3)
- Unit tests with synthetic way geometries (straight road with dogleg, genuinely winding road)

**Success measurement:**
- A straight road with an intersection dogleg has its curvature zeroed by the filter.
- A genuinely winding road retains its curvature scores through the filter.
- Before/after comparison on a real-data tile shows score reduction concentrated on straight roads.

**Exit criteria for Phase 3:** Deflection filter passes all unit tests. Manual comparison confirms it improves score quality.

### Phase 3: Score Cache + CLI

**Hypothesis:** The score cache eliminates redundant computation and the `score` subcommand provides a usable interface for running the pipeline.

**Scope:**
- Score cache implementation (Feature 4)
- `twisty score` subcommand (Feature 5)
- Cache invalidation by raw-tile hash and parameters hash
- Integration tests for the full pipeline (fetch cache → hard filter → score → deflection → score cache)

**Success measurement:**
- Second run of `twisty score` over the same area completes in under 1 second (all cache hits).
- Changing a scoring parameter invalidates all score cache entries.
- Re-fetching a tile invalidates its score cache entry.
- `twisty score` prints an accurate summary.

**Exit criteria:** All tests pass. `twisty score` works end-to-end on a real area.

## Non-Goals

- **Named road aggregation (stage 5).** Grouping ways by name and splitting at straight stretches is a separate piece of work.
- **Soft penalties (stage 6).** Highway-type penalties on aggregated scores are post-aggregation and out of scope.
- **KML output (stage 7).** Visualization is a separate piece of work.
- **CLI flags for scoring parameters.** Parameters are constants in code, not user-facing configuration.
- **Parallel tile processing.** Tiles are processed sequentially. Parallelism is a future optimization.
- **Modifying the `fetch` subcommand.** `fetch` continues to work as-is; `score` is a new, separate command.

## Success Criteria

1. Hard filter correctly removes all ways with unpaved surfaces, private/restricted access, or non-motor-vehicle highway types.
2. Curvature scoring assigns higher scores to objectively windier roads than to straighter roads.
3. Deflection filter reduces false-positive curvature on straight roads with doglegs without removing real curves.
4. Score cache achieves a >10x speedup on repeated runs over the same area with unchanged inputs.
5. Score cache correctly invalidates when raw tile data or scoring parameters change.
6. `twisty score` runs end-to-end and prints a useful summary.

## Future Considerations

- **Named road aggregation (stage 5):** The scored segment data model is designed to be consumed by aggregation. The `way_id` and segment ordering are preserved so that ways can be grouped by name tag and split at straight stretches.
- **KML output (stage 7):** Each segment's tier is preserved in the scored output, enabling per-segment color coding without re-scoring.
- **Parallel tile scoring:** Tile-local processing makes parallelization straightforward — each tile can be scored independently in a goroutine pool. The score cache uses atomic writes (temp file → rename) which is already safe for concurrent access.
- **Incremental scoring:** If only a few tiles are re-fetched, only those tiles need re-scoring. The cache invalidation design already supports this naturally.
