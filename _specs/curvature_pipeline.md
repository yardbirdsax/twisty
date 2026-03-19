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
