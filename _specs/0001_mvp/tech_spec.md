# Technical Specification: TwistRouter

**Related PRD:** `_specs/0001_mvp/prd.md`

## Overview

This document covers the implementation detail for TwistRouter: API contracts, data structures, algorithms, error handling, and package layout. The PRD describes what the system does; this document describes how to build it.

---

## Package Layout

```
twistrouter/
├── main.go            # flag parsing, pipeline orchestration, summary output
├── geo/
│   └── geo.go         # haversine, bearing, angle-difference, polyline decoder
├── geocode/
│   └── nominatim.go   # Nominatim client, input classification, disambiguation
├── route/
│   ├── osrm.go        # OSRM client, response parsing
│   └── score.go       # curvature scoring, twist-factor selection
├── quality/
│   └── overpass.go    # Overpass client, way matching, penalty computation
└── gpx/
    └── gpx.go         # GPX 1.1 writer
```

---

## Data Types

### Core Types (`geo` package)

```go
type Coord struct {
    Lat float64
    Lon float64
}
```

### Route Types (`route` package)

```go
type Route struct {
    Points   []geo.Coord
    Duration float64  // seconds
    Distance float64  // meters
    Stats    CurvatureStats
}

type CurvatureStats struct {
    Indirectness   float64  // straight-line / road distance (0–1; lower = more indirect)
    AngularDensity float64  // degrees of heading change per km
    Score          float64  // combined curvature score (higher = twistier)
    AdjustedScore  float64  // score after road quality penalties applied
}
```

---

## API Contracts

### Nominatim (Geocoding)

**Endpoint:**
```
GET https://nominatim.openstreetmap.org/search?q={url_encoded_query}&format=jsonv2&limit=5
```

**Required headers:**
```
User-Agent: twistrouter/1.0
```

**Response (relevant fields):**
```json
[
  {
    "lat": "37.7749295",
    "lon": "-122.4194155",
    "display_name": "San Francisco, California, United States",
    "type": "city",
    "importance": 0.923
  }
]
```

Note: `lat` and `lon` are strings. Parse to float64.

**Rate limit:** 1 request/second. Sleep 1 second between geocoding requests if both origin and destination require geocoding.

---

### OSRM (Route Alternatives)

**Endpoint:**
```
GET https://router.project-osrm.org/route/v1/driving/{originLon},{originLat};{destLon},{destLat}?alternatives=true&overview=full&geometries=polyline&steps=true
```

Note: OSRM coordinate order is `longitude,latitude` (reversed from standard).

**Query parameters:**

| Parameter | Value | Purpose |
|-----------|-------|---------|
| `alternatives` | `true` | Request multiple route options (typically 2–3) |
| `overview` | `full` | Return complete route geometry, not simplified |
| `geometries` | `polyline` | Google Encoded Polyline format, precision 5 |
| `steps` | `true` | Return turn-by-turn steps |

**Response (relevant fields):**
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

**Error handling:** If `code != "Ok"` or `routes` is empty, print the code and exit 1.

---

### Overpass API (Road Quality)

**Endpoint:**
```
POST https://overpass-api.de/api/interpreter
Content-Type: application/x-www-form-urlencoded
Body: data=<query>
```

**Query template:**
```
[out:json][timeout:10];
way["highway"](south,west,north,east);
out tags geom;
```

Where `south,west,north,east` is the combined bounding box of all candidate routes, expanded by 0.001° on each side.

**Response (relevant fields):**
```json
{
  "elements": [
    {
      "type": "way",
      "id": 12345678,
      "tags": {
        "highway": "secondary",
        "surface": "asphalt",
        "access": "yes"
      },
      "geometry": [
        { "lat": 37.7749, "lon": -122.4194 },
        { "lat": 37.7751, "lon": -122.4190 }
      ]
    }
  ]
}
```

**Failure handling:** Network error, timeout, or non-200 response → print warning, skip road quality filtering, continue. Do not exit.

---

## Algorithms

### Input Classification

```
func classifyInput(s string) (isCoord bool, lat, lon float64)
```

1. Split on `,`. If result is not exactly 2 parts, treat as address.
2. Attempt to parse both parts as float64. If either fails, treat as address.
3. If both parse successfully, return `isCoord=true` with the parsed values.

### Google Encoded Polyline Decoder (precision 5)

Decodes a string into `[]geo.Coord`. Algorithm:

1. Maintain a running `lat` and `lon` accumulator (integers), both starting at 0.
2. For each coordinate value (lat and lon alternate):
   a. Read bytes until a byte < 0x20 is found (i.e., byte value after subtracting 63 is < 32).
   b. For each byte: subtract 63, mask the low 5 bits, shift left by the current chunk index * 5, OR into accumulator.
   c. If the lowest bit is set, the value is negative: apply one's complement (`value = ~value`).
   d. Right-shift by 1 to get the actual delta.
   e. Add delta to the running accumulator.
3. Divide the final integer accumulator by 1e5 to get the float coordinate.

### Haversine Distance (meters)

```
R = 6371000
dLat = (lat2 - lat1) * π/180
dLon = (lon2 - lon1) * π/180
a = sin²(dLat/2) + cos(lat1*π/180) * cos(lat2*π/180) * sin²(dLon/2)
c = 2 * atan2(√a, √(1-a))
distance = R * c
```

### Bearing (degrees, 0–360)

```
dLon = (lon2 - lon1) * π/180
y = sin(dLon) * cos(lat2 * π/180)
x = cos(lat1*π/180) * sin(lat2*π/180) - sin(lat1*π/180) * cos(lat2*π/180) * cos(dLon)
bearing = atan2(y, x) * 180/π
bearing = math.Mod(bearing + 360, 360)
```

