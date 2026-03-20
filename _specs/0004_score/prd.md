# Product Requirements Document: Road Scoring Aggregation & KML Output

## Overview

Twisty is a tool that scores roads by "twistiness" to help drivers and riders discover fun, winding roads. The curvature pipeline fetches OpenStreetMap road data, filters it, and scores individual road segments by curve tightness. This PRD covers stages 5-7 of the pipeline: aggregating scored segments into named road collections, applying highway-type penalties, and rendering the results as a multi-color KML file. These stages transform raw per-segment scores into a human-usable map layer.

## Problem Statement

Stages 1-4 of the pipeline produce scored segments per OSM way, cached per tile. This data is useful for computation but not for end users — it lacks the grouping, filtering, and visualization needed to answer the question "where are the best twisty roads near me?"

1. **Fragmented road identity.** A single named road (e.g., "Route 100") may consist of dozens of OSM ways. Without aggregation, a user sees disconnected way-level scores rather than a single road-level score that reflects the full driving experience.

2. **Score dilution by straight stretches.** A road with a 5 km twisty section and 30 km of straight highway gets a misleading aggregate score. Without splitting at straight gaps, the twisty section's score is buried.

3. **No visual output.** Scored data sitting in a JSON cache is invisible. Drivers need a map view where they can see which roads are twisty, where the curves are, and how tight they are — at a glance.

4. **Undesirable roads inflating results.** Motorways and trunk roads can have curves (on-ramps, interchanges) that score well but are not enjoyable driving roads. Without penalty adjustments, these appear alongside genuinely fun roads.

## Users

Motorcycle riders and driving enthusiasts who want to discover twisty roads in a geographic area. They use the CLI to score an area and load the resulting KML into Google Earth or similar mapping tools to plan rides/drives. They care about actionable output: named roads they can look up and navigate to, colored by curve intensity so they can assess at a glance whether a road is worth the trip.

## Design Principles

### 1. Tile-Local Computation, Global Aggregation

Stages 1-4 operate per-tile and are cached at that granularity. Stages 5-7 operate across the full query area because road names span tiles and aggregation depends on the complete set of ways. The boundary between cached (tile-local) and uncached (global) computation is at the transition from stage 4 to stage 5.

### 2. Score Reflects Driving Experience

Every transformation — grouping by name, splitting at straights, applying penalties — should make the final score better reflect how a driver would experience the road. A score of 1500 should mean "this road has sustained, meaningful curves" not "this road has a few interchange ramps."

### 3. Visual Precision Over Simplicity

The KML output colors each segment by its curve tier rather than coloring entire roads by aggregate score. This costs more placemarks but lets users see exactly where curves are and how tight they are — critical information for planning a ride.

### 4. Additive Pipeline Stages

Each stage takes well-defined input and produces well-defined output. Aggregation, penalties, and KML rendering are separable concerns. This allows testing and evolving each stage independently.

## Feature 1: Road Collection Aggregation (Stage 5)

### Behavioral / Functional Goal

Transform a flat list of scored ways into named road collections that match how a driver thinks about roads. "Route 100" should appear as one entry (or a few entries if it has long straight gaps), with a single aggregate score that reflects the twisty portions.

### Trigger / Entry Point

Automatically runs after all tiles are scored (stages 1-4), as part of the `score` subcommand execution. Operates on the union of all scored ways across all tiles in the query area.

### Core Behavior

1. **Exclude unnamed ways.** Ways without a `name` tag are dropped from aggregation. They do not appear in the KML output.

2. **Group by name.** All scored ways sharing the same `name` tag value are collected into a candidate group.

3. **Find connected components.** Within each name group, identify connected chains of ways by endpoint proximity. Two ways are connected if an endpoint of one is within ~100 meters of an endpoint of the other. Each connected component becomes a separate road collection. This prevents geographically disconnected roads that share a name (e.g., two different "Main Street" roads in separate towns) from being merged.

4. **Order ways within a collection.** Ways within each connected component are ordered by chaining endpoints to form a continuous path.

5. **Split at straight gaps.** Walk the ordered segments within each collection. When a contiguous run of zero-score (tier 0) segments exceeds 2,414 meters (1.5 miles, matching the Curvature project), split the collection at that point. This isolates twisty sections so their scores are not diluted by long straight portions.

6. **Sum scores.** For each resulting sub-collection, sum all segment scores to produce the collection's total curvature score. Also compute total length (sum of all segment lengths) and per-km score (total score / total length in km).

### Rules and Constraints

