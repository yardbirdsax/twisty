# Product Requirements Document: TwistRouter

## Overview

TwistRouter is a CLI tool that finds the twistiest driving route between two points and outputs a GPX file. It operates in a problem space where standard routing tools optimize exclusively for speed or distance — ignoring the preference many drivers have for winding, scenic, or technically engaging roads. The core insight is that road curvature can be quantified from geometry alone, enabling preference-weighted route selection without any specialized data source.

## Problem Statement

Standard navigation tools (Google Maps, Apple Maps, OsmAnd) optimize for speed or distance. Drivers who want twisty roads — motorcyclists, driving enthusiasts, scenic tourists — have no automated way to find the curviest viable route between two points.

1. **No curvature preference in consumer tools.** Existing routers treat all roads as equivalent beyond speed and distance. A rider who wants switchbacks between San Francisco and San Jose has no tool to find the Page Mill Road route instead of Highway 101 — they rely on local knowledge or manual map inspection.

2. **Twisty roads are disproportionately inaccessible.** Curvy roads are more likely to be unpaved, private, or restricted-access (forest service roads, agricultural tracks). A naive curvature-maximizing tool without road quality filtering will reliably route users onto roads they cannot or should not drive.

3. **No interpolation between "fastest" and "twistiest."** Users may want to trade some time for twistiness without fully optimizing for curves. There is no existing tool that lets a user dial in this preference continuously.

## Users

Driving enthusiasts — motorcyclists, sports car drivers, and scenic touring drivers — who are planning routes in advance and want to maximize road engagement or scenic quality. These users are comfortable with a CLI tool and will use the output GPX file in a navigation app (OsmAnd, Google My Maps, etc.) for in-car navigation. They understand that the tool produces a suggestion, not a guarantee, and will sanity-check routes before driving.

## Design Principles

### 1. Free and Zero-Config

The tool should work out of the box with no API keys, accounts, or configuration. All data sources (Nominatim, OSRM, Overpass) are free and unauthenticated. This keeps the tool accessible and deployable as a single binary.

### 2. Pipeline Transparency

Each step of the pipeline should produce visible output. The user should know what addresses resolved to, how many routes were considered, what their scores were, and why one was selected. Opacity in route selection erodes trust in a tool that is making a meaningful travel recommendation.

### 3. Soft Failures Over Hard Gates

The road quality check (Overpass) is a safety layer, not a hard dependency. If it fails, the tool continues with a warning rather than blocking output. Route generation should succeed whenever the core routing pipeline (geocoding + OSRM) succeeds.

### 4. Geometry Over Heuristics

Curvature scoring is derived directly from route geometry — heading changes per km, indirectness ratio — rather than road tags, administrative classifications, or speed limits. This is more accurate for the actual driving experience and is available from any polyline without additional data.

### 5. No Novel Route Synthesis

The tool selects among routes OSRM already knows about. It does not inject waypoints or synthesize paths through known-twisty segments. This constraint keeps the MVP tractable and reliable; OSRM's routing graph handles road connectivity, legality, and traversability.

## Feature 1: Address and Coordinate Input Resolution

### Behavioral / Functional Goal

Users should be able to specify origin and destination as plain addresses or place names, not just raw coordinates. The tool should confirm what it resolved to and surface ambiguity so users can correct mismatches before a route is generated.

### Trigger / Entry Point

Invocation via `-origin` and `-dest` flags. Each value is independently resolved at startup before any routing is attempted.

### Core Behavior

Each input is classified as either raw coordinates or an address:

- **Raw coordinates:** input contains exactly one comma and both sides parse as float64. Used directly, no geocoding.
- **Address/place name:** anything else. Geocoded via Nominatim.

For Nominatim geocoding:
- Request up to 5 results (`limit=5`).
- If 1 result: use it, print confirmation.
- If multiple results: use the first (highest relevance), print the top 3 with a hint to use coordinates to override.
- If 0 results: print error and exit with code 1.
- If both inputs need geocoding, sleep 1 second between requests to respect Nominatim's 1 req/sec rate limit.

