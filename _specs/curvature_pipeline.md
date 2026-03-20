# Curvature Scoring Pipeline

## Goal

Score all roads in a geographic area by "twistiness" to help drivers/riders
discover fun, winding roads. Based on the approach used by the
[Curvature project](https://github.com/adamfranco/curvature), adapted for Go.

## Pipeline Stages

### 1. Fetch road data

Query Overpass API for all highway ways in a bounding box. Cache results to
avoid repeated network calls.

### 2. Hard filter

Remove roads that are never driveable or desirable:
- Unpaved surfaces (dirt, gravel, mud, sand)
- Private or restricted access
- Non-motor-vehicle ways (footways, cycleways, paths, bridleways)

### 3. Score each segment

For every three consecutive nodes (A, B, C) along a way:

1. **Circumradius**: Compute the radius of the unique circle passing through all
   three points. Smaller radius = tighter curve. Three collinear points produce
   an infinite radius (straight road).
2. **Tier assignment**: Bucket the radius into tiers. Each tier has a weight
   that increases for tighter curves:

   | Tier | Radius     | Weight |
   |------|------------|--------|
   | 4    | < 30 m     | 2.0    |
   | 3    | < 60 m     | 1.6    |
   | 2    | < 100 m    | 1.3    |
   | 1    | < 175 m    | 1.0    |
   | 0    | >= 175 m   | 0.0    |

   Tiering suppresses noise from inconsistent OSM node density. Small
   measurement differences within a tier have no effect, and cross-tier
   differences are bounded.

3. **Segment score**: `segment_length * tier_weight`. Multiplying by length
   normalizes for node density — a 500 m curve scores the same whether it has
   5 segments or 50. The result represents "distance spent in curves, weighted
   by tightness."

When a segment participates in two overlapping three-point triangles, the
**minimum** radius is used (biasing toward detecting tighter curves).

### 4. Filter deflections

Zero out curvature for segments that are minor jogs in an otherwise straight
road (e.g., doglegs at intersections). The filter looks ahead several segments
and checks whether the overall heading change is significant relative to the
distance traveled. If the road ends up heading the same direction it started,
the intermediate "curves" are noise, not real turns.

### 5. Aggregate into named roads

- **Group** individual OSM ways into road collections by matching the `name`
  tag (a single named road like "Route 100" may consist of dozens of OSM ways).
- **Split** collections at straight stretches longer than ~2.4 km. This
  isolates twisty sections so their scores aren't diluted by long straight
  portions of the same named road.
- **Sum** segment curvature scores within each collection.

### 6. Apply soft penalties

Adjust the aggregated road score based on road characteristics that make a road
less desirable but not disqualifying:
- Highway type (motorways penalized heavily, primary roads moderately)
- Other soft factors (e.g., road surface quality)

Applied after aggregation because that's when you have the true representation
of the entire road's score.

### 7. Output KML

Generate a multi-color KML file where each road segment is colored by its curve
radius tier, following the same visual approach as the Curvature project.

#### Color scheme

Each segment is styled by its curvature tier:

| Tier | Radius   | Color       | KML (AABBGGRR)  |
|------|----------|-------------|------------------|
| 0    | >= 175 m | Green       | `F000E010`       |
| 1    | < 175 m  | Yellow      | `F000FFFF`       |
| 2    | < 100 m  | Orange      | `F000AAFF`       |
| 3    | < 60 m   | Dark orange | `F00055FF`       |
| 4    | < 30 m   | Red         | `F00000FF`       |

#### KML structure

- `<Document>` with `<Style>` definitions for each tier.
- One `<Folder>` per named road collection (from stage 5), containing the road's
  name, total score, length, and highway type in its description.
- Inside each folder, one `<Placemark>` per contiguous run of the same curvature
  tier. Each placemark holds a `<LineString>` styled by that tier's color. This
  produces the green-to-red gradient visible when zooming into curves.
- Folder list style uses `checkHideChildren` so individual segment placemarks
  are hidden in the sidebar — the user sees road names, not hundreds of segments.

#### Placemark description

Each road folder's description includes:
- Curvature score (total and per-km)
- Total length
- Highway type(s)
- Constituent OSM way IDs

## Score Cache

Cache scored segments (the output of stages 1–4) per tile to avoid recomputation
when the underlying data and scoring parameters have not changed.

### Granularity

The score cache operates per-tile, matching the existing `TileCache` used for
raw Overpass responses. This makes cached scores reusable across overlapping
queries — if a user shifts their bounding box slightly, tiles that overlap with a
previous query are already scored.

### What is cached

Pre-aggregation scored segments: the per-way, per-segment scores produced after
stages 1–4 (fetch, hard filter, score, deflection filter). Aggregation into
named roads (stage 5) and soft penalties (stage 6) are applied at query time
on top of the cached segments, because those stages depend on the query area
boundaries and may change independently of the underlying segment scores.

### Invalidation

A cache entry is valid only when both its inputs are unchanged:

1. **Raw tile data** — if the Overpass tile cache entry has been re-fetched
   (detected by comparing the raw tile file's modification time or content hash
   against the value recorded when the score cache entry was written), the score
   cache entry is stale.
2. **Scoring parameters** — tier thresholds, tier weights, and deflection filter
   settings are hashed together into a parameters hash. If any parameter changes,
   all score cache entries are invalidated.

Each score cache file stores the raw-tile content hash and the parameters hash
alongside the scored segments. On read, both hashes are recomputed from current
inputs and compared. A mismatch triggers re-scoring for that tile.

### Storage

Score cache files live alongside the raw tile cache:

```
.cache/
  tiles/          # existing raw Overpass JSON responses
  scores/         # scored segments per tile
    <tile_key>.json
```

Each score cache file contains:
- `raw_tile_hash`: hash of the raw Overpass response used to produce these scores
- `params_hash`: hash of the scoring parameters at the time of scoring
- `segments`: the scored segment data (way ID, node coordinates, tier, score,
  length)

## Key Concepts

- **Circumcircle**: The unique circle passing through three points. Its radius
  is the core geometric measurement. Derived from the law of sines applied to
  the triangle formed by three consecutive nodes.
- **Why tiers, not raw radius**: OSM node density varies wildly. Tiers compress
  measurement noise so the same physical curve scores consistently regardless
  of who mapped it.
- **Why multiply by length**: Without length normalization, densely-noded roads
  look artificially curvier because they produce more scored segments.
- **Why split at straight stretches**: Prevents a short twisty section from
  being buried in a long road's aggregate score.
- **Why filter deflections**: Prevents intersection doglegs and minor GPS jogs
  from inflating scores on straight roads.
- **Why multi-color KML**: Per-segment coloring shows riders exactly where curves
  are and how tight they are, rather than just which roads are twisty overall.
- **Why cache pre-aggregation**: Aggregation groups ways by name across the query
  area, so its output changes when the bounding box changes. Segment scores are
  tile-local and stable, making them the right caching boundary.
- **Why content-hash invalidation**: Time-based expiry would either serve stale
  data or discard valid scores unnecessarily. Hashing the actual inputs ensures
  the cache is always correct and never expires prematurely.
