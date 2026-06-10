package diag

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/quality"
)

// TestDiagPA120 traces PA-120 through every stage of the scoring pipeline to
// identify why a road that is empirically twisty does not surface in output.
// PA-120 runs through the Susquehanna/Lycoming county mountains in north-central
// Pennsylvania (approx. Lock Haven area).  We load all cached tiles that cover
// it, then inspect each pipeline stage for ways dropped, scores zeroed, or
// collections filtered out.
func TestDiagPA120(t *testing.T) {
	cfg := DefaultCacheConfig()

	// PA-120 runs roughly between (41.1, -77.8) and (41.5, -78.5).
	// Use AllCachedTiles so we don't miss anything already in the cache, then
	// filter to tiles that could contain the road.
	allTiles, err := AllCachedTiles(cfg)
	if err != nil {
		t.Fatalf("AllCachedTiles: %v", err)
	}
	t.Logf("Total cached tiles: %d", len(allTiles))

	// Filter to the rough bounding box of PA-120 with a generous buffer.
	pa120BBox := BBox{
		South: 41.0,
		North: 41.7,
		West:  -78.8,
		East:  -77.5,
	}
	var tiles []quality.Tile
	for _, tile := range allTiles {
		tileBBox := BBox{South: tile.South, North: tile.North, West: tile.West, East: tile.East}
		if bboxesOverlap(tileBBox, pa120BBox) {
			tiles = append(tiles, tile)
		}
	}
	t.Logf("Tiles covering PA-120 bbox: %d", len(tiles))

	if len(tiles) == 0 {
		t.Log("WARNING: No cached tiles found for PA-120's bounding box.")
		t.Log("You may need to run the score command over this area first.")
		t.Log("Try: twisty score --lat 41.3 --lon -78.1 --radius 100")
		// Fall back to all tiles so we can still find whatever is cached.
		tiles = allTiles
		t.Logf("Falling back to all %d cached tiles", len(tiles))
	}

	// -------------------------------------------------------------------------
	// Stage 1: What PA 120 ways exist in the raw Overpass cache?
	// -------------------------------------------------------------------------
	t.Log("\n=== STAGE 1: Raw Overpass Cache ===")
	rawMatches, err := FindWaysInOverpassCache(tiles, cfg, func(w quality.Way) bool {
		ref := w.Tags["ref"]
		name := w.Tags["name"]
		return strings.Contains(ref, "120") || strings.Contains(name, "PA 120") ||
			strings.Contains(name, "Route 120")
	})
	if err != nil {
		t.Fatalf("FindWaysInOverpassCache: %v", err)
	}
	rawMatches = DeduplicateWayMatches(rawMatches)
	t.Logf("Unique ways with ref/name containing '120': %d", len(rawMatches))

	// Narrow to PA 120 specifically.
	var pa120Raw []WayMatch
	for _, m := range rawMatches {
		ref := m.Way.Tags["ref"]
		if ref == "PA 120" || ref == "SR 120" || strings.Contains(ref, "PA 120") {
			pa120Raw = append(pa120Raw, m)
		}
	}
	t.Logf("Ways with ref matching 'PA 120': %d", len(pa120Raw))

	for _, m := range pa120Raw {
		bb := WayGeoBounds(m.Way)
		t.Logf("  Way %d: name=%q ref=%q highway=%s surface=%q access=%q motor_vehicle=%q nodes=%d",
			m.Way.ID, m.Way.Tags["name"], m.Way.Tags["ref"],
			m.Way.Tags["highway"], m.Way.Tags["surface"],
			m.Way.Tags["access"], m.Way.Tags["motor_vehicle"],
			len(m.Way.Geometry))
		t.Logf("    Geo: lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
			bb.South, bb.North, bb.West, bb.East)
	}

	// -------------------------------------------------------------------------
	// Stage 2: HardFilter — which ways survive?
	// -------------------------------------------------------------------------
	t.Log("\n=== STAGE 2: HardFilter ===")
	rawWays := make([]quality.Way, len(pa120Raw))
	for i, m := range pa120Raw {
		rawWays[i] = m.Way
	}
	filtered := quality.HardFilter(rawWays)
	t.Logf("Ways before hard filter: %d, after: %d (dropped: %d)",
		len(rawWays), len(filtered), len(rawWays)-len(filtered))

	filteredSet := make(map[int64]bool)
	for _, w := range filtered {
		filteredSet[w.ID] = true
	}
	for _, w := range rawWays {
		if !filteredSet[w.ID] {
			t.Logf("  DROPPED Way %d: highway=%s surface=%q access=%q motor_vehicle=%q",
				w.ID, w.Tags["highway"], w.Tags["surface"], w.Tags["access"], w.Tags["motor_vehicle"])
		}
	}

	// -------------------------------------------------------------------------
	// Stage 3: Full pipeline trace using TraceRoad.
	// -------------------------------------------------------------------------
	t.Log("\n=== STAGE 3: Full Pipeline Trace (TraceRoad) ===")
	trace, err := TraceRoad("PA 120", tiles, cfg)
	if err != nil {
		t.Fatalf("TraceRoad: %v", err)
	}

	for _, step := range trace.Steps {
		t.Logf("[%s] %s", step.Stage, step.Description)
		if step.Details != "" {
			for i, line := range splitLines(step.Details) {
				if i >= 40 {
					t.Logf("  ... (%d more lines)", len(splitLines(step.Details))-40)
					break
				}
				t.Logf("  %s", line)
			}
		}
	}

	// -------------------------------------------------------------------------
	// Stage 4: Full SimulatePipelineFull with per-group detail.
	// -------------------------------------------------------------------------
	t.Log("\n=== STAGE 4: SimulatePipelineFull with detail ===")
	result, err := SimulatePipelineFull(tiles, cfg, SimulatePipelineOptions{
		DetailKeys: []string{"PA 120"},
	})
	if err != nil {
		t.Fatalf("SimulatePipelineFull: %v", err)
	}

	pa120Collections := FindCollections(result.Collections, "PA 120")
	t.Logf("PA 120 collections: %d", len(pa120Collections))
	for _, c := range pa120Collections {
		t.Log(FormatCollectionSummary(c, 0))
	}

	if detail, ok := result.GroupDetail["PA 120"]; ok {
		t.Logf("\nGroupDetail for 'PA 120':")
		t.Logf("  Input ways: %d", len(detail.InputWays))
		t.Logf("  Connected components: %d", len(detail.Components))
		for i, comp := range detail.PerComponent {
			totalInputScore := 0.0
			for _, w := range comp.InputWays {
				for _, seg := range w.Segments {
					totalInputScore += seg.Score
				}
			}
			t.Logf("  Component %d: %d input ways (raw score=%.1f), %d ordered ways",
				i, len(comp.InputWays), totalInputScore, len(comp.OrderedWays))
			t.Logf("    Score before deflection: %.1f", comp.ScoreBeforeDeflection)
			t.Logf("    Score after deflection:  %.1f (zeroed %d segs)",
				comp.ScoreAfterDeflection, comp.SegmentsZeroedByDeflection)
			t.Logf("    Segment groups after straight-gap split: %d", len(comp.SegmentGroups))
			for j, grp := range comp.SegmentGroups {
				grpLen, grpScore := 0.0, 0.0
				tierDist := make(map[int]int)
				for _, seg := range grp {
					grpLen += seg.Length
					grpScore += seg.Score
					tierDist[seg.Tier]++
				}
				t.Logf("    Group %d: %d segs, %.0f m, score=%.1f | tier dist: t0=%d t1=%d t2=%d t3=%d t4=%d",
					j, len(grp), grpLen, grpScore,
					tierDist[0], tierDist[1], tierDist[2], tierDist[3], tierDist[4])
			}
		}
		t.Logf("  Final collections: %d", len(detail.Collections))
		for _, c := range detail.Collections {
			t.Log("  " + FormatCollectionSummary(c, 0))
		}
	} else {
		t.Log("No GroupDetail for 'PA 120' — road may be grouped under a different key.")
		t.Log("Checking what keys are present in grouped output for ways overlapping PA-120 bbox...")
		// Find grouped keys for ways whose coords overlap the bbox.
		count := 0
		for key, ways := range result.Grouped {
			for _, w := range ways {
				bb := ScoredWayGeoBounds(w)
				if bboxesOverlap(bb, pa120BBox) {
					t.Logf("  Grouped key %q has %d ways (sample way %d in bbox)", key, len(ways), w.WayID)
					count++
					break
				}
			}
			if count > 20 {
				t.Log("  (stopped after 20 keys)")
				break
			}
		}
	}

	// -------------------------------------------------------------------------
	// Stage 5: Segment-level curvature analysis for PA 120 raw ways.
	// Show how individual ways score BEFORE deflection filtering, to determine
	// whether the geometry is actually getting scored at all.
	// -------------------------------------------------------------------------
	t.Log("\n=== STAGE 5: Per-Way Segment Curvature Analysis ===")
	for _, m := range pa120Raw {
		if !filteredSet[m.Way.ID] {
			continue // skip hard-filtered ways
		}
		w := m.Way
		// Score this single way in isolation.
		single := quality.RunScorePipeline([]quality.Way{w})
		if len(single.ScoredWays) == 0 {
			t.Logf("  Way %d: RunScorePipeline returned no scored ways", w.ID)
			continue
		}
		sw := single.ScoredWays[0]
		totalScore, totalLen := 0.0, 0.0
		tierDist := make(map[int]int)
		for _, seg := range sw.Segments {
			totalScore += seg.Score
			totalLen += seg.Length
			tierDist[seg.Tier]++
		}
		scorePerKm := 0.0
		if totalLen > 0 {
			scorePerKm = totalScore / (totalLen / 1000)
		}
		t.Logf("  Way %d: %d nodes → %d segs, len=%.0f m, score=%.1f (%.1f/km) | tier dist: t0=%d t1=%d t2=%d t3=%d t4=%d",
			w.ID, len(w.Geometry), len(sw.Segments), totalLen, totalScore, scorePerKm,
			tierDist[0], tierDist[1], tierDist[2], tierDist[3], tierDist[4])
	}

	// -------------------------------------------------------------------------
	// Stage 6: Deflection filter sensitivity analysis.
	// Run the assembled chain through the deflection filter and show which
	// segments get zeroed, and WHY (heading-change window value).
	// -------------------------------------------------------------------------
	t.Log("\n=== STAGE 6: Deflection Filter Analysis ===")
	if detail, ok := result.GroupDetail["PA 120"]; ok {
		for ci, comp := range detail.PerComponent {
			if len(comp.OrderedWays) == 0 {
				continue
			}
			flatSegs := quality.FlattenWaySegments(comp.OrderedWays)
			t.Logf("Component %d: %d flat segments before deflection filter", ci, len(flatSegs))

			// Count non-zero tier segments before filter.
			scoredBefore := 0
			for _, seg := range flatSegs {
				if seg.Tier > 0 {
					scoredBefore++
				}
			}
			quality.DeflectionFilterSegments(flatSegs)
			scoredAfter := 0
			for _, seg := range flatSegs {
				if seg.Tier > 0 {
					scoredAfter++
				}
			}
			t.Logf("  Tier>0 segments: %d before deflection, %d after (zeroed: %d)",
				scoredBefore, scoredAfter, scoredBefore-scoredAfter)

			// Show geography of zeroed segments — are they spread throughout the road
			// or concentrated in one area?
			zeroedBBox := BBox{South: 90, North: -90, West: 180, East: -180}
			survivingBBox := BBox{South: 90, North: -90, West: 180, East: -180}
			for _, seg := range flatSegs {
				if seg.Score == 0 {
					if seg.Start.Lat < zeroedBBox.South {
						zeroedBBox.South = seg.Start.Lat
					}
					if seg.Start.Lat > zeroedBBox.North {
						zeroedBBox.North = seg.Start.Lat
					}
					if seg.Start.Lon < zeroedBBox.West {
						zeroedBBox.West = seg.Start.Lon
					}
					if seg.Start.Lon > zeroedBBox.East {
						zeroedBBox.East = seg.Start.Lon
					}
				} else {
					if seg.Start.Lat < survivingBBox.South {
						survivingBBox.South = seg.Start.Lat
					}
					if seg.Start.Lat > survivingBBox.North {
						survivingBBox.North = seg.Start.Lat
					}
					if seg.Start.Lon < survivingBBox.West {
						survivingBBox.West = seg.Start.Lon
					}
					if seg.Start.Lon > survivingBBox.East {
						survivingBBox.East = seg.Start.Lon
					}
				}
			}
			if zeroedBBox.South < 90 {
				t.Logf("  Zeroed segments geo bbox:    lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
					zeroedBBox.South, zeroedBBox.North, zeroedBBox.West, zeroedBBox.East)
			}
			if survivingBBox.South < 90 {
				t.Logf("  Surviving segments geo bbox: lat=[%.4f,%.4f] lon=[%.4f,%.4f]",
					survivingBBox.South, survivingBBox.North, survivingBBox.West, survivingBBox.East)
			}
		}
	}

	// -------------------------------------------------------------------------
	// Stage 7: Connectivity analysis — are the raw ways contiguous?
	// A highly fragmented road can lose score to the straight-gap split.
	// -------------------------------------------------------------------------
	t.Log("\n=== STAGE 7: Way Continuity Analysis ===")
	continuity := AnalyzeWayContinuity(filtered, quality.ConnectedEndpointProximityM)
	t.Logf("Ways: %d, total nodes: %d, total length: %.0f m (%.1f km)",
		continuity.TotalWays, continuity.TotalNodes,
		continuity.TotalLengthM, continuity.TotalLengthM/1000)
	t.Logf("Connected components: %d (sizes: %v)",
		continuity.Components, continuity.ComponentSizes)
	t.Logf("Is contiguous: %v", continuity.IsContiguous)
	if len(continuity.Gaps) > 0 {
		t.Logf("Gaps between components:")
		for i, g := range continuity.Gaps {
			t.Logf("  Gap %d: way %d → way %d, distance=%.0f m",
				i, g.FromWayID, g.ToWayID, g.DistanceM)
		}
	}

	// -------------------------------------------------------------------------
	// Summary: synthesize what we found.
	// -------------------------------------------------------------------------
	t.Log("\n=== SUMMARY ===")
	t.Logf("Total raw PA 120 ways in cache: %d", len(pa120Raw))
	t.Logf("Ways surviving hard filter: %d", len(filtered))
	scoringStep := findStep(trace.Steps, "scoring")
	if scoringStep != nil {
		t.Logf("Total score at scoring stage: %.1f (%d ways, %d segs, %.0f m)",
			scoringStep.TotalScore, scoringStep.WayCount, scoringStep.SegCount, scoringStep.TotalLength)
	}
	deflectionStep := findStep(trace.Steps, "deflection_filter")
	if deflectionStep != nil {
		t.Logf("Score after deflection filter: %.1f", deflectionStep.TotalScore)
	}
	penaltyStep := findStep(trace.Steps, "penalties")
	if penaltyStep != nil {
		t.Logf("Collections after penalties: %d", penaltyStep.WayCount)
	}
	outputStep := findStep(trace.Steps, "output_filter")
	if outputStep != nil {
		t.Logf("Collections passing output filter: %d", outputStep.WayCount)
		t.Log(outputStep.Details)
	}

	// Print a final hypothesis based on what we found.
	t.Log("")
	t.Log("Hypothesis hints:")
	if len(pa120Raw) == 0 {
		t.Log("  *** Cache is empty for PA-120's area — road simply isn't in the cache yet. ***")
		t.Log("  Run: twisty score --lat 41.3 --lon -78.1 --radius 100")
	} else if len(filtered) == 0 {
		t.Log("  *** All ways were dropped by HardFilter (check highway type, surface, access). ***")
	} else if scoringStep != nil && scoringStep.TotalScore == 0 {
		t.Log("  *** Ways scored zero — geometry may not have enough curvature (all tier 0). ***")
		t.Log("  Check if circumradius calculation is treating long OSM ways as single segments.")
	} else if deflectionStep != nil && deflectionStep.TotalScore < scoringStep.TotalScore*0.2 {
		t.Log("  *** Deflection filter is zeroing >80% of score — road may have long straight")
		t.Log("  sections that dominate the 2400m look-ahead window, causing twisty parts")
		t.Log("  embedded within straights to be zeroed out. ***")
	} else if outputStep != nil && outputStep.WayCount == 0 && penaltyStep != nil && penaltyStep.WayCount > 0 {
		t.Log("  *** Collections exist but fail the output filter (min score or min length). ***")
		t.Log("  Check PenalizedScore and TotalLength in the penalties stage output.")
	} else {
		t.Log("  *** Road appears in output — check if the correct road name/ref is used. ***")
		t.Log("  Try searching with a broader ref match or the road's common name.")
		t.Logf("  PA 120 collections found: %d", len(pa120Collections))
		for _, c := range pa120Collections {
			t.Log("  " + FormatCollectionSummary(c, 0))
		}
	}

	// Emit helpful diagnostic strings for immediate copy-paste use.
	t.Logf("\nQuery hint: twisty score --lat 41.3 --lon -78.1 --radius 120")
	_ = fmt.Sprintf // suppress unused import
}

// bboxesOverlap returns true if two bounding boxes overlap.
func bboxesOverlap(a, b BBox) bool {
	return a.South <= b.North && a.North >= b.South &&
		a.West <= b.East && a.East >= b.West
}

// findStep returns the TraceStep with the given stage name, or nil.
func findStep(steps []TraceStep, stage string) *TraceStep {
	for i := range steps {
		if steps[i].Stage == stage {
			return &steps[i]
		}
	}
	return nil
}