Confirmation output (1 result):
```
Origin: "San Francisco, CA" → San Francisco, California, United States (37.7749, -122.4194)
```

Disambiguation output (multiple results):
```
Origin: "Springfield" → resolved to Springfield, Illinois, United States (39.7817, -89.6501)
  Also matched: Springfield, Missouri, United States (37.2090, -93.2923)
  Also matched: Springfield, Massachusetts, United States (42.1015, -72.5898)
  (Use coordinates directly to override, e.g. -origin "39.7817,-89.6501")
```

### Rules and Constraints

- `User-Agent: twistrouter/1.0` must be sent on all Nominatim requests (required by ToS).
- `lat` and `lon` in Nominatim responses are strings; parse to float64.
- If input looks like coordinates but fails to parse, print `Bad origin: expected lat,lon` and exit 1.
- Geocoding network errors exit with code 1.

### Data Model

```json
{
  "lat": "37.7749295",
  "lon": "-122.4194155",
  "display_name": "San Francisco, California, United States",
  "type": "city",
  "importance": 0.923
}
```

## Feature 2: Route Fetching and Geometry Decoding

### Behavioral / Functional Goal

Fetch multiple candidate driving routes between the resolved coordinates, producing a set of decoded polylines for scoring.

### Trigger / Entry Point

Executes after both inputs are resolved to coordinates.

### Core Behavior

Requests routes from the public OSRM demo server with `alternatives=true`, `overview=full`, `geometries=polyline`, and `steps=true`. OSRM typically returns 2–3 alternatives.

Each route's `geometry` field is a Google Encoded Polyline (precision 5). The tool decodes each to a slice of `{Lat, Lon}` coordinate pairs using the standard algorithm (delta-encoded 5-bit variable-length quantities, offset by 63, with one's complement for negatives).

### Rules and Constraints

- OSRM uses `longitude,latitude` coordinate order (not `lat,lon`).
- If `code` is not `"Ok"` or no routes are returned: print error and exit 1.
- Do not make rapid repeated requests to the OSRM demo server.

### Data Model

```json
{
  "code": "Ok",
  "routes": [
    {
      "geometry": "<encoded polyline string>",
      "duration": 1234.5,
      "distance": 45678.9,
      "legs": [
        {
          "steps": [
            {
              "geometry": "<encoded polyline>",
              "name": "Road Name"
            }
          ]
        }
      ]
    }
  ]
}
```

## Feature 3: Curvature Scoring

### Behavioral / Functional Goal

Quantify how twisty each candidate route is, producing a score that reflects actual driving engagement — curves per kilometer — rather than just total length or time.

### Trigger / Entry Point

Executes on each decoded polyline after route fetching.

### Core Behavior

Two metrics are computed per route:

**Indirectness Ratio:**
```
indirectness = haversine(first_point, last_point) / sum_of_segment_distances
```
Ranges from ~0 (very indirect) to 1.0 (perfectly straight). Lower = potentially twistier.

**Angular Density (°/km):**
For every three consecutive points (P[i-1], P[i], P[i+1]):
1. Compute bearing P[i-1]→P[i] and bearing P[i]→P[i+1].
2. Take absolute heading change, handling 360°/0° wraparound.
3. Sum all heading changes, divide by total road distance in km.

Higher = more curves per km.

**Combined Score:**
```
score = angular_density * 0.7 + (1.0 - indirectness) * 1000 * 0.3
```
The `(1 - indirectness) * 1000` normalizes indirectness to a comparable scale. 0.7/0.3 weighting prioritizes actual curviness over mere indirectness (which can result from detours on straight roads).

### Rules and Constraints

