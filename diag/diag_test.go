package diag

import (
	"testing"

	"github.com/yardbirdsax/twisty/quality"
)

func TestTracePA339(t *testing.T) {
	cfg := DefaultCacheConfig()

	// Royersford, PA approximate center
	centerLat, centerLon := 40.1843, -75.5381
	radiusKm := 300.0
	tileSizeDeg := 1.0

	tiles := quality.ComputeTiles(centerLat, centerLon, radiusKm, tileSizeDeg)
	t.Logf("Total tiles in grid: %d", len(tiles))

	// Step 1: Find PA 339 in the overpass cache
	matches, err := FindWaysInOverpassCache(tiles, cfg, WayMatchesRef("339"))
	if err != nil {
		t.Fatalf("FindWaysInOverpassCache: %v", err)
	}
	matches = DeduplicateWayMatches(matches)
	t.Logf("Found %d unique ways matching ref containing '339'", len(matches))

	// Filter to just PA 339
	var pa339Ways []WayMatch
	for _, m := range matches {
		ref := m.Way.Tags["ref"]
		if ref == "PA 339" || ref == "SR 339" {
			pa339Ways = append(pa339Ways, m)
		}
	}
	t.Logf("PA 339 ways: %d", len(pa339Ways))

	for _, m := range pa339Ways {
		t.Logf("  Way %d: name=%q ref=%q highway=%s maxspeed=%q nodes=%d",
			m.Way.ID, m.Way.Tags["name"], m.Way.Tags["ref"],
			m.Way.Tags["highway"], m.Way.Tags["maxspeed"], len(m.Way.Geometry))
	}

	// Step 2: Run the full pipeline simulation with detail on PA 339's group key
	result, err := SimulatePipelineFull(tiles, cfg, SimulatePipelineOptions{
		DetailKeys: []string{"PA 339"},
	})
	if err != nil {
		t.Fatalf("SimulatePipelineFull: %v", err)
	}

	// Step 3: Find PA 339 collections
	pa339Collections := FindCollections(result.Collections, "PA 339")
	t.Logf("\n--- PA 339 Collections (%d total) ---", len(pa339Collections))
	for _, c := range pa339Collections {
		t.Log(FormatCollectionSummary(c, 0))
		// Show speed info
		passingFrac := quality.SpeedPassingFraction(c.WaySpeeds, c.TotalLength, 35)
		t.Logf("  Speed filter (min=35mph): passing fraction=%.2f (need >=0.50)", passingFrac)
		hasSpeedCount := 0
		noSpeedCount := 0
		var hasSpeedLength, noSpeedLength float64
		for _, s := range c.WaySpeeds {
			if s.HasSpeed {
				hasSpeedCount++
				hasSpeedLength += s.LengthM
				t.Logf("    Way with speed: %.0f mph, %.0f m", s.SpeedMPH, s.LengthM)
			} else {
				noSpeedCount++
				noSpeedLength += s.LengthM
				t.Logf("    Way WITHOUT speed tag: %.0f m", s.LengthM)
			}
		}
		t.Logf("  Summary: %d ways have speed (%.0f m), %d lack speed tag (%.0f m)",
			hasSpeedCount, hasSpeedLength, noSpeedCount, noSpeedLength)
		wouldPass := passingFrac >= 0.5
		t.Logf("  Would pass --min-speed 35 filter: %v", wouldPass)
	}

	// Step 4: Show detail if available
	if detail, ok := result.GroupDetail["PA 339"]; ok {
		t.Logf("\n--- PA 339 Pipeline Detail ---")
		t.Logf("Input ways: %d", len(detail.InputWays))
		t.Logf("Connected components: %d", len(detail.Components))
		for i, comp := range detail.PerComponent {
			t.Logf("  Component %d: %d input ways, %d ordered ways", i, len(comp.InputWays), len(comp.OrderedWays))
			t.Logf("    Score before deflection: %.1f, after: %.1f (zeroed: %d segs)",
				comp.ScoreBeforeDeflection, comp.ScoreAfterDeflection, comp.SegmentsZeroedByDeflection)
			t.Logf("    Segment groups after straight-gap split: %d", len(comp.SegmentGroups))
			for j, grp := range comp.SegmentGroups {
				grpLen := 0.0
				grpScore := 0.0
				for _, seg := range grp {
					grpLen += seg.Length
					grpScore += seg.Score
				}
				t.Logf("      Group %d: %d segs, %.0f m, score=%.1f", j, len(grp), grpLen, grpScore)
			}
		}
		t.Logf("Final collections: %d", len(detail.Collections))
		for _, c := range detail.Collections {
			t.Log("  " + FormatCollectionSummary(c, 0))
		}
	}

	// Step 5: Also check what the trace shows
	t.Logf("\n--- TraceRoad PA 339 ---")
	trace, err := TraceRoad("PA 339", tiles, cfg)
	if err != nil {
		t.Fatalf("TraceRoad: %v", err)
	}
	for _, step := range trace.Steps {
		t.Logf("[%s] %s", step.Stage, step.Description)
		if step.Details != "" {
			// Print first 20 lines of details
			lines := splitLines(step.Details)
			for i, line := range lines {
				if i >= 30 {
					t.Logf("  ... (%d more lines)", len(lines)-30)
					break
				}
				t.Logf("  %s", line)
			}
		}
	}
}

func splitLines(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
