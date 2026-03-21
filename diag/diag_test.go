package diag_test

import (
	"fmt"
	"testing"

	"github.com/yardbirdsax/twisty/diag"
	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// TestTracePA901 traces PA 901 through the twisty scoring pipeline for the
// section between approximately 40.7005,-76.3097 and 40.6902,-76.2568.
//
// This is a diagnostic test: it never fails on missing data. All findings are
// reported via t.Logf so they appear in -v output and in `go test -run`.
func TestTracePA901(t *testing.T) {
	cfg := diag.DefaultCacheConfig()

	// Bounding box for the missing section, with a 0.01° buffer.
	sectionCoords := []geo.Coord{
		{Lat: 40.7005, Lon: -76.3097}, // start
		{Lat: 40.6902, Lon: -76.2568}, // end
	}
	sectionBBox := diag.BBoxForCoords(sectionCoords, 0.01)
	t.Logf("Section bounding box: S=%.4f W=%.4f N=%.4f E=%.4f",
		sectionBBox.South, sectionBBox.West, sectionBBox.North, sectionBBox.East)

	// Tiles covering the section bounding box (0.05° tile size matching the
	// default used by twisty score).
	const tileSizeDeg = 0.05
	sectionTiles := diag.TilesForBBox(sectionBBox, tileSizeDeg)
	t.Logf("Section tiles (%d):", len(sectionTiles))
	for _, tile := range sectionTiles {
		t.Logf("  %.3f,%.3f → %.3f,%.3f", tile.South, tile.West, tile.North, tile.East)
	}

	// Full tile grid for a typical 25 km search centred on the midpoint of the
	// section, to check overall coverage.
	midLat := (sectionCoords[0].Lat + sectionCoords[1].Lat) / 2
	midLon := (sectionCoords[0].Lon + sectionCoords[1].Lon) / 2
	coverage := diag.TileCoverage(midLat, midLon, 25.0, tileSizeDeg, cfg)
	t.Logf("Tile coverage for 25 km search: %d present, %d missing (of %d total)",
		len(coverage.Present), len(coverage.Missing), len(coverage.Tiles))
	if len(coverage.Missing) > 0 {
		t.Logf("Missing tiles:")
		for _, tile := range coverage.Missing {
			t.Logf("  %.3f,%.3f", tile.South, tile.West)
		}
	}

	// Use section tiles for the cache searches.
	tiles := sectionTiles

	// ------------------------------------------------------------------
	// FindWaysInOverpassCache: locate PA 901 ways in raw cache data.
	// ------------------------------------------------------------------
	t.Logf("")
	t.Logf("=== Stage: overpass_cache ===")
	wayMatches, err := diag.FindWaysInOverpassCache(tiles, cfg, diag.WayMatchesRef("PA 901"))
	if err != nil {
		t.Logf("  ERROR: %v", err)
	} else {
		t.Logf("  Ways found: %d", len(wayMatches))
		for _, m := range wayMatches {
			bb := diag.WayGeoBounds(m.Way)
			t.Logf("  Way %d %q (ref=%s) highway=%s nodes=%d",
				m.Way.ID, m.Way.Tags["name"], m.Way.Tags["ref"],
				m.Way.Tags["highway"], len(m.Way.Geometry))
			t.Logf("    Geo bounds: lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
				bb.South, bb.North, bb.West, bb.East)
			t.Logf("    Tile: %.3f,%.3f", m.Tile.South, m.Tile.West)
		}
	}

	// ------------------------------------------------------------------
	// FindWaysInScoreCache: locate PA 901 scored ways.
	// ------------------------------------------------------------------
	t.Logf("")
	t.Logf("=== Stage: score_cache ===")
	scoredMatches, err := diag.FindWaysInScoreCache(tiles, cfg, diag.ScoredWayMatchesRef("PA 901"))
	if err != nil {
		t.Logf("  ERROR: %v", err)
	} else {
		t.Logf("  Scored ways found: %d", len(scoredMatches))
		for _, m := range scoredMatches {
			bb := diag.ScoredWayGeoBounds(m.Way)
			totalScore := 0.0
			tierCounts := make(map[int]int)
			for _, seg := range m.Way.Segments {
				totalScore += seg.Score
				tierCounts[seg.Tier]++
			}
			t.Logf("  Way %d %q (ref=%s) highway=%s segs=%d score=%.1f",
				m.Way.WayID, m.Way.Tags["name"], m.Way.Tags["ref"],
				m.Way.Tags["highway"], len(m.Way.Segments), totalScore)
			t.Logf("    Tiers: t0=%d t1=%d t2=%d t3=%d t4=%d",
				tierCounts[0], tierCounts[1], tierCounts[2], tierCounts[3], tierCounts[4])
			t.Logf("    Geo bounds: lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
				bb.South, bb.North, bb.West, bb.East)
			t.Logf("    Tile: %.3f,%.3f", m.Tile.South, m.Tile.West)
		}
	}

	// ------------------------------------------------------------------
	// TraceRoad: full pipeline trace.
	// ------------------------------------------------------------------
	t.Logf("")
	t.Logf("=== TraceRoad: PA 901 ===")
	trace, err := diag.TraceRoad("PA 901", tiles, cfg)
	if err != nil {
		t.Logf("  ERROR: %v", err)
		return
	}

	for _, step := range trace.Steps {
		t.Logf("")
		t.Logf("=== Stage: %s ===", step.Stage)
		t.Logf("  %s", step.Description)
		if step.WayCount > 0 || step.SegCount > 0 {
			t.Logf("  Ways: %d  Segments: %d  Score: %.1f  Length: %.0f m",
				step.WayCount, step.SegCount, step.TotalScore, step.TotalLength)
		}
		if step.Details != "" {
			for _, line := range splitLines(step.Details) {
				t.Logf("  %s", line)
			}
		}
	}

	// Summary
	t.Logf("")
	t.Logf("=== Summary ===")
	t.Logf("Road: %s", trace.Name)
	t.Logf("Pipeline steps traced: %d", len(trace.Steps))
	if len(trace.Steps) > 0 {
		last := trace.Steps[len(trace.Steps)-1]
		t.Logf("Final output_filter: %s — %d collection(s) pass", last.Stage, last.WayCount)
	}
}

// splitLines splits a multi-line string into individual lines, excluding
// trailing empty lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	// Trim trailing empty lines.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// TestDiagHelpers exercises the pure helper functions to verify they compile
// and return sensible results without any cache dependency.
func TestDiagHelpers(t *testing.T) {
	coords := []geo.Coord{
		{Lat: 40.6902, Lon: -76.3097},
		{Lat: 40.7005, Lon: -76.2568},
	}

	bb := diag.BBoxForCoords(coords, 0.01)
	if bb.South >= bb.North {
		t.Errorf("BBoxForCoords: south %.4f >= north %.4f", bb.South, bb.North)
	}
	if bb.West >= bb.East {
		t.Errorf("BBoxForCoords: west %.4f >= east %.4f", bb.West, bb.East)
	}
	t.Logf("BBox: S=%.4f W=%.4f N=%.4f E=%.4f", bb.South, bb.West, bb.North, bb.East)

	tiles := diag.TilesForBBox(bb, 0.05)
	if len(tiles) == 0 {
		t.Error("TilesForBBox: expected at least one tile")
	}
	t.Logf("TilesForBBox: %d tile(s)", len(tiles))

	// CoordInBBox
	mid := geo.Coord{Lat: (coords[0].Lat + coords[1].Lat) / 2, Lon: (coords[0].Lon + coords[1].Lon) / 2}
	if !diag.CoordInBBox(mid, bb) {
		t.Errorf("CoordInBBox: midpoint should be inside bbox")
	}
	outside := geo.Coord{Lat: 0, Lon: 0}
	if diag.CoordInBBox(outside, bb) {
		t.Errorf("CoordInBBox: origin (0,0) should be outside bbox")
	}

	// DefaultCacheConfig
	cfg := diag.DefaultCacheConfig()
	if cfg.OverpassDir == "" {
		t.Error("DefaultCacheConfig: OverpassDir is empty")
	}
	if cfg.ScoreDir == "" {
		t.Error("DefaultCacheConfig: ScoreDir is empty")
	}
	if cfg.Precision == 0 {
		t.Error("DefaultCacheConfig: Precision is 0")
	}
	t.Logf("DefaultCacheConfig: overpass=%s score=%s precision=%d",
		cfg.OverpassDir, cfg.ScoreDir, cfg.Precision)

	// WayMatchesRef predicate
	predRef := diag.WayMatchesRef("PA 901")
	_ = fmt.Sprintf("%T", predRef) // exercise
}

// TestFindAllPA901Ways searches the entire Overpass cache for all ways tagged
// as PA 901, deduplicates them, orders them geographically, and checks whether
// they form a contiguous path. This is a diagnostic test — it never fails on
// missing data.
func TestFindAllPA901Ways(t *testing.T) {
	cfg := diag.DefaultCacheConfig()

	// List all cached tiles.
	allTiles, err := diag.AllCachedTiles(cfg)
	if err != nil {
		t.Fatalf("AllCachedTiles: %v", err)
	}
	t.Logf("Total cached tiles: %d", len(allTiles))

	// Find all PA 901 ways across the entire cache.
	matches, err := diag.FindWaysInOverpassCache(allTiles, cfg, diag.WayMatchesRef("PA 901"))
	if err != nil {
		t.Fatalf("FindWaysInOverpassCache: %v", err)
	}
	t.Logf("Raw matches (before dedup): %d", len(matches))

	// Deduplicate.
	deduped := diag.DeduplicateWayMatches(matches)
	t.Logf("Unique ways: %d", len(deduped))

	// Extract just the ways and order geographically.
	ways := make([]quality.Way, len(deduped))
	for i, m := range deduped {
		ways[i] = m.Way
	}
	ways = diag.OrderWaysByGeography(ways)

	// Print each way with its details.
	t.Logf("")
	t.Logf("=== PA 901 Ways (ordered south to north) ===")
	for i, w := range ways {
		bb := diag.WayGeoBounds(w)
		wayLen := 0.0
		for j := 0; j < len(w.Geometry)-1; j++ {
			wayLen += geo.Haversine(w.Geometry[j], w.Geometry[j+1])
		}
		t.Logf("  [%d] Way %d  name=%q  highway=%s  nodes=%d  length=%.0f m",
			i, w.ID, w.Tags["name"], w.Tags["highway"], len(w.Geometry), wayLen)
		t.Logf("       lat=[%.4f, %.4f]  lon=[%.4f, %.4f]",
			bb.South, bb.North, bb.West, bb.East)
		if len(w.Geometry) >= 2 {
			start := w.Geometry[0]
			end := w.Geometry[len(w.Geometry)-1]
			t.Logf("       start=(%.5f, %.5f)  end=(%.5f, %.5f)",
				start.Lat, start.Lon, end.Lat, end.Lon)
		}
	}

	// Analyze continuity.
	t.Logf("")
	t.Logf("=== Continuity Analysis ===")
	cont := diag.AnalyzeWayContinuity(ways, quality.ConnectedEndpointProximityM)
	t.Logf("Total ways: %d", cont.TotalWays)
	t.Logf("Total nodes: %d", cont.TotalNodes)
	t.Logf("Total length: %.0f m (%.2f km)", cont.TotalLengthM, cont.TotalLengthM/1000)
	t.Logf("Overall bbox: lat=[%.4f, %.4f]  lon=[%.4f, %.4f]",
		cont.OverallBBox.South, cont.OverallBBox.North,
		cont.OverallBBox.West, cont.OverallBBox.East)
	t.Logf("Connected components: %d", cont.Components)
	for i, size := range cont.ComponentSizes {
		t.Logf("  Component %d: %d ways", i, size)
	}
	t.Logf("Is contiguous: %v", cont.IsContiguous)

	if len(cont.Gaps) > 0 {
		t.Logf("")
		t.Logf("=== Gaps Between Components ===")
		for i, gap := range cont.Gaps {
			t.Logf("  Gap %d: %.0f m", i, gap.DistanceM)
			t.Logf("    From Way %d at (%.5f, %.5f)", gap.FromWayID, gap.FromCoord.Lat, gap.FromCoord.Lon)
			t.Logf("    To   Way %d at (%.5f, %.5f)", gap.ToWayID, gap.ToCoord.Lat, gap.ToCoord.Lon)
		}
	}
}

// TestSimulateFullPipelinePA901 runs the full scoring pipeline across all
// cached tiles and traces both "PA 901" (ref group) and "Sunbury Road" (name
// group) through aggregation. This diagnoses why the PA 901 section is absent
// from the KML output despite passing all filters in isolation.
//
// The test prints:
//   - How many ways land in each group after tile-level dedup
//   - Connected component counts and sizes
//   - Per-component ordering, deflection, and gap-split details
//   - Final RoadCollection scores, penalties, and output-filter pass/fail
//   - Any collections that contain the specific way ID 371984174 (the missing
//     section)
func TestSimulateFullPipelinePA901(t *testing.T) {
	cfg := diag.DefaultCacheConfig()

	allTiles, err := diag.AllCachedTiles(cfg)
	if err != nil {
		t.Fatalf("AllCachedTiles: %v", err)
	}
	t.Logf("Total cached tiles: %d", len(allTiles))

	// The way IDs we want to track through the pipeline.
	// 371984174 is the "Sunbury Road" way in the missing section.
	const targetWayID int64 = 371984174

	// Run pipeline with detail for both grouping keys.
	detailKeys := []string{"PA 901", "Sunbury Road"}
	result, err := diag.SimulatePipelineFull(allTiles, cfg, diag.SimulatePipelineOptions{
		DetailKeys: detailKeys,
	})
	if err != nil {
		t.Fatalf("SimulatePipelineFull: %v", err)
	}

	t.Logf("Total name groups: %d", len(result.Grouped))
	t.Logf("Total collections: %d", len(result.Collections))

	// Report on each detail key.
	for _, key := range detailKeys {
		t.Logf("")
		t.Logf("========================================")
		t.Logf("=== Group: %q ===", key)
		t.Logf("========================================")

		detail, ok := result.GroupDetail[key]
		if !ok {
			t.Logf("  NOT FOUND in pipeline output — no ways with this name/ref")
			continue
		}

		// Input ways.
		t.Logf("")
		t.Logf("--- Input Ways (%d) ---", len(detail.InputWays))
		for i, w := range detail.InputWays {
			bb := diag.ScoredWayGeoBounds(w)
			totalScore := 0.0
			for _, seg := range w.Segments {
				totalScore += seg.Score
			}
			marker := ""
			if w.WayID == targetWayID {
				marker = "  <<<< TARGET"
			}
			t.Logf("  [%d] Way %d name=%q highway=%s segs=%d score=%.1f lat=[%.4f,%.4f] lon=[%.4f,%.4f]%s",
				i, w.WayID, w.Tags["name"], w.Tags["highway"],
				len(w.Segments), totalScore,
				bb.South, bb.North, bb.West, bb.East,
				marker)
		}

		// Connected components.
		t.Logf("")
		t.Logf("--- Connected Components (%d) ---", len(detail.Components))
		for ci, comp := range detail.Components {
			compLen := 0.0
			compScore := 0.0
			hasTarget := false
			for _, w := range comp {
				if w.WayID == targetWayID {
					hasTarget = true
				}
				for _, seg := range w.Segments {
					compLen += seg.Length
					compScore += seg.Score
				}
			}
			marker := ""
			if hasTarget {
				marker = "  <<<< CONTAINS TARGET"
			}
			t.Logf("  Component %d: %d ways, %.0f m, score=%.1f%s",
				ci, len(comp), compLen, compScore, marker)
		}

		// Per-component details.
		t.Logf("")
		t.Logf("--- Per-Component Processing ---")
		for ci, cd := range detail.PerComponent {
			hasTarget := false
			for _, w := range cd.InputWays {
				if w.WayID == targetWayID {
					hasTarget = true
					break
				}
			}
			if !hasTarget {
				t.Logf("  Component %d: %d input ways (target not here, skipping detail)", ci, len(cd.InputWays))
				continue
			}

			t.Logf("  Component %d: <<<< CONTAINS TARGET", ci)
			t.Logf("    Input ways: %d", len(cd.InputWays))
			t.Logf("    After ordering + gap split: %d ways (dropped %d)",
				len(cd.OrderedWays), cd.DroppedByOrdering)

			// Check if target survived ordering.
			targetInOrdered := false
			for _, w := range cd.OrderedWays {
				if w.WayID == targetWayID {
					targetInOrdered = true
					break
				}
			}
			if !targetInOrdered {
				t.Logf("    *** TARGET WAY %d DROPPED BY ORDERING/GAP SPLIT ***", targetWayID)
			} else {
				t.Logf("    Target way %d survived ordering", targetWayID)
			}

			t.Logf("    Deflection: score before=%.1f after=%.1f (zeroed segs=%d)",
				cd.ScoreBeforeDeflection, cd.ScoreAfterDeflection, cd.SegmentsZeroedByDeflection)

			t.Logf("    Straight-gap split: %d segment group(s)", len(cd.SegmentGroups))
			for gi, grp := range cd.SegmentGroups {
				grpLen := 0.0
				grpScore := 0.0
				hasTargetSeg := false
				for _, seg := range grp {
					grpLen += seg.Length
					grpScore += seg.Score
					if seg.WayID == targetWayID {
						hasTargetSeg = true
					}
				}
				marker := ""
				if hasTargetSeg {
					marker = "  <<<< TARGET SEGS"
				}
				t.Logf("      Group %d: %d segs, %.0f m, score=%.1f%s",
					gi, len(grp), grpLen, grpScore, marker)
			}
		}

		// Final collections.
		t.Logf("")
		t.Logf("--- Collections (%d) ---", len(detail.Collections))
		for _, c := range detail.Collections {
			hasTarget := false
			for _, id := range c.WayIDs {
				if id == targetWayID {
					hasTarget = true
					break
				}
			}
			marker := ""
			if hasTarget {
				marker = "  <<<< TARGET"
			}
			t.Logf("  %s%s", diag.FormatCollectionSummary(c, 0), marker)
		}
	}

	// Search ALL collections for any that contain the target way.
	t.Logf("")
	t.Logf("========================================")
	t.Logf("=== All collections containing Way %d ===", targetWayID)
	t.Logf("========================================")
	found := diag.FindCollectionsContainingWay(result.Collections, targetWayID)
	if len(found) == 0 {
		t.Logf("  NONE — target way not present in any output collection")
	}
	for _, c := range found {
		passes := diag.CollectionPassesOutputFilter(c, 0)
		t.Logf("  %s", diag.FormatCollectionSummary(c, 0))
		if !passes {
			t.Logf("    ^^^ FILTERED OUT of KML output")
		}
	}
}

// TestComparePA901ToMariettaKML runs the full pipeline simulation using the
// same tile set that would be used for a Marietta-area search, then compares
// the output against the actual marietta.kml to identify which PA 901
// collections are present or missing and why.
//
// The test:
//  1. Parses way IDs from the PA 901 entries in marietta.kml
//  2. Determines which tiles the marietta.kml run used (by examining the
//     first road's coordinates to infer center, then computing tiles)
//  3. Runs SimulatePipelineFull with detail for "PA 901" and "Sunbury Road"
//  4. Compares simulation collections against the KML way IDs
//  5. For missing collections, checks whether the constituent ways exist in
//     the tile cache
func TestComparePA901ToMariettaKML(t *testing.T) {
	cfg := diag.DefaultCacheConfig()
	const kmlPath = "/Users/joshuafeierman/repos/yardbirdsax/twisty/marietta.kml"

	// -----------------------------------------------------------------------
	// Step 1: Parse PA 901 way IDs from the KML.
	// -----------------------------------------------------------------------
	kmlWayIDs := parseKMLWayIDs(t, kmlPath, "PA 901")
	t.Logf("PA 901 way IDs in marietta.kml: %v", kmlWayIDs)

	allKMLWayIDs := parseKMLWayIDs(t, kmlPath, "Sunbury Road")
	t.Logf("Sunbury Road way IDs in marietta.kml: %v", allKMLWayIDs)

	// -----------------------------------------------------------------------
	// Step 2: Determine the tile set.
	// The first road in the KML is near lon=-76.19, lat=40.58, suggesting a
	// center around the Pottsville/Schuylkill County area. Use the KML's
	// first coordinate to infer a reasonable center+radius.
	// -----------------------------------------------------------------------
	// Use the center of the PA 901 area as the search center.
	// From the KML, PA 901 ways span roughly lat 40.58-40.79, lon -76.52 to -76.19.
	// A search centered on ~40.69, -76.30 with radius 40 km should cover everything.
	centerLat, centerLon := 40.69, -76.30
	radiusKm := 40.0
	const tileSizeDeg = 0.05

	coverage := diag.TileCoverage(centerLat, centerLon, radiusKm, tileSizeDeg, cfg)
	t.Logf("Tile coverage for %.0f km search at (%.2f, %.2f): %d present, %d missing (of %d total)",
		radiusKm, centerLat, centerLon,
		len(coverage.Present), len(coverage.Missing), len(coverage.Tiles))

	// -----------------------------------------------------------------------
	// Step 3: Run the full pipeline on present tiles with detail.
	// -----------------------------------------------------------------------
	detailKeys := []string{"PA 901", "Sunbury Road"}
	result, err := diag.SimulatePipelineFull(coverage.Present, cfg, diag.SimulatePipelineOptions{
		DetailKeys: detailKeys,
	})
	if err != nil {
		t.Fatalf("SimulatePipelineFull: %v", err)
	}
	t.Logf("Simulation: %d groups, %d collections", len(result.Grouped), len(result.Collections))

	// -----------------------------------------------------------------------
	// Step 4: Compare simulation against KML for each detail key.
	// -----------------------------------------------------------------------
	const targetWayID int64 = 371984174

	for _, key := range detailKeys {
		t.Logf("")
		t.Logf("========================================")
		t.Logf("=== Comparing %q: simulation vs KML ===", key)
		t.Logf("========================================")

		simCollections := diag.FindCollections(result.Collections, key)
		t.Logf("Simulation produced %d collections for %q", len(simCollections), key)

		var kmlIDs []int64
		if key == "PA 901" {
			kmlIDs = kmlWayIDs
		} else {
			kmlIDs = allKMLWayIDs
		}

		comparisons := diag.CompareCollectionsToKMLWays(simCollections, kmlIDs, 0)

		for _, comp := range comparisons {
			c := comp.Collection
			status := "IN KML"
			if !comp.InKML {
				status = "NOT IN KML"
			}
			filterStatus := ""
			if !comp.PassesFilter {
				filterStatus = " [FILTERED OUT]"
			}

			hasTarget := false
			for _, id := range c.WayIDs {
				if id == targetWayID {
					hasTarget = true
					break
				}
			}
			marker := ""
			if hasTarget {
				marker = " <<<< TARGET WAY"
			}

			t.Logf("  %s: %s%s%s", status, diag.FormatCollectionSummary(c, 0), filterStatus, marker)

			if !comp.InKML && len(comp.WaysMissing) > 0 {
				t.Logf("    Missing way IDs: %v", comp.WaysMissing)
			}
		}

		// Show detail for target-containing component if available.
		if detail, ok := result.GroupDetail[key]; ok {
			for ci, cd := range detail.PerComponent {
				hasTarget := false
				for _, w := range cd.InputWays {
					if w.WayID == targetWayID {
						hasTarget = true
						break
					}
				}
				if !hasTarget {
					continue
				}

				t.Logf("")
				t.Logf("  --- Component %d (contains target) detail ---", ci)
				t.Logf("    Input ways: %d", len(cd.InputWays))
				t.Logf("    After ordering + gap split: %d (dropped %d)", len(cd.OrderedWays), cd.DroppedByOrdering)

				targetInOrdered := false
				for _, w := range cd.OrderedWays {
					if w.WayID == targetWayID {
						targetInOrdered = true
						break
					}
				}
				if !targetInOrdered {
					t.Logf("    *** TARGET WAY %d WAS DROPPED BY ORDERING/GAP SPLIT ***", targetWayID)

					// Show all ways in the component to understand the ordering.
					t.Logf("    Input ways (before ordering):")
					for _, w := range cd.InputWays {
						bb := diag.ScoredWayGeoBounds(w)
						m := ""
						if w.WayID == targetWayID {
							m = " <<<< TARGET"
						}
						t.Logf("      Way %d name=%q segs=%d lat=[%.4f,%.4f] lon=[%.4f,%.4f]%s",
							w.WayID, w.Tags["name"], len(w.Segments),
							bb.South, bb.North, bb.West, bb.East, m)
					}
					t.Logf("    Ordered ways (after SplitOrderingGaps kept longest chunk):")
					for _, w := range cd.OrderedWays {
						bb := diag.ScoredWayGeoBounds(w)
						t.Logf("      Way %d name=%q segs=%d lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
							w.WayID, w.Tags["name"], len(w.Segments),
							bb.South, bb.North, bb.West, bb.East)
					}
				} else {
					t.Logf("    Target survived ordering")
				}

				t.Logf("    Deflection: score %.1f → %.1f", cd.ScoreBeforeDeflection, cd.ScoreAfterDeflection)
				t.Logf("    Straight-gap split: %d group(s)", len(cd.SegmentGroups))
				for gi, grp := range cd.SegmentGroups {
					grpScore := 0.0
					grpLen := 0.0
					hasTargetSeg := false
					for _, seg := range grp {
						grpScore += seg.Score
						grpLen += seg.Length
						if seg.WayID == targetWayID {
							hasTargetSeg = true
						}
					}
					m := ""
					if hasTargetSeg {
						m = " <<<< TARGET"
					}
					t.Logf("      Group %d: %d segs, %.0f m, score=%.1f%s", gi, len(grp), grpLen, grpScore, m)
				}
			}
		}
	}

	// -----------------------------------------------------------------------
	// Step 5: Check if target way exists in the tile cache for these tiles.
	// -----------------------------------------------------------------------
	t.Logf("")
	t.Logf("========================================")
	t.Logf("=== Target way %d tile lookup ===", targetWayID)
	t.Logf("========================================")

	locations, err := diag.LocateWaysInCache([]int64{targetWayID}, coverage.Present, cfg)
	if err != nil {
		t.Logf("ERROR: %v", err)
	} else {
		for _, loc := range locations {
			if loc.Found {
				t.Logf("  Way %d found in tile %.3f,%.3f", loc.WayID, loc.Tile.South, loc.Tile.West)
			} else {
				t.Logf("  Way %d NOT FOUND in any of the %d present tiles", loc.WayID, len(coverage.Present))
			}
		}
	}

	// Also check which tile SHOULD contain this way.
	targetBBox := diag.BBoxForCoords([]geo.Coord{
		{Lat: 40.6953, Lon: -76.3098},
		{Lat: 40.7006, Lon: -76.2824},
	}, 0)
	expectedTiles := diag.TilesForBBox(targetBBox, tileSizeDeg)
	t.Logf("  Expected tiles for target way bbox:")
	for _, tile := range expectedTiles {
		has := false
		for _, p := range coverage.Present {
			if p.South == tile.South && p.West == tile.West {
				has = true
				break
			}
		}
		status := "MISSING"
		if has {
			status = "PRESENT"
		}
		t.Logf("    tile %.3f,%.3f — %s", tile.South, tile.West, status)
	}
}

// TestPA901TileCoverageForMinersville checks how many of the 58 PA 901 ways
// are present in the Minersville 15km tile set versus the full cache. This
// identifies whether gaps in the KML output are caused by missing tile data.
func TestPA901TileCoverageForMinersville(t *testing.T) {
	cfg := diag.DefaultCacheConfig()

	// Minersville center (from geocode output).
	centerLat, centerLon := 40.6906, -76.2622
	radiusKm := 15.0
	const tileSizeDeg = 0.05

	coverage := diag.TileCoverage(centerLat, centerLon, radiusKm, tileSizeDeg, cfg)
	t.Logf("Minersville 15km tiles: %d present, %d missing (of %d)",
		len(coverage.Present), len(coverage.Missing), len(coverage.Tiles))

	// Find PA 901 ways in the Minersville tile set only.
	localMatches, err := diag.FindWaysInOverpassCache(coverage.Present, cfg, diag.WayMatchesRef("PA 901"))
	if err != nil {
		t.Fatalf("FindWaysInOverpassCache (local): %v", err)
	}
	localDeduped := diag.DeduplicateWayMatches(localMatches)

	// Find PA 901 ways across ALL cached tiles.
	allTiles, err := diag.AllCachedTiles(cfg)
	if err != nil {
		t.Fatalf("AllCachedTiles: %v", err)
	}
	allMatches, err := diag.FindWaysInOverpassCache(allTiles, cfg, diag.WayMatchesRef("PA 901"))
	if err != nil {
		t.Fatalf("FindWaysInOverpassCache (all): %v", err)
	}
	allDeduped := diag.DeduplicateWayMatches(allMatches)

	t.Logf("PA 901 ways in Minersville 15km tiles: %d of %d total", len(localDeduped), len(allDeduped))

	// Find which ways are missing from the local set.
	localIDs := make(map[int64]bool)
	for _, m := range localDeduped {
		localIDs[m.Way.ID] = true
	}

	t.Logf("")
	t.Logf("=== PA 901 Ways Missing From Minersville 15km Tiles ===")
	missingCount := 0
	for _, m := range allDeduped {
		if !localIDs[m.Way.ID] {
			missingCount++
			bb := diag.WayGeoBounds(m.Way)
			t.Logf("  Way %d name=%q highway=%s lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
				m.Way.ID, m.Way.Tags["name"], m.Way.Tags["highway"],
				bb.South, bb.North, bb.West, bb.East)
		}
	}
	if missingCount == 0 {
		t.Logf("  (none — all PA 901 ways are covered)")
	} else {
		t.Logf("Total missing: %d ways", missingCount)

		// Check continuity of just the local ways.
		localWays := make([]quality.Way, len(localDeduped))
		for i, m := range localDeduped {
			localWays[i] = m.Way
		}
		cont := diag.AnalyzeWayContinuity(localWays, quality.ConnectedEndpointProximityM)
		t.Logf("")
		t.Logf("Continuity of local PA 901 ways: %d components, contiguous=%v",
			cont.Components, cont.IsContiguous)
		if !cont.IsContiguous {
			for i, gap := range cont.Gaps {
				t.Logf("  Gap %d: %.0f m (%.2f mi) between Way %d and Way %d",
					i, gap.DistanceM, gap.DistanceM/1609.34,
					gap.FromWayID, gap.ToWayID)
			}
		}
	}
}

// TestTracePA901OrderingAndSplitting runs the full pipeline simulation on the
// Minersville 15km tile set with detailed tracing for the "PA 901" group. It
// logs every step of the ordering and splitting process to diagnose where the
// 4.5-mile gap between PA 901 (2) and PA 901 (3) emerges.
func TestTracePA901OrderingAndSplitting(t *testing.T) {
	cfg := diag.DefaultCacheConfig()

	// Minersville 15km tile set.
	centerLat, centerLon := 40.6906, -76.2622
	radiusKm := 15.0
	const tileSizeDeg = 0.05

	coverage := diag.TileCoverage(centerLat, centerLon, radiusKm, tileSizeDeg, cfg)

	result, err := diag.SimulatePipelineFull(coverage.Present, cfg, diag.SimulatePipelineOptions{
		DetailKeys: []string{"PA 901"},
	})
	if err != nil {
		t.Fatalf("SimulatePipelineFull: %v", err)
	}

	detail, ok := result.GroupDetail["PA 901"]
	if !ok {
		t.Fatal("PA 901 group not found in simulation")
	}

	t.Logf("PA 901 group: %d input ways, %d connected components",
		len(detail.InputWays), len(detail.Components))

	for ci, cd := range detail.PerComponent {
		t.Logf("")
		t.Logf("========================================")
		t.Logf("=== Component %d: %d input ways ===", ci, len(cd.InputWays))
		t.Logf("========================================")

		// Log input ways geographically.
		t.Logf("")
		t.Logf("--- Input Ways ---")
		for _, w := range cd.InputWays {
			bb := diag.ScoredWayGeoBounds(w)
			score := 0.0
			for _, seg := range w.Segments {
				score += seg.Score
			}
			t.Logf("  Way %d name=%q highway=%s segs=%d score=%.1f lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
				w.WayID, w.Tags["name"], w.Tags["highway"],
				len(w.Segments), score,
				bb.South, bb.North, bb.West, bb.East)
		}

		// Re-run OrderWays to see the chain order and then SplitOrderingGaps
		// to see the chunks.
		t.Logf("")
		t.Logf("--- OrderWays Result ---")
		compCopy := quality.DeepCopyWays(cd.InputWays)
		ordered := quality.OrderWays(compCopy)

		t.Logf("Ordered chain (%d ways):", len(ordered))
		for i, w := range ordered {
			bb := diag.ScoredWayGeoBounds(w)
			// Show gap from previous way.
			gapStr := ""
			if i > 0 {
				_, prevEnd, _ := quality.WayEndpointsPublic(ordered[i-1])
				curStart, _, _ := quality.WayEndpointsPublic(w)
				gap := geo.Haversine(prevEnd, curStart)
				gapStr = fmt.Sprintf(" gap_from_prev=%.0f m", gap)
				if gap > quality.ConnectedEndpointProximityM {
					gapStr += " *** GAP ***"
				}
			}
			t.Logf("  [%d] Way %d name=%q segs=%d lat=[%.4f,%.4f] lon=[%.4f,%.4f]%s",
				i, w.WayID, w.Tags["name"], len(w.Segments),
				bb.South, bb.North, bb.West, bb.East, gapStr)
		}

		// Show chunks from SplitOrderingGaps.
		chunks := quality.SplitOrderingGaps(ordered, quality.ConnectedEndpointProximityM)
		t.Logf("")
		t.Logf("--- SplitOrderingGaps: %d chunk(s) ---", len(chunks))
		for chi, chunk := range chunks {
			chunkScore := 0.0
			chunkLen := 0.0
			for _, w := range chunk {
				for _, seg := range w.Segments {
					chunkScore += seg.Score
					chunkLen += seg.Length
				}
			}
			t.Logf("  Chunk %d: %d ways, %.0f m, score=%.1f", chi, len(chunk), chunkLen, chunkScore)
			for _, w := range chunk {
				bb := diag.ScoredWayGeoBounds(w)
				t.Logf("    Way %d name=%q lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
					w.WayID, w.Tags["name"], bb.South, bb.North, bb.West, bb.East)
			}
		}

		// Show segment groups from SplitAtStraightGaps per chunk.
		t.Logf("")
		t.Logf("--- SplitAtStraightGaps ---")
		for chi, chunk := range chunks {
			// Apply deflection first (as the pipeline does).
			chunkCopy := quality.DeepCopyWays(chunk)
			flatSegs := quality.FlattenWaySegments(chunkCopy)
			quality.DeflectionFilterSegments(flatSegs)
			quality.UnflattenWaySegments(chunkCopy, flatSegs)

			segGroups := quality.SplitAtStraightGaps(chunkCopy, quality.StraightGapSplitM)
			t.Logf("  Chunk %d → %d segment group(s)", chi, len(segGroups))
			for gi, grp := range segGroups {
				grpScore := 0.0
				grpLen := 0.0
				for _, seg := range grp {
					grpScore += seg.Score
					grpLen += seg.Length
				}
				bb := diag.SegmentGroupBBox(grp)
				start, end := diag.SegmentGroupEndpoints(grp)
				wayIDs := diag.SegmentGroupWayIDs(grp)
				passLen := grpLen >= quality.MinRoadLengthM
				passScore := grpScore > 0
				t.Logf("    Group %d: %d segs, %.0f m, score=%.1f, passLen=%v, passScore=%v",
					gi, len(grp), grpLen, grpScore, passLen, passScore)
				t.Logf("      start=(%.5f, %.5f) end=(%.5f, %.5f)",
					start.Lat, start.Lon, end.Lat, end.Lon)
				t.Logf("      bbox: lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
					bb.South, bb.North, bb.West, bb.East)
				t.Logf("      ways: %v", wayIDs)
			}
		}
	}
}

// TestCheckMissingPA901WaysInTiles checks whether specific PA 901 ways that
// should connect the gap (West Market Street / Pottsville Minersville Highway
// through Minersville) are present in the Minersville 15km tile set and have
// the PA 901 ref tag.
func TestCheckMissingPA901WaysInTiles(t *testing.T) {
	cfg := diag.DefaultCacheConfig()

	// Ways that connect the gap — from the full-cache PA 901 analysis.
	connectingWayIDs := []int64{
		610336496, 610336497, // West Market Street
		371984170, 371984176, // Pottsville Minersville Highway
		15007340,             // West Market Street
		610336494, 610336495, // West Market Street / PMH
		236007106,            // Pottsville Minersville Highway
		371984167, 371984183, // Pottsville Minersville Highway
		15017733, 371984172,  // Pottsville Minersville Highway
		236007098,            // Pottsville Minersville Highway
	}

	// Minersville 15km tile set.
	coverage := diag.TileCoverage(40.6906, -76.2622, 15.0, 0.05, cfg)

	locations, err := diag.LocateWaysInCache(connectingWayIDs, coverage.Present, cfg)
	if err != nil {
		t.Fatalf("LocateWaysInCache: %v", err)
	}

	t.Logf("Checking %d connecting ways in %d tiles:", len(connectingWayIDs), len(coverage.Present))
	for _, loc := range locations {
		if loc.Found {
			t.Logf("  Way %d: FOUND in tile %.3f,%.3f", loc.WayID, loc.Tile.South, loc.Tile.West)
		} else {
			t.Logf("  Way %d: NOT FOUND", loc.WayID)
		}
	}

	// For found ways, check their ref tag.
	t.Logf("")
	t.Logf("Checking ref tags:")
	allMatches, err := diag.FindWaysInOverpassCache(coverage.Present, cfg, func(w quality.Way) bool {
		for _, id := range connectingWayIDs {
			if w.ID == id {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("FindWaysInOverpassCache: %v", err)
	}
	deduped := diag.DeduplicateWayMatches(allMatches)
	for _, m := range deduped {
		t.Logf("  Way %d name=%q ref=%q highway=%s",
			m.Way.ID, m.Way.Tags["name"], m.Way.Tags["ref"], m.Way.Tags["highway"])
	}
}

// parseKMLWayIDs extracts way IDs from all KML folders whose name starts with
// the given prefix, using the reusable ParseKMLRoadEntries function.
func parseKMLWayIDs(t *testing.T, kmlPath, namePrefix string) []int64 {
	t.Helper()
	entries, err := diag.ParseKMLRoadEntries(kmlPath, namePrefix)
	if err != nil {
		t.Fatalf("ParseKMLRoadEntries: %v", err)
	}
	seen := make(map[int64]bool)
	var wayIDs []int64
	for _, e := range entries {
		for _, id := range e.WayIDs {
			if !seen[id] {
				seen[id] = true
				wayIDs = append(wayIDs, id)
			}
		}
	}
	return wayIDs
}

// TestAnalyzePA901SpacingMinersville parses the PA 901 entries from
// minersville.kml and checks the distance between each pair of collections.
// Also runs the full pipeline simulation to identify whether the target way
// (371984174) appears in the output and where gaps exist.
func TestAnalyzePA901SpacingMinersville(t *testing.T) {
	const kmlPath = "/Users/joshuafeierman/repos/yardbirdsax/twisty/minersville.kml"
	const targetWayID int64 = 371984174

	// -----------------------------------------------------------------------
	// Step 1: Parse PA 901 entries from the KML.
	// -----------------------------------------------------------------------
	entries, err := diag.ParseKMLRoadEntries(kmlPath, "PA 901")
	if err != nil {
		t.Fatalf("ParseKMLRoadEntries: %v", err)
	}
	t.Logf("PA 901 entries in minersville.kml: %d", len(entries))

	for i, e := range entries {
		t.Logf("")
		t.Logf("  [%d] %s", i, e.Name)
		t.Logf("      %s", e.Description)
		t.Logf("      Ways: %v", e.WayIDs)
		t.Logf("      Start: (%.5f, %.5f)  End: (%.5f, %.5f)",
			e.StartCoord.Lat, e.StartCoord.Lon, e.EndCoord.Lat, e.EndCoord.Lon)
		t.Logf("      BBox: lat=[%.4f, %.4f] lon=[%.4f, %.4f]  coords=%d",
			e.BBox.South, e.BBox.North, e.BBox.West, e.BBox.East, e.NumCoords)

		// Check if target way is in this entry.
		for _, id := range e.WayIDs {
			if id == targetWayID {
				t.Logf("      <<<< CONTAINS TARGET WAY %d", targetWayID)
			}
		}
	}

	// -----------------------------------------------------------------------
	// Step 2: Analyze spacing between collections.
	// -----------------------------------------------------------------------
	t.Logf("")
	t.Logf("=== Spacing Between PA 901 Collections ===")
	gaps := diag.AnalyzeCollectionSpacing(entries)
	for _, gap := range gaps {
		distMiles := gap.DistanceM / 1609.34
		status := "OK"
		if gap.DistanceM > 402 { // 1/4 mile = 402m
			status = "*** GAP > 1/4 MILE ***"
		}
		t.Logf("  %s ↔ %s: %.0f m (%.2f mi) %s",
			gap.FromName, gap.ToName, gap.DistanceM, distMiles, status)
		t.Logf("    from (%.5f, %.5f) to (%.5f, %.5f)",
			gap.FromCoord.Lat, gap.FromCoord.Lon, gap.ToCoord.Lat, gap.ToCoord.Lon)
	}

	// -----------------------------------------------------------------------
	// Step 3: Check if target way is in any entry.
	// -----------------------------------------------------------------------
	t.Logf("")
	targetFound := false
	for _, e := range entries {
		for _, id := range e.WayIDs {
			if id == targetWayID {
				targetFound = true
				t.Logf("Target way %d found in: %s", targetWayID, e.Name)
			}
		}
	}
	if !targetFound {
		t.Logf("Target way %d NOT FOUND in any PA 901 KML entry", targetWayID)

		// Run simulation to see where it ended up.
		t.Logf("")
		t.Logf("=== Running pipeline simulation to locate target ===")
		cfg := diag.DefaultCacheConfig()
		allTiles, err := diag.AllCachedTiles(cfg)
		if err != nil {
			t.Fatalf("AllCachedTiles: %v", err)
		}

		simResult, err := diag.SimulatePipelineFull(allTiles, cfg, diag.SimulatePipelineOptions{
			DetailKeys: []string{"PA 901", "Sunbury Road"},
		})
		if err != nil {
			t.Fatalf("SimulatePipelineFull: %v", err)
		}

		found := diag.FindCollectionsContainingWay(simResult.Collections, targetWayID)
		if len(found) == 0 {
			t.Logf("Target way %d not in ANY collection in simulation", targetWayID)
		} else {
			for _, c := range found {
				t.Logf("  %s", diag.FormatCollectionSummary(c, 0))
			}
		}

		// Show detail for PA 901 group.
		if detail, ok := simResult.GroupDetail["PA 901"]; ok {
			for ci, cd := range detail.PerComponent {
				for _, w := range cd.InputWays {
					if w.WayID == targetWayID {
						t.Logf("")
						t.Logf("  Target in PA 901 component %d:", ci)
						t.Logf("    Input ways: %d", len(cd.InputWays))
						t.Logf("    After ordering + gap split: %d (dropped %d)",
							len(cd.OrderedWays), cd.DroppedByOrdering)
						targetInOrdered := false
						for _, ow := range cd.OrderedWays {
							if ow.WayID == targetWayID {
								targetInOrdered = true
							}
						}
						if !targetInOrdered {
							t.Logf("    *** TARGET STILL DROPPED BY ORDERING/GAP SPLIT ***")
						} else {
							t.Logf("    Target survived ordering")
						}
						break
					}
				}
			}
		}
	}
}