- All geo math uses float64.
- Haversine uses R = 6371000 meters.
- Bearing: `atan2(sin(dLon)*cos(lat2), cos(lat1)*sin(lat2) - sin(lat1)*cos(lat2)*cos(dLon))`, converted to degrees mod 360.
- Angle difference wraps correctly: subtract 360 while > 180, add 360 while < -180.
- Score formula is a starting point and may need tuning after real-world testing.

### Data Model

```go
type CurvatureStats struct {
    Indirectness   float64  // straight-line / road distance (0–1, lower = more indirect)
    AngularDensity float64  // degrees of heading change per km
    Score          float64  // combined curvature score (higher = twistier)
}
```

## Feature 4: Road Quality Filtering

### Behavioral / Functional Goal

Penalize routes that use private, restricted, or unpaved roads so that curvature scoring doesn't reliably recommend roads the user can't or shouldn't drive.

### Trigger / Entry Point

Executes after curvature scoring, before route selection. If Overpass is unavailable, this feature is skipped entirely with a warning.

### Core Behavior

Queries the Overpass API for all OSM highway ways within a single bounding box covering all candidate routes (with ~0.001° buffer). For each route, matches polyline points to nearby ways (within ~30m) and computes the fraction of route distance on penalized or disqualified segments.

**Disqualifying tags** (hard penalty — score multiplied to near zero):
- `access=private` or `access=no`
- `highway=track`, `highway=path`, `highway=footway`, `highway=cycleway`
- `motor_vehicle=no` or `motor_vehicle=private`

**Penalty tags** (soft penalty — score reduced):
- `surface=unpaved`, `surface=gravel`, `surface=dirt`, `surface=mud`, `surface=sand`
- `surface=compacted`, `surface=fine_gravel` (light penalty)
- `highway=unclassified` with no `surface` tag (mild penalty)
- `highway=service` (mild penalty)

**Score adjustment:**
```
adjusted_score = score * (1.0 - disqualified_fraction) * (1.0 - 0.5 * penalized_fraction)
```

If any route has `disqualified_fraction > 0.10`, print:
```
⚠ Route 2: ~15% on private/restricted roads — score penalized
```

**Fallback on failure:**
```
Warning: Could not check road quality (Overpass API unavailable). Results may include unpaved or private roads.
```

### Rules and Constraints

- Use a single Overpass query covering all routes (not one per route).
- Query uses POST to `https://overpass-api.de/api/interpreter` with `Content-Type: application/x-www-form-urlencoded`.
- Timeout: 10 seconds.
- Road quality check is a soft failure — do NOT exit on Overpass error.
- Proximity matching (~30m) is a heuristic; precision is not required.

### Data Model

Overpass query:
```
[out:json][timeout:10];
way["highway"](south,west,north,east);
out tags geom;
```

## Feature 5: Twist-Factor Route Selection

### Behavioral / Functional Goal

Let users continuously dial between "fastest route" and "twistiest route" so the tool accommodates both time-constrained and experience-maximizing trips.

### Trigger / Entry Point

Executes after road quality scoring. Uses the `-twist` flag value (0.0–1.0, default 0.5).

### Core Behavior

1. Normalize all route durations to [0, 1] via min-max normalization.
2. Normalize all adjusted twist scores to [0, 1] via min-max normalization.
3. Compute selection score per route:
   ```
   selection = twist * normalized_twist_score + (1 - twist) * (1 - normalized_duration)
   ```
   Where `(1 - normalized_duration)` means lower duration = higher score.
4. Select the route with the highest selection score.

When OSRM returns only one route, use it regardless of twist factor.

### Rules and Constraints

- `-twist 0.0` should select the fastest route.
- `-twist 1.0` should select the twistiest route.
- Normalization is across the candidate set only (not some global scale).

## Feature 6: GPX Output

### Behavioral / Functional Goal

Produce a standard GPX 1.1 file that imports cleanly into OsmAnd, Google My Maps, or any GPX-compatible navigation app, with the route's polyline encoded as a track.

