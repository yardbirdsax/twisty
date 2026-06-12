# Build: Twistiness Score Per Km

**Date:** 2026-06-13
**Branch:** feat/route-build

## Problem

The current "Twist Score" in the build UI stat box is a raw sum of `segment_length × curvature_weight` across all segments on the route. This value grows with route distance, making it useless for comparing routes of different lengths — a long highway route can outscore a short twisty one just by being longer.

The goal is a metric that goes up when you pick a twistier route and down when you add highway miles, regardless of total distance.

## Solution

Replace the raw score with **score per km** — total twist score divided by total route distance in km. This is a purely relative metric: the user just wants to see the number move up or down as they adjust waypoints. Absolute interpretability is not required.

Color-code the displayed value using the same yellow→red→magenta gradient used by the map overlay, so the stat box gives an immediate visual signal matching the map.

## Architecture

### Backend

**`scoreFromCachedTiles` (`route_build.go`):** Change return type from `float64` to a small struct (or two named return values) carrying both `totalScore` and `scorePerKm`. Track `totalLength` (sum of `seg.Length` in meters) alongside the existing `totalScore`. Compute `scorePerKm = totalScore / (totalLength / 1000)`. If `totalLength == 0`, `scorePerKm` is 0.

**`scoreResponse` struct:** Add a `ScorePerKm float64 \`json:"score_per_km"\`` field. Both `Score` (raw, kept for backwards compatibility) and `ScorePerKm` are returned.

**New constant (`scoring_params.go`):** `DefaultMaxCurvaturePerKm = 2000.0` — the score/km value that maps to full magenta on the gradient. Derived heuristically: a maximally twisty road collection with `TotalScore=8000` over ~4 km yields ~2000/km. Tunable.

### Frontend

**Stat box label:** "Twist Score" → "Twistiness".

**Stat box value:** Display `data.score_per_km` (rounded to nearest integer) instead of `data.score`. When `score_per_km` is 0 or absent, show `—`.

**Color coding:** Port the `CurvatureColorLevel` + `GradientColorCSS` logic to JS inline in `route_build.go`. Apply the resulting CSS color to the score value element's text color. Scale: 0 → green, 1–`maxPerKm` → yellow→red→magenta (logarithmic), using `DefaultMaxCurvaturePerKm` baked in as a JS constant.

**Viewport score path:** The viewport score endpoint (`/api/score/viewport`) also returns a `scoreResponse`. Since viewport mode has no route drawn, `score_per_km` will be 0 and the stat box will show `—` — same behavior as today.

## Data Flow

```
User drags waypoint
  → requestScore() POSTs route points to /api/score
  → backend sums seg.Score and seg.Length for matched segments
  → returns { score, score_per_km, pending_tiles, failed_tiles }
  → JS reads score_per_km, computes color level, sets text + color
```

## Error Handling

- If `score_per_km` is missing or 0 (no segments matched, or route not yet scored), display `—` with no color applied.
- Pending/failed tile states already handled by existing poll-retry logic — no change.

## Testing

- **Go test:** Add a test asserting `/api/score` response includes `score_per_km > 0` when route points are provided and tiles are cached. Mirror the existing `TestHandleScore_*` pattern.
- **HTML test:** Add a test asserting the rendered HTML contains `score_per_km`, `DefaultMaxCurvaturePerKm` (or the JS constant name), and the updated label "Twistiness".
- **Unit test:** Add a test for `scoreFromCachedTiles` asserting it returns a non-zero `scorePerKm` when segments have length.
- **KML export:** No test changes needed — that code path is untouched.

## What Is Not Changing

- KML export coloring (`WriteKMLSingleColor`) — uses `col.TotalScore` on `RoadCollection`, completely separate.
- Viewport score behavior — stays as `—` when no route is drawn.
- The raw `score` field in the API response — kept for backwards compatibility.
- All other stat box rows (Distance, Time).