### Angle Difference (handles 360°/0° wraparound)

```
diff = b - a
for diff > 180  { diff -= 360 }
for diff < -180 { diff += 360 }
return math.Abs(diff)
```

### Curvature Scoring

**Indirectness ratio:**
```
indirectness = haversine(points[0], points[n-1]) / totalRoadDistance
```

**Angular density:**
```
totalHeadingChange = 0
for i = 1; i < len(points)-1; i++:
    b1 = bearing(points[i-1], points[i])
    b2 = bearing(points[i], points[i+1])
    totalHeadingChange += angleDiff(b1, b2)

angularDensity = totalHeadingChange / (totalRoadDistance / 1000)  // °/km
```

**Combined score:**
```
score = angularDensity * 0.7 + (1.0 - indirectness) * 1000 * 0.3
```

### Road Quality Penalty

**Way-to-route matching:**
For each point in the route polyline, find the nearest Overpass way whose geometry contains a node within 30 meters. Assign that way's tags to the point. Accumulate distance-weighted fractions by walking adjacent point pairs and checking the tags on the midpoint.

**Penalty computation:**
```
disqualified_fraction = distance_on_disqualifying_roads / total_route_distance
penalized_fraction    = distance_on_penalty_roads       / total_route_distance

adjusted_score = score * (1.0 - disqualified_fraction) * (1.0 - 0.5 * penalized_fraction)
```

Disqualifying tags:
- `access=private` or `access=no`
- `highway` ∈ {`track`, `path`, `footway`, `cycleway`}
- `motor_vehicle=no` or `motor_vehicle=private`

Penalty tags:
- `surface` ∈ {`unpaved`, `gravel`, `dirt`, `mud`, `sand`} — full soft penalty
- `surface` ∈ {`compacted`, `fine_gravel`} — half soft penalty
- `highway=unclassified` with no `surface` tag — quarter soft penalty
- `highway=service` — quarter soft penalty

### Twist-Factor Route Selection

```
// Normalize durations: 0 = longest, 1 = shortest
// Normalize adjusted scores: 0 = lowest, 1 = highest

selectionScore[i] = twist * normalizedScore[i] + (1 - twist) * (1 - normalizedDuration[i])
```

Pick route with highest `selectionScore`. If only one route, use it.

Min-max normalization:
```
normalized[i] = (value[i] - min) / (max - min)
```
When all values are equal (max == min), normalized = 1.0 for all.

---

## GPX Output Format

Use `encoding/xml`. Write the XML declaration manually before marshaling (Go's xml encoder does not emit it by default):

```go
file.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
```

Schema:
```xml
<gpx version="1.1" creator="twistrouter" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Twist Route (factor=0.8, score=142)</name>
    <trkseg>
      <trkpt lat="37.7749" lon="-122.4194"/>
      ...
    </trkseg>
  </trk>
</gpx>
```

Track name format: `Twist Route (factor=%.1f, score=%.0f)` using the twist flag value and the adjusted score of the selected route.

---

## Error Handling Reference

| Condition | Behavior | Exit code |
|-----------|----------|-----------|
| Missing `-origin` or `-dest` | Print usage | 1 |
| Input looks like coords but fails to parse | `Bad origin: expected lat,lon` | 1 |
| Nominatim returns 0 results | `Error: could not geocode origin/dest "<input>" — no results found` | 1 |
| Nominatim network error | Print error | 1 |
| OSRM network error | Print error | 1 |
| OSRM `code != "Ok"` | Print the code | 1 |
| OSRM returns 0 routes | `No routes found` | 1 |
| Overpass network error / timeout | Print warning, skip road quality, continue | — |
| File write error | Print error | 1 |

---

## Testing

### Recommended Test Routes

| Route | What to verify |
|-------|---------------|
| `-origin "San Francisco, CA" -dest "San Jose, CA"` | Highway 101 (fast/straight) vs Page Mill Rd / Hwy 9 / Skyline (twisty). Should select different routes at `-twist 0.0` vs `-twist 1.0`. |
| `-origin "Santa Monica, CA" -dest "Malibu, CA"` | PCH coastal curves vs inland straight. |
| Any mountain pass route | Switchbacks should score dramatically higher than flat alternatives. |
| `-origin "37.7749,-122.4194" -dest "37.3382,-121.8863"` | Raw coordinate input; must skip geocoding and work correctly. |
| `-origin "Springfield"` | Ambiguous address; must print top 3 disambiguations. |

### Verification Checklist

- [ ] `-twist 0.0` selects the fastest OSRM route
- [ ] `-twist 1.0` selects the route with highest angular density
- [ ] GPX imports into OsmAnd and renders as a road-following track
- [ ] GPX imports into Google My Maps and renders correctly
- [ ] Address inputs print resolved name and coordinates
- [ ] Ambiguous addresses print top 3 matches with override hint
- [ ] Raw coordinate inputs skip geocoding
- [ ] Overpass failure prints warning and produces output anyway
- [ ] `-show-all` prints comparison table before selection line

### Known Limitations (Document in README)

1. **Limited alternatives.** OSRM typically returns only 2–3 alternatives. The tool cannot synthesize novel routes through known-twisty roads.
2. **Public API servers.** OSRM, Nominatim, and Overpass are shared public resources. Not suitable for heavy automated use; run local instances for that.
3. **Road quality filtering is approximate.** The ~30m proximity matching is a heuristic; it may miss short private segments or misattribute a nearby parallel road's tags.
4. **No elevation data.** Mountain switchbacks and flat twisty roads score similarly.
5. **Polyline resolution.** OSRM's encoded polylines may slightly underreport curvature. `overview=full` mitigates but does not eliminate this.