### Trigger / Entry Point

Executes after route selection. Writes to the path specified by `-out` (default: `route.gpx`).

### Core Behavior

Writes a valid GPX 1.1 file using Go's `encoding/xml` package. The track name includes the twist factor and score for reference.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="twistrouter" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Twist Route (factor=0.8, score=142)</name>
    <trkseg>
      <trkpt lat="37.7749" lon="-122.4194"/>
      <!-- all decoded polyline points -->
    </trkseg>
  </trk>
</gpx>
```

### Rules and Constraints

- Include the XML declaration header (`<?xml version="1.0" encoding="UTF-8"?>`).
- GPX track must visually follow roads, not draw straight lines between waypoints.
- File write errors: print error and exit 1.

## Feature 7: CLI Interface and Summary Output

### Behavioral / Functional Goal

Provide a simple, discoverable CLI with clear output so users understand what was resolved, what was considered, and what was selected.

### Trigger / Entry Point

Program entry point; flags parsed at startup.

### Core Behavior

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-origin` | string | required | Address/place name or `lat,lon` |
| `-dest` | string | required | Address/place name or `lat,lon` |
| `-twist` | float64 | `0.5` | 0.0 = fastest, 1.0 = twistiest |
| `-out` | string | `route.gpx` | Output GPX file path |
| `-show-all` | bool | `false` | Print comparison table of all candidates |

**Always printed:**
- Resolved origin and destination
- Number of routes fetched
- Selected route: distance (km), duration (min), angular density (°/km), twist score, point count

**With `-show-all`:**
```
Route comparison:
------------------------------------------------------------------------
Route    Distance     Duration   Angular°/km      Twist Score
------------------------------------------------------------------------
1        48.2         32         85.3             142.1
2        52.7         38         124.6            210.4
3        55.1         41         98.2             178.9
------------------------------------------------------------------------
  Distance in km, Duration in minutes
```

### Rules and Constraints

- Missing `-origin` or `-dest`: print usage and exit 1.
- `-show-all` prints the table before printing the selection.

## Technical Direction

Implementation detail — API contracts, data structures, algorithms, polyline decoding, geo math, error handling, and package layout — is in `_specs/0001_mvp/tech_spec.md`. This section covers only the decisions that constrain or inform that spec.

### Language and Dependencies

Go, standard library only. No external Go modules. HTTP, JSON, XML, math, flags, and I/O are all available in stdlib. The binary should be self-contained and cross-compilable.

### Geo Math

Implement haversine distance, bearing, and angle-difference functions from scratch using `math` package. All computations in float64. These are small, pure functions suitable for a `geo` package within the project.

### Pipeline Structure

The tool is a sequential pipeline: resolve inputs → fetch routes → decode geometries → score curvature → check road quality → select route → write GPX → print summary. Each stage has clear inputs and outputs. Error handling at each stage either exits (hard failure) or continues with warning (soft failure — Overpass only).

### External APIs

| Service | Purpose | Auth | Rate limit |
|---------|---------|------|------------|
| Nominatim (`nominatim.openstreetmap.org`) | Geocoding | None (requires User-Agent) | 1 req/sec |
| OSRM (`router.project-osrm.org`) | Route alternatives | None | Shared public server |
| Overpass (`overpass-api.de`) | Road quality tags | None | Shared public server |

### Integration with Existing System

This is a greenfield CLI tool. No existing codebase to integrate with.

New code lives in:
- `main.go` — CLI flag parsing, pipeline orchestration, summary output
- `geo/` — haversine, bearing, angle-difference math
- `geocode/` — Nominatim client and response parsing
- `route/` — OSRM client, polyline decoder, curvature scoring, route selection
- `quality/` — Overpass client and road quality penalty computation
- `gpx/` — GPX file writer

## Phasing

### Phase 1: Core Pipeline (MVP)