- Ways with no `name` tag are excluded entirely
- Split threshold is 2,414 meters of contiguous zero-score segments
- A collection with a total score of 0 after aggregation is still retained at this stage (filtering by score happens at KML output time via `-min-score`)
- When a named road spans multiple tiles, ways from all tiles are merged into one name group before connectivity analysis
- Same-named roads that are not geographically connected (no endpoints within ~100m) become separate collections
- Sub-collections created by connectivity splitting or straight-gap splitting inherit the parent's name, with a numeric suffix if needed (e.g., "Route 100 (1)", "Route 100 (2)")

### Data Model

```json
{
  "name": "Route 100",
  "sub_index": 0,
  "highway_types": ["secondary", "tertiary"],
  "way_ids": [12345, 12346, 12347],
  "segments": [
    {
      "start": {"lat": 44.0, "lon": -72.5},
      "end": {"lat": 44.001, "lon": -72.501},
      "tier": 3,
      "score": 48.5,
      "length": 30.3
    }
  ],
  "total_score": 1523.7,
  "total_length": 12340.0,
  "score_per_km": 123.5
}
```

## Feature 2: Highway-Type Soft Penalties (Stage 6)

### Behavioral / Functional Goal

Reduce the scores of road types that are technically curvy but not enjoyable to drive, so that the final ranking better reflects driving desirability. Motorway interchange ramps should not rank alongside mountain switchbacks.

### Trigger / Entry Point

Runs immediately after aggregation (stage 5), before KML output. Applied to each road collection's total score.

### Core Behavior

Multiply each road collection's total score by a penalty factor determined by the collection's highway type(s). The penalty factor is the **minimum** penalty across all highway types present in the collection (most penalizing type wins, since a road that is partially motorway is less desirable overall).

| Highway Type | Penalty Multiplier |
|---|---|
| motorway, motorway_link | 0.3 |
| trunk, trunk_link | 0.5 |
| primary, primary_link | 0.8 |
| secondary, secondary_link | 0.9 |
| tertiary, tertiary_link | 1.0 |
| unclassified | 1.0 |
| residential | 1.0 |
| service | 1.0 |
| all others | 1.0 |

The penalty is applied to `total_score` and `score_per_km`. Individual segment scores and tiers are **not** modified — the penalty is an aggregate adjustment. This preserves per-segment coloring in the KML while adjusting ranking.

### Rules and Constraints

- Penalty multipliers are defined as constants alongside the existing scoring parameters in `scoring_params.go`
- The minimum penalty across all highway types in a collection is used (conservative: a road partially on a motorway gets the motorway penalty)
- Penalty is applied after aggregation because a road's highway types are only fully known after grouping
- Segment-level tiers and scores are unchanged; only collection-level totals are adjusted
- A penalty multiplier of 1.0 means no penalty

### Data Model

The road collection model from stage 5 gains:

```json
{
  "penalty_factor": 0.8,
  "penalized_score": 1218.96,
  "penalized_score_per_km": 98.8
}
```

## Feature 3: KML Output (Stage 7)

### Behavioral / Functional Goal

Produce a multi-color KML file where each road segment is colored by its curvature tier, following the Curvature project's visual approach. Users load this into Google Earth and can immediately see which roads are twisty and where the tight curves are.

### Trigger / Entry Point

Runs as the final stage of the `score` subcommand. Output path is specified via the required `-out` flag. An optional `-min-score` flag (default 0) filters low-scoring roads from the output.

### Core Behavior

Generate a KML document with:

1. **Style definitions.** Five `<Style>` elements, one per curvature tier, defining line colors and width:

   | Tier | Radius | Color Name | KML Color (AABBGGRR) |
   |------|--------|------------|----------------------|
   | 0 | >= 175 m | Green | `F000E010` |
   | 1 | < 175 m | Yellow | `F000FFFF` |
   | 2 | < 100 m | Orange | `F000AAFF` |
   | 3 | < 60 m | Dark Orange | `F00055FF` |
   | 4 | < 30 m | Red | `F00000FF` |

2. **One `<Folder>` per road collection.** Each folder contains:
   - Road name as the folder name
   - Description with: penalized curvature score, score per km, total length, highway type(s), constituent OSM way IDs
   - `<Style><ListStyle><listItemType>checkHideChildren</listItemType></ListStyle></Style>` to hide individual segments in the sidebar

3. **Placemarks within each folder.** One `<Placemark>` per contiguous run of the same curvature tier. Each placemark contains a `<LineString>` with the coordinates of that run, styled by the tier's color. This produces the green-to-red gradient visible when zooming into curves.

4. **Road ordering.** Roads are sorted by penalized score descending, so the highest-scoring roads appear first in the KML sidebar.

### Rules and Constraints

