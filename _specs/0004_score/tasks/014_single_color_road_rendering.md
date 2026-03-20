---
# Task 014: Single-Color Per-Road KML Rendering

## Summary

Replace the default KML rendering with a single-color-per-road mode that colors each road uniformly based on its total curvature score, matching the Curvature project's `SingleColorKmlOutput`. The current per-segment tier coloring becomes an opt-in fallback via a CLI flag.

Roads like Birchrun Rd that are consistently curvy but have short straight sections between curves will now render as uniformly orange instead of a confusing mix of green and orange.

## Dependencies

None — this is independent of Tasks 012/013 (though it benefits from them).

## Detailed Directions

### 1. Add a Continuous Color Gradient Function in `quality/kml.go`

Implement Curvature's logarithmic color mapping. The function takes a `TotalScore` and returns a KML color string (AABBGGRR format).

**Algorithm** (from Curvature's `SingleColorKmlOutput.level_for_curvature`):

```go
const (
    DefaultMinCurvature = 0.0
    DefaultMaxCurvature = 4000.0
)

// CurvatureColorLevel maps a total curvature score to a color level (0-511)
// using a logarithmic scale for better visual differentiation at lower scores.
func CurvatureColorLevel(score, minCurvature, maxCurvature float64) int {
    if score < minCurvature {
        return 0
    }
    pct := (score - minCurvature) / (maxCurvature - minCurvature)
    if pct > 1 {
        pct = 1
    }
    // Logarithmic scale: y = 1 - 1/(10^(x*2))
    colorPct := 1 - 1/math.Pow(10, pct*2)
    return int(math.Round(510*colorPct)) + 1
}
```

**Color gradient** (511 levels):
- Levels 1–256: **yellow → red** gradient. Interpolate:
  - Red channel: FF (constant)
  - Green channel: FF → 00
  - Blue channel: 00 (constant)
- Levels 257–511: **red → magenta** gradient. Interpolate:
  - Red channel: FF (constant)
  - Green channel: 00 (constant)
  - Blue channel: 00 → FF

Remember KML uses AABBGGRR format (alpha, blue, green, red), not standard RGB.

```go
// GradientColor returns a KML AABBGGRR color string for the given level (0-511).
// Level 0 returns green (same as tier 0). Levels 1-256 are yellow→red.
// Levels 257-511 are red→magenta.
func GradientColor(level int) string {
    if level <= 0 {
        return TierColors[0] // green
    }
    if level <= 256 {
        // Yellow (FF,FF,00) → Red (FF,00,00)
        green := 255 - (level-1)*255/255
        // KML AABBGGRR: alpha=FF, blue=00, green=variable, red=FF
        return fmt.Sprintf("FF00%02XFF", green)
    }
    // Red (FF,00,00) → Magenta (FF,00,FF)
    blue := (level - 257) * 255 / 254
    // KML AABBGGRR: alpha=FF, blue=variable, green=00, red=FF
    return fmt.Sprintf("FF%02X00FF", blue)
}
```

### 2. Add `WriteKMLSingleColor` Function in `quality/kml.go`

Create a new KML writer that renders each road as a single-color polyline.

Key differences from `WriteKML`:
- No tier-based styles. Instead, each road folder gets an inline `<Style>` with its computed gradient color.
- Each road is rendered as a single `<Placemark>` with all segments' coordinates concatenated (no tier-run splitting).
- The road's `TotalScore` (pre-penalty, since penalty affects ranking but the visual should reflect actual curvature) determines the color. Or use `PenalizedScore` for consistency with ranking — document the choice and make it easy to change.

```go
func WriteKMLSingleColor(w io.Writer, collections []RoadCollection, minScore float64) error {
    // Filter and sort same as WriteKML
    // For each collection:
    //   level := CurvatureColorLevel(collection.TotalScore, DefaultMinCurvature, DefaultMaxCurvature)
    //   color := GradientColor(level)
    //   Render one folder with one placemark using that color for the entire road
}
```

The folder description should include the same metadata as the current renderer (score, per-km, length, types, ways).

### 3. Add `-multi-color` CLI Flag in `main.go`

In `runScore`:

```go
multiColor := fs.Bool("multi-color", false, "Use per-segment tier coloring instead of single-color per-road")
```

Then when writing KML:

```go
if *multiColor {
    err = quality.WriteKML(f, collections, *minScore)
} else {
    err = quality.WriteKMLSingleColor(f, collections, *minScore)
}
```

### 4. Add Constants to `quality/scoring_params.go`

```go
// DefaultMinCurvature is the minimum total curvature score for the single-color
// gradient. Roads below this score render as green. Matches the Curvature project default.
const DefaultMinCurvature = 0.0

// DefaultMaxCurvature is the total curvature score that maps to the maximum color
// intensity in single-color rendering. Roads at or above this score render as magenta.
// Matches the Curvature project default of 4000.
const DefaultMaxCurvature = 4000.0
```

These do not need to be in `ScoringParamsHash` — they only affect rendering.

### 5. Add Unit Tests

**`quality/kml_test.go`:**

- `TestCurvatureColorLevel`: Verify the logarithmic mapping:
  - Score 0 → level 0
  - Score 4000 → level 511
  - Score > 4000 → level 511 (clamped)
  - Score 2000 (midpoint) → verify it's well above 256 due to log scale (the log curve compresses high values)
  - Negative score → level 0

- `TestGradientColor`: Verify color output:
  - Level 0 → green (same as tier 0)
  - Level 1 → yellow-ish (AABBGGRR with high green, full red)
  - Level 256 → red (FF0000FF)
  - Level 511 → magenta (FFFF00FF)

- `TestWriteKMLSingleColor`:
  - Creates a few collections with different scores
  - Verifies valid XML output
  - Verifies each folder has exactly one placemark (not split by tier runs)
  - Verifies collections are sorted by penalized score descending
  - Verifies min-score and min-length filters still apply

- `TestWriteKMLSingleColor_MatchesWriteKML_Filtering`:
  - Same collections, same filters — verify the same roads are included/excluded in both modes

### 6. Update Integration Tests

In `quality/pipeline_integration_test.go`, add a test that runs the full pipeline through `WriteKMLSingleColor` and verifies:
- Valid XML output
- Expected folder count matches `WriteKML` (same filtering logic)
- Each folder has exactly one placemark

### 7. Update `main_test.go`

If there are tests that assert on KML output format, update them for the new default (single-color). Add a test that verifies `-multi-color` flag produces the old per-segment output.

## Acceptance Criteria

- [ ] `CurvatureColorLevel` implements Curvature's logarithmic score-to-level mapping
- [ ] `GradientColor` produces a continuous yellow→red→magenta gradient in AABBGGRR format
- [ ] `WriteKMLSingleColor` renders each road as a single-color polyline based on total score
- [ ] Default `twisty score` output uses single-color rendering
- [ ] `-multi-color` flag falls back to per-segment tier coloring
- [ ] Min-score and min-length filters work identically in both modes
- [ ] Unit tests cover color mapping, gradient output, and KML generation
- [ ] Integration test verifies full pipeline through single-color output
- [ ] `go test -race ./...` passes

## Notes

- The Curvature project uses `TotalScore` (absolute, not per-km) for the single-color gradient. This means longer curvy roads are more colorful than short ones — intentional, since a 10-mile winding road is a better ride than a 1-mile one.
- The default max of 4000 is a tuning parameter. After running on real data, this may need adjustment. It's a constant in `scoring_params.go`, easy to change.
- The logarithmic scale is important — it gives good visual differentiation for roads in the 0–1000 range rather than making everything look the same until it hits 4000.
- Consider whether to use `TotalScore` or `PenalizedScore` for the color. `TotalScore` reflects actual curvature; `PenalizedScore` reflects "ride quality" (highways penalized). Start with `TotalScore` for the color since the visual should reflect actual road geometry, while `PenalizedScore` is used for filtering and sort order.
- The inline style approach (one style per folder) avoids needing 511 shared style definitions in the document header.