**Hypothesis:** A curvature score derived from route geometry is sufficient to identify meaningfully twistier routes from the alternatives OSRM already returns.

**Scope:**
- All 7 features described above
- Standard library only, no external Go modules
- Public API servers (Nominatim, OSRM, Overpass)

**Success measurement:**
- With `-twist 0.0`, tool selects the fastest OSRM route
- With `-twist 1.0`, tool selects the most angular route
- GPX imports cleanly into OsmAnd and Google My Maps and visually follows roads
- Address inputs resolve correctly; ambiguous inputs print disambiguation
- Raw coordinate inputs work and skip geocoding
- Overpass failure produces a warning but does not block output
- Minimum measurement period: 10 manual test runs across varied route types

**Exit criteria for Phase 2:** Curvature scoring produces visibly different route selections across at least 3 diverse test corridors (e.g. SF→SJ, PCH coastal, mountain pass).

### Phase 2: Curvature Database + Waypoint Injection (Post-MVP)

**Hypothesis:** Pre-computing curvature scores for all OSM road segments and injecting high-scoring waypoints will surface routes OSRM would never return as alternatives.

**Scope:**
- Pre-compute curvature index from OSM data
- Identify high-scoring segments within a corridor around the baseline route
- Inject as OSRM intermediate waypoints
- Evaluate quality vs. Phase 1

**Success measurement:**
- Tool finds routes not in OSRM's alternative set that score higher on curvature
- Routes remain driveable (no regressions on road quality filtering)
- Minimum measurement period: 20 test runs

**Exit criteria for Phase 3:** Waypoint injection consistently produces better-scoring routes than OSRM alternatives alone, without introducing access or surface problems.

### Phase 3: Elevation Weighting

**Hypothesis:** Boosting scores for roads with combined elevation gain and curvature (mountain passes) better reflects driving engagement than angular density alone.

**Scope:**
- Integrate SRTM or equivalent elevation data
- Add elevation-weighted curvature metric
- Expose as optional flag or automatic enhancement

**Success measurement:**
- Mountain pass routes score higher relative to flat twisty roads
- No regressions on flat-terrain test cases

## Non-Goals

- No UI of any kind — CLI only
- No API key management or authentication flows
- No novel route synthesis in Phase 1 (waypoint injection is Phase 2)
- No elevation data in Phase 1
- No named road preferences ("prefer Highway 1")
- No support for non-driving modes (cycling, walking)
- Not suitable for heavy automated use against public API servers

## Success Criteria

1. With `-twist 0.0`, the tool selects the fastest OSRM-returned route; with `-twist 1.0`, it selects the most angular one.
2. The output GPX file imports into OsmAnd and Google My Maps and renders as a track that visually follows roads (not straight-line waypoints).
3. Address disambiguation is clear enough that a user can detect and correct a wrong resolution before a route is generated.
4. Road quality filtering reduces the frequency of routes through private or unpaved roads compared to unfiltered curvature selection.
5. (Qualitative) On known twisty corridors (SF→SJ via Page Mill Road, Santa Monica→Malibu via PCH, any mountain pass), the tool selects the road that an experienced driver would recognize as the twistiest viable option.

## Future Considerations

- **Curvature database + waypoint injection:** Pre-computing OSM segment curvature and injecting waypoints would let the tool find routes OSRM never surfaces as alternatives. Requires a build step to generate the index from OSM data; architecture should not preclude adding this later.
- **Elevation weighting:** SRTM data could boost scores for roads with elevation gain + curvature. The scoring formula should be designed to accept additional weighted terms without restructuring.
- **Self-hosted API stack:** Users with high query volume should be able to point the tool at local OSRM, Nominatim, and Overpass instances. API base URLs should be configurable (env vars or flags) rather than hardcoded.
- **Named road preferences and avoidances:** "Prefer Highway 1", "avoid freeways" — would require integrating road name metadata from OSRM steps or Overpass tags into the scoring model.