- Roads with a penalized score below the `-min-score` threshold are excluded from the KML
- The `-out` flag is required; the command fails with an error if not provided
- KML line width is 4 pixels for all tiers
- Coordinates in `<LineString>` use `lon,lat,0` format (KML standard: longitude first)
- Contiguous tier runs are merged into single placemarks to minimize file size (e.g., five consecutive tier-2 segments become one placemark with all their coordinates)
- The KML document name is "Twisty Roads"

### Data Model

KML is an XML format. No additional application data model is needed beyond the road collection model from stages 5-6. The KML structure is:

```xml
<Document>
  <name>Twisty Roads</name>
  <Style id="tier0">...</Style>
  ...
  <Folder>
    <name>Route 100</name>
    <description>Score: 1219 | Per km: 99 | Length: 12.3 km | Types: secondary | Ways: 12345, 12346</description>
    <Style>...</Style>
    <Placemark>
      <styleUrl>#tier3</styleUrl>
      <LineString><coordinates>-72.5,44.0,0 -72.501,44.001,0</coordinates></LineString>
    </Placemark>
    ...
  </Folder>
</Document>
```

## Feature 4: CLI Integration

### Behavioral / Functional Goal

The `score` subcommand becomes the complete end-to-end pipeline: fetch scored data from cache, aggregate, apply penalties, and output KML.

### Trigger / Entry Point

`twisty score -address "..." -radius 25 -out roads.kml`

### Core Behavior

The updated `score` subcommand:

1. Resolves address to center point (existing)
2. Computes tiles and scores them via cache or pipeline (existing stages 1-4)
3. Collects all scored ways across all tiles, enriched with tags
4. Runs aggregation (stage 5)
5. Applies highway-type penalties (stage 6)
6. Writes KML to `-out` path, filtering by `-min-score` (stage 7)
7. Prints summary to stdout including: number of road collections, top-scoring roads, output file path

### Rules and Constraints

- `-out` flag is required (string, path to output KML file)
- `-min-score` flag is optional (float64, default 0)
- If no roads pass the min-score filter, write a valid but empty KML document and print a warning
- Summary output goes to stderr so it doesn't interfere with piping
- Existing flags (`-address`, `-radius`, `-tile-size`, `-cache-dir`, `-no-cache`, `-clear-score-cache`, `-v`) retain their current behavior

### Data Model

No new data model. CLI flags map directly to pipeline configuration.

## Technical Direction

### ScoredWay Tag Enrichment

Add a `Tags map[string]string` field to the `ScoredWay` struct. This persists relevant OSM tags (at minimum `name` and `highway`) into the score cache, making stages 5-7 self-contained from cached data alone without re-parsing raw tile data. This is a backward-incompatible cache change — existing score cache entries will need to be invalidated (the params hash change will handle this automatically if tags are included in the serialized form, or a manual cache clear on upgrade).

### Concurrent Pipeline Architecture

The pipeline should maximize concurrency, streaming data through stages in the smallest chunks each stage can accept. The architecture has two fan-out/fan-in phases separated by one barrier:

**Phase A: Tile Processing (fan-out → barrier)**
- A bounded worker pool (sized to `GOMAXPROCS`) processes tiles concurrently. Each worker reads from the score cache (or runs stages 1-4 on cache miss) and sends its `[]ScoredWay` results into a shared channel.
- A collector goroutine reads from this channel and accumulates all scored ways, grouping them by `name` tag as they arrive. This is the **barrier** — aggregation cannot begin until all tiles are processed, because a named road may span multiple tiles.

**Phase B: Per-Name-Group Processing (fan-out → fan-in)**
- Once all tiles are collected, the set of name groups is dispatched to a worker pool. Each worker independently:
  1. Finds connected components within the name group (stage 5: connectivity)
  2. Orders ways within each component (stage 5: ordering)
  3. Splits at straight gaps (stage 5: splitting)
  4. Computes aggregate scores (stage 5: scoring)
  5. Applies highway-type penalties (stage 6)
  6. Builds KML folder XML fragments for each resulting collection (stage 7: fragment generation)
- Workers send completed `[]RoadCollection` (with KML fragments) into an output channel.

**Assembly:**
- A final step collects all road collections, sorts by penalized score descending, applies the `-min-score` filter, and assembles the complete KML document from the pre-built fragments.

**Key constraints:**
- The barrier between Phase A and Phase B is unavoidable — it is the minimum synchronization point required by the aggregation semantics
- Within each phase, work items are independent and require no coordination
- Channel-based flow control provides natural backpressure
- Worker pool sizes should be configurable but default to `runtime.GOMAXPROCS(0)`

### Aggregation Implementation

Aggregation operates on the full set of scored ways across all tiles. The core function signature is `Aggregate(waysByName map[string][]ScoredWay) []RoadCollection`. It receives pre-grouped ways (grouped during the Phase A collection step) and performs connectivity analysis, ordering, splitting, and scoring per name group. Each name group is independent and can be processed concurrently. No caching at this level — aggregation depends on query-area boundaries.

### KML Generation

Use Go's `encoding/xml` package to generate KML. Define Go structs that marshal to the KML schema. Avoid string concatenation for XML generation — struct-based marshaling is safer and handles escaping. KML folder fragments are built per-collection during Phase B, then assembled into the final document after sorting and filtering.

### Integration with Existing System

The pipeline orchestration in `runScore()` (main.go) currently processes tiles sequentially and prints stats. It needs to be replaced with the concurrent pipeline described above: (a) fan-out tile processing with a worker pool, (b) collect and group by name, (c) fan-out per-name-group processing, (d) assemble and write KML.

New code lives in:
- `quality/aggregate.go` — road collection aggregation (stage 5)
- `quality/penalty.go` — highway-type penalty application (stage 6)
- `quality/kml.go` — KML generation (stage 7)
- `quality/scoring_params.go` — extended with penalty multiplier constants
- `quality/curvature.go` — `ScoredWay` struct updated with Tags field
- `main.go` — `runScore()` extended with stages 5-7 and new flags

## Phasing

### Phase 1: Aggregation & KML Output

**Hypothesis:** Grouping scored ways by road name, splitting at straight gaps, and rendering as multi-color KML produces output that is immediately useful for discovering twisty roads.

**Scope:**
- Add Tags to ScoredWay and update score cache serialization
- Implement road collection aggregation (group by name, split at straights, sum scores)
- Implement highway-type soft penalties
- Implement KML output with per-segment tier coloring
- Extend `score` subcommand with `-out` and `-min-score` flags
- Unit tests for aggregation logic, penalty application, and KML generation
- Integration test: synthetic scored ways through full stages 5-7 pipeline

**Success measurement:**
- KML loads correctly in Google Earth with visible per-segment coloring
- Known twisty roads in test area appear with high scores
- Motorways/trunks are visibly deprioritized vs. secondary/tertiary roads
- Pipeline completes in under 5 seconds for a 25 km radius area (excluding fetch time)

**Exit criteria for Phase 2:** KML output is validated against a real geographic area and road scores match qualitative expectations.

### Phase 2: Refinement

**Hypothesis:** Real-world usage will reveal edge cases in aggregation (name collisions, way ordering ambiguity) and scoring (penalty tuning) that need adjustment.

**Scope:**
- Tune penalty multipliers based on real-world output review
- Handle edge cases: roads with the same name in different areas, disconnected way chains
- Add optional score normalization or ranking output
- Performance optimization if needed for large areas

**Success measurement:**
- No false-positive high-scoring roads in motorway/trunk category
- Road collections accurately represent contiguous drivable segments
- User feedback indicates output is actionable for ride planning

**Exit criteria for Phase 2:** Stable output quality across 3+ diverse geographic areas.

## Non-Goals

- Real-time or interactive map rendering (KML is a batch output format)
- Turn-by-turn routing or navigation
- Mobile app or web UI
- Multi-format output (GeoJSON, GPX, etc.) — KML only for now
- Elevation or gradient scoring
- Traffic or road condition data
- User accounts or saved preferences
- OSM data editing or contribution

## Success Criteria

1. The `score` subcommand produces a valid KML file that loads in Google Earth without errors
2. Per-segment coloring correctly reflects curvature tiers (visual verification against known roads)
3. Road names in the KML match OSM road names and group ways correctly
4. Straight-gap splitting produces distinct collections for twisty sections separated by long straights
5. Highway-type penalties visibly reduce the ranking of motorway/trunk roads relative to secondary/tertiary roads
6. The `-min-score` flag effectively filters low-scoring roads from output
7. End-to-end pipeline (stages 1-7) completes in reasonable time for a 25 km radius area

## Future Considerations

- **GeoJSON output:** An alternative output format for web-based map viewers. Worth designing the rendering interface to be pluggable rather than KML-specific.
- **Score normalization:** A 0-100 normalized score that accounts for road length, enabling fairer comparison between short and long roads.
- **Surface quality penalties:** The spec mentions surface quality as a soft factor. Could be added as an additional penalty multiplier using the `surface` tag.
- **Configurable parameters:** Exposing penalty multipliers, split threshold, and tier boundaries as CLI flags or a config file for user tuning.
