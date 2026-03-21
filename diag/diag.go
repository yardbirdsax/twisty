// Package diag provides reusable diagnostic functions for tracing roads
// through the twisty scoring pipeline. It is intended for use in diagnostic
// tests and ad-hoc investigations, not in production code.
package diag

import (
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// CacheConfig holds paths to the Overpass and score cache directories.
type CacheConfig struct {
	OverpassDir string
	ScoreDir    string
	Precision   int // tile coordinate decimal places (default 3)
}

// DefaultCacheConfig returns a CacheConfig pointing to ~/.twisty/cache/.
func DefaultCacheConfig() CacheConfig {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".twisty", "cache")
	return CacheConfig{
		OverpassDir: filepath.Join(base, "overpass"),
		ScoreDir:    filepath.Join(base, "scores"),
		Precision:   3,
	}
}

func (c CacheConfig) precision() int {
	if c.Precision == 0 {
		return 3
	}
	return c.Precision
}

func (c CacheConfig) tileCache() *quality.TileCache {
	return &quality.TileCache{Dir: c.OverpassDir, Precision: c.precision()}
}

func (c CacheConfig) scoreCache() *quality.ScoreCache {
	return &quality.ScoreCache{Dir: c.ScoreDir, Precision: c.precision()}
}

// BBox is a geographic bounding box.
type BBox struct {
	South, West, North, East float64
}

// TileCoverageResult holds the outcome of a tile-coverage check.
type TileCoverageResult struct {
	Tiles   []quality.Tile
	Present []quality.Tile
	Missing []quality.Tile
}

// TileCoverage checks which tiles from a computed tile grid exist in the
// Overpass cache. Returns lists of present and missing tiles.
func TileCoverage(centerLat, centerLon, radiusKm, tileSizeDeg float64, cfg CacheConfig) TileCoverageResult {
	tiles := quality.ComputeTiles(centerLat, centerLon, radiusKm, tileSizeDeg)
	tc := cfg.tileCache()
	result := TileCoverageResult{Tiles: tiles}
	for _, t := range tiles {
		if tc.Has(t) {
			result.Present = append(result.Present, t)
		} else {
			result.Missing = append(result.Missing, t)
		}
	}
	return result
}

// BBoxForCoords computes the bounding box for a set of lat/lon points with an
// optional buffer (in degrees) applied to each edge.
func BBoxForCoords(coords []geo.Coord, bufferDeg float64) BBox {
	if len(coords) == 0 {
		return BBox{}
	}
	bb := BBox{
		South: math.MaxFloat64,
		West:  math.MaxFloat64,
		North: -math.MaxFloat64,
		East:  -math.MaxFloat64,
	}
	for _, c := range coords {
		if c.Lat < bb.South {
			bb.South = c.Lat
		}
		if c.Lat > bb.North {
			bb.North = c.Lat
		}
		if c.Lon < bb.West {
			bb.West = c.Lon
		}
		if c.Lon > bb.East {
			bb.East = c.Lon
		}
	}
	bb.South -= bufferDeg
	bb.West -= bufferDeg
	bb.North += bufferDeg
	bb.East += bufferDeg
	return bb
}

// TilesForBBox returns the tiles that cover a given bounding box.
// tileSizeDeg is the edge length of each tile in degrees.
func TilesForBBox(bbox BBox, tileSizeDeg float64) []quality.Tile {
	snap := func(coord float64) float64 {
		return math.Floor(coord/tileSizeDeg) * tileSizeDeg
	}
	var tiles []quality.Tile
	for s := snap(bbox.South); s < bbox.North; s += tileSizeDeg {
		for w := snap(bbox.West); w < bbox.East; w += tileSizeDeg {
			tiles = append(tiles, quality.Tile{
				South: s,
				West:  w,
				North: s + tileSizeDeg,
				East:  w + tileSizeDeg,
			})
		}
	}
	return tiles
}

// WayMatch is a way found in the Overpass tile cache together with the tile it
// was read from.
type WayMatch struct {
	Way  quality.Way
	Tile quality.Tile
}

// FindWaysInOverpassCache searches the Overpass tile cache for ways matching a
// predicate. Each tile is parsed independently; the same way may appear more
// than once if it spans multiple tiles.
func FindWaysInOverpassCache(tiles []quality.Tile, cfg CacheConfig, match func(quality.Way) bool) ([]WayMatch, error) {
	tc := cfg.tileCache()
	var matches []WayMatch
	for _, tile := range tiles {
		if !tc.Has(tile) {
			continue
		}
		data, err := tc.Read(tile)
		if err != nil {
			return nil, fmt.Errorf("reading tile %.3f,%.3f: %w", tile.South, tile.West, err)
		}
		ways, err := quality.ParseTileData(data)
		if err != nil {
			return nil, fmt.Errorf("parsing tile %.3f,%.3f: %w", tile.South, tile.West, err)
		}
		for _, w := range ways {
			if match(w) {
				matches = append(matches, WayMatch{Way: w, Tile: tile})
			}
		}
	}
	return matches, nil
}

// ScoredWayMatch is a scored way found in the score cache together with the
// tile it was read from.
type ScoredWayMatch struct {
	Way  quality.ScoredWay
	Tile quality.Tile
}

// FindWaysInScoreCache searches the score cache for scored ways matching a
// predicate. It reads the raw tile data to validate the score cache entry's
// hash before returning results.
func FindWaysInScoreCache(tiles []quality.Tile, cfg CacheConfig, match func(quality.ScoredWay) bool) ([]ScoredWayMatch, error) {
	tc := cfg.tileCache()
	sc := cfg.scoreCache()
	var matches []ScoredWayMatch
	for _, tile := range tiles {
		if !tc.Has(tile) {
			continue
		}
		rawData, err := tc.Read(tile)
		if err != nil {
			return nil, fmt.Errorf("reading raw tile %.3f,%.3f: %w", tile.South, tile.West, err)
		}
		scoredWays, hit := sc.Read(tile, rawData)
		if !hit {
			continue
		}
		for _, sw := range scoredWays {
			if match(sw) {
				matches = append(matches, ScoredWayMatch{Way: sw, Tile: tile})
			}
		}
	}
	return matches, nil
}

// WayMatchesRef returns a predicate that matches ways whose "ref" tag contains
// the given string (case-sensitive).
func WayMatchesRef(ref string) func(quality.Way) bool {
	return func(w quality.Way) bool {
		return strings.Contains(w.Tags["ref"], ref)
	}
}

// ScoredWayMatchesRef returns a predicate that matches scored ways whose "ref"
// tag contains the given string (case-sensitive).
func ScoredWayMatchesRef(ref string) func(quality.ScoredWay) bool {
	return func(w quality.ScoredWay) bool {
		return strings.Contains(w.Tags["ref"], ref)
	}
}

// WayGeoBounds returns the lat/lon bounding box of a way's geometry nodes.
func WayGeoBounds(w quality.Way) BBox {
	coords := make([]geo.Coord, len(w.Geometry))
	copy(coords, w.Geometry)
	return BBoxForCoords(coords, 0)
}

// ScoredWayGeoBounds returns the lat/lon bounding box of a scored way's
// segment endpoints.
func ScoredWayGeoBounds(w quality.ScoredWay) BBox {
	var coords []geo.Coord
	for _, seg := range w.Segments {
		coords = append(coords, seg.Start, seg.End)
	}
	return BBoxForCoords(coords, 0)
}

// CoordInBBox returns true if the coordinate falls within the bounding box
// (inclusive on all four edges).
func CoordInBBox(c geo.Coord, bb BBox) bool {
	return c.Lat >= bb.South && c.Lat <= bb.North &&
		c.Lon >= bb.West && c.Lon <= bb.East
}

// WayIntersectsBBox returns a predicate that matches ways with at least one
// geometry node inside the bounding box.
func WayIntersectsBBox(bb BBox) func(quality.Way) bool {
	return func(w quality.Way) bool {
		for _, node := range w.Geometry {
			if CoordInBBox(node, bb) {
				return true
			}
		}
		return false
	}
}

// ScoredWayIntersectsBBox returns a predicate that matches scored ways with at
// least one segment endpoint inside the bounding box.
func ScoredWayIntersectsBBox(bb BBox) func(quality.ScoredWay) bool {
	return func(w quality.ScoredWay) bool {
		for _, seg := range w.Segments {
			if CoordInBBox(seg.Start, bb) || CoordInBBox(seg.End, bb) {
				return true
			}
		}
		return false
	}
}

// PipelineResult holds the output of SimulatePipeline.
type PipelineResult struct {
	Grouped     map[string][]quality.ScoredWay
	Collections []quality.RoadCollection
}

// SimulatePipeline runs the full scoring pipeline on cached tile data for a
// given set of tiles. It is single-threaded and deterministic. Tiles with no
// cache data are silently skipped.
func SimulatePipeline(tiles []quality.Tile, cfg CacheConfig) (PipelineResult, error) {
	tc := cfg.tileCache()
	sc := cfg.scoreCache()

	// Phase A: load and score each tile, group by name+ref with WayID dedup.
	grouped := make(map[string][]quality.ScoredWay)
	seenPerGroup := make(map[string]map[int64]bool)

	for _, tile := range tiles {
		if !tc.Has(tile) {
			continue
		}
		rawData, err := tc.Read(tile)
		if err != nil {
			continue
		}

		var scoredWays []quality.ScoredWay
		if cached, hit := sc.Read(tile, rawData); hit {
			scoredWays = cached
		} else {
			ways, err := quality.ParseTileData(rawData)
			if err != nil {
				continue
			}
			result := quality.RunScorePipeline(ways)
			scoredWays = result.ScoredWays
		}

		for _, w := range scoredWays {
			for _, key := range groupKeys(w) {
				if seenPerGroup[key] == nil {
					seenPerGroup[key] = make(map[int64]bool)
				}
				if !seenPerGroup[key][w.WayID] {
					seenPerGroup[key][w.WayID] = true
					grouped[key] = append(grouped[key], w)
				}
			}
		}
	}

	// Phase B: aggregate each name group into road collections.
	var collections []quality.RoadCollection
	for name, ways := range grouped {
		cols := aggregateNameGroup(name, ways)
		collections = append(collections, cols...)
	}

	quality.ApplyPenalties(collections)

	return PipelineResult{
		Grouped:     grouped,
		Collections: collections,
	}, nil
}

// groupKeys returns the grouping keys (name and/or ref) for a scored way.
// Semicolon-separated ref values are split into individual keys.
func groupKeys(w quality.ScoredWay) []string {
	var keys []string
	if name := w.Tags["name"]; name != "" {
		keys = append(keys, name)
	}
	if ref := w.Tags["ref"]; ref != "" {
		for _, r := range strings.Split(ref, ";") {
			r = strings.TrimSpace(r)
			if r != "" {
				keys = append(keys, r)
			}
		}
	}
	return keys
}

// aggregateNameGroup runs connected-component analysis, ordering, deflection
// filtering, and gap splitting for a single name group.
func aggregateNameGroup(name string, ways []quality.ScoredWay) []quality.RoadCollection {
	components := quality.FindConnectedComponents(ways, quality.ConnectedEndpointProximityM)

	wayByID := make(map[int64]quality.ScoredWay, len(ways))
	for _, w := range ways {
		wayByID[w.WayID] = w
	}

	var nameCollections []quality.RoadCollection
	for _, comp := range components {
		compCopy := quality.DeepCopyWays(comp)
		ordered := quality.OrderWays(compCopy)
		chunks := quality.SplitOrderingGaps(ordered, quality.ConnectedEndpointProximityM)

		for _, chunk := range chunks {
			flatSegs := quality.FlattenWaySegments(chunk)
			quality.DeflectionFilterSegments(flatSegs)
			quality.UnflattenWaySegments(chunk, flatSegs)

			segGroups := quality.SplitAtStraightGaps(chunk, quality.StraightGapSplitM)
			for _, segs := range segGroups {
				rc := buildCollection(name, wayByID, segs)
				nameCollections = append(nameCollections, rc)
			}
		}
	}
	for i := range nameCollections {
		nameCollections[i].SubIndex = i
	}
	return nameCollections
}

// buildCollection constructs a RoadCollection from a segment group. It mirrors
// the logic in pipeline.go's buildRoadCollectionFromSegs.
func buildCollection(name string, wayByID map[int64]quality.ScoredWay, segs []quality.ScoredSegment) quality.RoadCollection {
	rc := quality.RoadCollection{Name: name, Segments: segs}

	seenWay := make(map[int64]bool)
	seenHighway := make(map[string]bool)
	for _, seg := range segs {
		if !seenWay[seg.WayID] {
			seenWay[seg.WayID] = true
			rc.WayIDs = append(rc.WayIDs, seg.WayID)
			if w, ok := wayByID[seg.WayID]; ok {
				if hw := w.Tags["highway"]; hw != "" && !seenHighway[hw] {
					seenHighway[hw] = true
					rc.HighwayTypes = append(rc.HighwayTypes, hw)
				}
			}
		}
	}
	for _, seg := range segs {
		rc.TotalScore += seg.Score
		rc.TotalLength += seg.Length
	}
	if rc.TotalLength > 0 {
		rc.ScorePerKm = rc.TotalScore / (rc.TotalLength / 1000.0)
	}
	return rc
}

// TraceStep records what happened at a single pipeline stage.
type TraceStep struct {
	Stage       string
	Description string
	WayCount    int
	SegCount    int
	TotalScore  float64
	TotalLength float64
	Details     string // free-form details about what happened at this stage
}

// TraceResult holds the full trace of a road through the pipeline.
type TraceResult struct {
	Name  string
	Steps []TraceStep
}

// TraceRoad follows a specific road (identified by name or ref) through each
// pipeline stage and returns a structured trace. The function reads only from
// the cache; no network calls are made. Tiles without cached data are silently
// skipped, so calling this function with an incomplete cache will simply show
// fewer ways at the overpass_cache stage.
func TraceRoad(name string, tiles []quality.Tile, cfg CacheConfig) (TraceResult, error) {
	result := TraceResult{Name: name}
	addStep := func(s TraceStep) {
		result.Steps = append(result.Steps, s)
	}

	tc := cfg.tileCache()
	sc := cfg.scoreCache()

	// -----------------------------------------------------------------------
	// Stage 1: overpass_cache — raw ways in the cache matching by name or ref.
	// -----------------------------------------------------------------------
	var rawWays []quality.Way
	seenRawID := make(map[int64]bool)
	for _, tile := range tiles {
		if !tc.Has(tile) {
			continue
		}
		data, err := tc.Read(tile)
		if err != nil {
			continue
		}
		ways, err := quality.ParseTileData(data)
		if err != nil {
			continue
		}
		for _, w := range ways {
			if !matchesNameOrRef(w.Tags, name) {
				continue
			}
			if w.ID != 0 && seenRawID[w.ID] {
				continue
			}
			if w.ID != 0 {
				seenRawID[w.ID] = true
			}
			rawWays = append(rawWays, w)
		}
	}

	var overpassDetails strings.Builder
	for _, w := range rawWays {
		bb := WayGeoBounds(w)
		fmt.Fprintf(&overpassDetails, "Way %d %q (ref=%s) highway=%s nodes=%d\n",
			w.ID, w.Tags["name"], w.Tags["ref"], w.Tags["highway"], len(w.Geometry))
		fmt.Fprintf(&overpassDetails, "  Geo bounds: lat=[%.4f,%.4f] lon=[%.4f,%.4f]\n",
			bb.South, bb.North, bb.West, bb.East)
	}

	addStep(TraceStep{
		Stage:       "overpass_cache",
		Description: fmt.Sprintf("Raw ways matching name/ref %q found in cache", name),
		WayCount:    len(rawWays),
		Details:     overpassDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 2: hard_filter — apply HardFilter to matching raw ways.
	// -----------------------------------------------------------------------
	filtered := quality.HardFilter(rawWays)
	var filterDetails strings.Builder
	dropped := len(rawWays) - len(filtered)
	if dropped > 0 {
		fmt.Fprintf(&filterDetails, "%d ways removed by hard filter\n", dropped)
		for _, w := range rawWays {
			kept := false
			for _, f := range filtered {
				if f.ID == w.ID {
					kept = true
					break
				}
			}
			if !kept {
				fmt.Fprintf(&filterDetails, "  Dropped Way %d: surface=%s access=%s motor_vehicle=%s highway=%s\n",
					w.ID, w.Tags["surface"], w.Tags["access"], w.Tags["motor_vehicle"], w.Tags["highway"])
			}
		}
	}

	addStep(TraceStep{
		Stage:       "hard_filter",
		Description: fmt.Sprintf("%d of %d ways survived hard filter", len(filtered), len(rawWays)),
		WayCount:    len(filtered),
		Details:     filterDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 3: scoring — run curvature scoring on the hard-filtered ways.
	// Use the score cache if available; otherwise score inline.
	// -----------------------------------------------------------------------
	// Collect scored ways for matching raw ways from each tile (cache-aware).
	var scoredWays []quality.ScoredWay
	seenScoredID := make(map[int64]bool)

	for _, tile := range tiles {
		if !tc.Has(tile) {
			continue
		}
		rawData, err := tc.Read(tile)
		if err != nil {
			continue
		}

		// Try score cache first.
		var tileScoredWays []quality.ScoredWay
		if cached, hit := sc.Read(tile, rawData); hit {
			tileScoredWays = cached
		} else {
			ways, err := quality.ParseTileData(rawData)
			if err != nil {
				continue
			}
			pipeResult := quality.RunScorePipeline(ways)
			tileScoredWays = pipeResult.ScoredWays
		}

		for _, sw := range tileScoredWays {
			if !matchesNameOrRef(sw.Tags, name) {
				continue
			}
			if sw.WayID != 0 && seenScoredID[sw.WayID] {
				continue
			}
			if sw.WayID != 0 {
				seenScoredID[sw.WayID] = true
			}
			scoredWays = append(scoredWays, sw)
		}
	}

	// Compute tier distribution and total score.
	tierCounts := make(map[int]int)
	totalSegs := 0
	totalScore := 0.0
	totalLength := 0.0
	for _, sw := range scoredWays {
		for _, seg := range sw.Segments {
			tierCounts[seg.Tier]++
			totalSegs++
			totalScore += seg.Score
			totalLength += seg.Length
		}
	}

	var scoringDetails strings.Builder
	fmt.Fprintf(&scoringDetails, "Total segments: %d\n", totalSegs)
	fmt.Fprintf(&scoringDetails, "Tier distribution: tier0=%d tier1=%d tier2=%d tier3=%d tier4=%d\n",
		tierCounts[0], tierCounts[1], tierCounts[2], tierCounts[3], tierCounts[4])
	fmt.Fprintf(&scoringDetails, "Total score: %.1f  Total length: %.0f m\n", totalScore, totalLength)

	addStep(TraceStep{
		Stage:       "scoring",
		Description: fmt.Sprintf("%d scored ways, total score=%.1f", len(scoredWays), totalScore),
		WayCount:    len(scoredWays),
		SegCount:    totalSegs,
		TotalScore:  totalScore,
		TotalLength: totalLength,
		Details:     scoringDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 4: grouping — group by name+ref and deduplicate by WayID.
	// (Simulates the collector loop in processTilesConcurrently.)
	// -----------------------------------------------------------------------
	// Collect all scored ways from all tiles first, then group.
	allScoredWays := scoredWays // already deduplicated above

	// GroupWays groups by name and ref; a way can appear in both groups.
	allGrouped := quality.GroupWays(allScoredWays)
	groupWays, groupExists := allGrouped[name]

	var groupDetails strings.Builder
	if !groupExists {
		fmt.Fprintf(&groupDetails, "No group found for key %q\n", name)
		// The road is unnamed under this key; check what keys exist.
		for key, ways := range allGrouped {
			fmt.Fprintf(&groupDetails, "  Available group %q: %d ways\n", key, len(ways))
		}
	} else {
		// Dedup within group (as processTilesConcurrently does).
		seenGroup := make(map[int64]bool)
		var dedupedGroup []quality.ScoredWay
		for _, w := range groupWays {
			if !seenGroup[w.WayID] {
				seenGroup[w.WayID] = true
				dedupedGroup = append(dedupedGroup, w)
			}
		}
		groupWays = dedupedGroup
		fmt.Fprintf(&groupDetails, "%d ways in group %q after dedup\n", len(groupWays), name)
		for _, w := range groupWays {
			fmt.Fprintf(&groupDetails, "  Way %d %q highway=%s segments=%d\n",
				w.WayID, w.Tags["name"], w.Tags["highway"], len(w.Segments))
		}
	}

	if groupWays == nil {
		groupWays = []quality.ScoredWay{}
	}

	addStep(TraceStep{
		Stage:       "grouping",
		Description: fmt.Sprintf("%d ways in name group %q", len(groupWays), name),
		WayCount:    len(groupWays),
		Details:     groupDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 5: connected_components.
	// -----------------------------------------------------------------------
	components := quality.FindConnectedComponents(groupWays, quality.ConnectedEndpointProximityM)

	var compDetails strings.Builder
	fmt.Fprintf(&compDetails, "%d connected component(s)\n", len(components))
	for i, comp := range components {
		compLen := 0.0
		for _, w := range comp {
			for _, seg := range w.Segments {
				compLen += seg.Length
			}
		}
		fmt.Fprintf(&compDetails, "  Component %d: %d ways, %.0f m\n", i, len(comp), compLen)
	}

	addStep(TraceStep{
		Stage:       "connected_components",
		Description: fmt.Sprintf("%d connected component(s) from %d ways", len(components), len(groupWays)),
		WayCount:    len(groupWays),
		Details:     compDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 6: ordering & gap splitting.
	// -----------------------------------------------------------------------
	var allOrderedWays []quality.ScoredWay
	var orderDetails strings.Builder
	for i, comp := range components {
		compCopy := quality.DeepCopyWays(comp)
		ordered := quality.OrderWays(compCopy)
		chunks := quality.SplitOrderingGaps(ordered, quality.ConnectedEndpointProximityM)
		totalAfter := 0
		for _, c := range chunks {
			totalAfter += len(c)
			allOrderedWays = append(allOrderedWays, c...)
		}
		fmt.Fprintf(&orderDetails, "  Component %d: %d ways before gap split, %d chunk(s) with %d total ways after\n",
			i, len(ordered), len(chunks), totalAfter)
	}

	addStep(TraceStep{
		Stage:       "ordering",
		Description: fmt.Sprintf("%d ways after ordering and gap splitting across %d component(s)", len(allOrderedWays), len(components)),
		WayCount:    len(allOrderedWays),
		Details:     orderDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 7: deflection_filter — per-component, on the full assembled chain.
	// We need to redo this per chunk to mirror what aggregateNameGroup does.
	// -----------------------------------------------------------------------
	// Redo ordering per component so we can apply deflection on each chunk.
	type compOrdered struct {
		ordered []quality.ScoredWay
	}
	var perCompOrdered []compOrdered
	for _, comp := range components {
		compCopy := quality.DeepCopyWays(comp)
		ordered := quality.OrderWays(compCopy)
		chunks := quality.SplitOrderingGaps(ordered, quality.ConnectedEndpointProximityM)
		for _, chunk := range chunks {
			perCompOrdered = append(perCompOrdered, compOrdered{ordered: chunk})
		}
	}

	scoreBeforeDeflection := 0.0
	scoreAfterDeflection := 0.0
	segsZeroedByDeflection := 0

	var deflectDetails strings.Builder
	for i, pco := range perCompOrdered {
		flatSegs := quality.FlattenWaySegments(pco.ordered)
		before := 0.0
		for _, seg := range flatSegs {
			before += seg.Score
		}
		quality.DeflectionFilterSegments(flatSegs)
		after := 0.0
		zeroed := 0
		for _, seg := range flatSegs {
			after += seg.Score
			if seg.Score == 0 && seg.Tier != 0 {
				// Was non-zero before but zeroed by deflection.
				zeroed++
			}
		}
		// Re-count: segments whose score is now 0 but whose tier became 0 via filter.
		// A simpler proxy: count segments that changed tier to 0.
		zerodCount := 0
		for j, seg := range flatSegs {
			_ = j
			if seg.Tier == 0 && seg.Score == 0 {
				zerodCount++
			}
		}
		scoreBeforeDeflection += before
		scoreAfterDeflection += after
		segsZeroedByDeflection += zeroed
		fmt.Fprintf(&deflectDetails, "  Component %d: score before=%.1f after=%.1f (zeroed=%d)\n",
			i, before, after, zeroed)
		quality.UnflattenWaySegments(pco.ordered, flatSegs)
	}

	addStep(TraceStep{
		Stage:       "deflection_filter",
		Description: fmt.Sprintf("score before=%.1f after=%.1f (%d segments zeroed)", scoreBeforeDeflection, scoreAfterDeflection, segsZeroedByDeflection),
		TotalScore:  scoreAfterDeflection,
		Details:     deflectDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 8: straight_gap_split — split at runs of zero-score > StraightGapSplitM.
	// -----------------------------------------------------------------------
	var allSegGroups [][]quality.ScoredSegment
	var splitDetails strings.Builder
	for i, pco := range perCompOrdered {
		segGroups := quality.SplitAtStraightGaps(pco.ordered, quality.StraightGapSplitM)
		fmt.Fprintf(&splitDetails, "  Component %d: %d segment group(s) after straight-gap split\n", i, len(segGroups))
		for j, grp := range segGroups {
			grpLen := 0.0
			grpScore := 0.0
			for _, seg := range grp {
				grpLen += seg.Length
				grpScore += seg.Score
			}
			fmt.Fprintf(&splitDetails, "    Group %d: %d segs, %.0f m, score=%.1f\n", j, len(grp), grpLen, grpScore)
		}
		allSegGroups = append(allSegGroups, segGroups...)
	}

	addStep(TraceStep{
		Stage:       "straight_gap_split",
		Description: fmt.Sprintf("%d collection(s) after splitting at straight gaps", len(allSegGroups)),
		WayCount:    len(allSegGroups),
		Details:     splitDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 9: penalties.
	// -----------------------------------------------------------------------
	// Build RoadCollections from the segment groups.
	wayByID := make(map[int64]quality.ScoredWay, len(groupWays))
	for _, w := range groupWays {
		wayByID[w.WayID] = w
	}

	var prepenaltyCollections []quality.RoadCollection
	for _, segs := range allSegGroups {
		rc := buildCollection(name, wayByID, segs)
		prepenaltyCollections = append(prepenaltyCollections, rc)
	}
	for i := range prepenaltyCollections {
		prepenaltyCollections[i].SubIndex = i
	}
	quality.ApplyPenalties(prepenaltyCollections)

	var penaltyDetails strings.Builder
	for i, rc := range prepenaltyCollections {
		fmt.Fprintf(&penaltyDetails,
			"  Collection %d: highway types=%v penalty factor=%.2f score before=%.1f penalized=%.1f\n",
			i, rc.HighwayTypes, rc.HighwayPenaltyFactor, rc.TotalScore, rc.PenalizedScore)
	}

	addStep(TraceStep{
		Stage:       "penalties",
		Description: fmt.Sprintf("%d collection(s) after penalty application", len(prepenaltyCollections)),
		WayCount:    len(prepenaltyCollections),
		Details:     penaltyDetails.String(),
	})

	// -----------------------------------------------------------------------
	// Stage 10: output_filter — minScore=0, minLength=MinRoadLengthM.
	// -----------------------------------------------------------------------
	const minScore = 0.0

	var outputDetails strings.Builder
	passed := 0
	for i, rc := range prepenaltyCollections {
		passScore := rc.PenalizedScore > minScore
		passLength := rc.TotalLength >= quality.MinRoadLengthM
		passes := passScore && passLength
		if passes {
			passed++
		}
		status := "PASS"
		if !passes {
			status = "FAIL"
		}
		fmt.Fprintf(&outputDetails,
			"  Collection %d [%s]: penalized_score=%.1f (>%.1f: %v) length=%.0f m (>=%.0f m: %v)\n",
			i, status,
			rc.PenalizedScore, minScore, passScore,
			rc.TotalLength, quality.MinRoadLengthM, passLength)
	}

	addStep(TraceStep{
		Stage:       "output_filter",
		Description: fmt.Sprintf("%d of %d collection(s) pass output filter", passed, len(prepenaltyCollections)),
		WayCount:    passed,
		Details:     outputDetails.String(),
	})

	return result, nil
}

// matchesNameOrRef returns true if the tags map has a "name" or "ref" value
// that equals (case-sensitive) the given key.
func matchesNameOrRef(tags map[string]string, key string) bool {
	return tags["name"] == key || tags["ref"] == key
}

// AllCachedTiles returns every tile present in the Overpass cache directory.
// It parses tile filenames to reconstruct the Tile coordinates.
func AllCachedTiles(cfg CacheConfig) ([]quality.Tile, error) {
	entries, err := os.ReadDir(cfg.OverpassDir)
	if err != nil {
		return nil, fmt.Errorf("reading cache dir: %w", err)
	}
	var tiles []quality.Tile
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		// Format: tile_<south>_<west>
		// We can't use fmt.Sscanf because %f greedily consumes the underscore
		// separator. Split manually instead.
		rest := strings.TrimPrefix(name, "tile_")
		if rest == name {
			continue // didn't have "tile_" prefix
		}
		south, west, ok := parseTwoFloats(rest)
		if !ok {
			continue
		}
		tiles = append(tiles, quality.Tile{
			South: south,
			West:  west,
			North: south + 0.05, // default tile size
			East:  west + 0.05,
		})
	}
	return tiles, nil
}

// parseTwoFloats splits a string like "40.700_-76.300" at the underscore
// separating two float values. The second value may be negative, so we find
// the split point by looking for "_" followed by a digit or minus sign after
// the first character.
func parseTwoFloats(s string) (a, b float64, ok bool) {
	// Find the split underscore. The first float ends at the first "_" that
	// is followed by a digit or '-'. We skip index 0 since the first float
	// itself might start with '-'.
	splitIdx := -1
	for i := 1; i < len(s)-1; i++ {
		if s[i] == '_' {
			splitIdx = i
			break
		}
	}
	if splitIdx < 0 {
		return 0, 0, false
	}
	_, err1 := fmt.Sscan(s[:splitIdx], &a)
	_, err2 := fmt.Sscan(s[splitIdx+1:], &b)
	return a, b, err1 == nil && err2 == nil
}

// DeduplicateWayMatches returns a deduplicated slice of WayMatch, keeping the
// first occurrence of each Way ID. Ways with ID 0 are never deduplicated.
func DeduplicateWayMatches(matches []WayMatch) []WayMatch {
	seen := make(map[int64]bool)
	var result []WayMatch
	for _, m := range matches {
		if m.Way.ID == 0 {
			result = append(result, m)
			continue
		}
		if !seen[m.Way.ID] {
			seen[m.Way.ID] = true
			result = append(result, m)
		}
	}
	return result
}

// WayEndpoint represents one end of a way (first or last geometry node).
type WayEndpoint struct {
	WayID int64
	Coord geo.Coord
	IsEnd bool // false = start, true = end
}

// WayGap represents a gap between two way endpoints that exceeds the
// connectivity threshold.
type WayGap struct {
	FromWayID int64
	FromCoord geo.Coord
	ToWayID   int64
	ToCoord   geo.Coord
	DistanceM float64
}

// ContinuityResult holds the analysis of whether a set of ways forms a
// contiguous path.
type ContinuityResult struct {
	TotalWays       int
	TotalNodes      int
	Components      int          // number of connected components
	ComponentSizes  []int        // way count per component
	Gaps            []WayGap     // gaps between closest unconnected endpoints
	IsContiguous    bool         // true if all ways form a single connected chain
	OverallBBox     BBox         // bounding box of all ways
	TotalLengthM    float64      // sum of haversine distances across all way geometry
}

// AnalyzeWayContinuity checks whether a set of ways form a contiguous path.
// It uses the same connected-component logic as the pipeline (endpoint
// proximity), then identifies gaps between components.
func AnalyzeWayContinuity(ways []quality.Way, proximityM float64) ContinuityResult {
	result := ContinuityResult{
		TotalWays: len(ways),
	}
	if len(ways) == 0 {
		return result
	}

	// Collect all geometry coordinates for bounding box, and compute total length.
	var allCoords []geo.Coord
	for _, w := range ways {
		result.TotalNodes += len(w.Geometry)
		allCoords = append(allCoords, w.Geometry...)
		for i := 0; i < len(w.Geometry)-1; i++ {
			result.TotalLengthM += geo.Haversine(w.Geometry[i], w.Geometry[i+1])
		}
	}
	result.OverallBBox = BBoxForCoords(allCoords, 0)

	// Score the ways so we can use FindConnectedComponents (which works on
	// ScoredWay). We only need the geometry endpoints.
	scored := make([]quality.ScoredWay, len(ways))
	for i, w := range ways {
		segs := make([]quality.ScoredSegment, 0)
		if len(w.Geometry) >= 2 {
			segs = append(segs, quality.ScoredSegment{
				Start: w.Geometry[0],
				End:   w.Geometry[len(w.Geometry)-1],
			})
		}
		scored[i] = quality.ScoredWay{
			WayID:    w.ID,
			Tags:     w.Tags,
			Segments: segs,
		}
	}

	components := quality.FindConnectedComponents(scored, proximityM)
	result.Components = len(components)
	result.IsContiguous = len(components) <= 1

	for _, comp := range components {
		result.ComponentSizes = append(result.ComponentSizes, len(comp))
	}

	// Find gaps between components: for each pair of components, find the
	// closest pair of endpoints.
	if len(components) > 1 {
		type compEndpoints struct {
			starts []WayEndpoint
			ends   []WayEndpoint
		}
		var compEps []compEndpoints
		for _, comp := range components {
			var ce compEndpoints
			for _, sw := range comp {
				if len(sw.Segments) > 0 {
					ce.starts = append(ce.starts, WayEndpoint{
						WayID: sw.WayID, Coord: sw.Segments[0].Start, IsEnd: false,
					})
					ce.ends = append(ce.ends, WayEndpoint{
						WayID: sw.WayID, Coord: sw.Segments[len(sw.Segments)-1].End, IsEnd: true,
					})
				}
			}
			compEps = append(compEps, ce)
		}

		// For each pair of components, find minimum gap.
		for i := 0; i < len(compEps); i++ {
			for j := i + 1; j < len(compEps); j++ {
				bestDist := math.MaxFloat64
				var bestGap WayGap
				allI := append(compEps[i].starts, compEps[i].ends...)
				allJ := append(compEps[j].starts, compEps[j].ends...)
				for _, ei := range allI {
					for _, ej := range allJ {
						d := geo.Haversine(ei.Coord, ej.Coord)
						if d < bestDist {
							bestDist = d
							bestGap = WayGap{
								FromWayID: ei.WayID,
								FromCoord: ei.Coord,
								ToWayID:   ej.WayID,
								ToCoord:   ej.Coord,
								DistanceM: d,
							}
						}
					}
				}
				result.Gaps = append(result.Gaps, bestGap)
			}
		}
	}

	return result
}

// OrderWaysByGeography sorts ways by their starting latitude (south to north),
// breaking ties by starting longitude (west to east). This produces a
// human-readable order for ways along a route.
func OrderWaysByGeography(ways []quality.Way) []quality.Way {
	sorted := make([]quality.Way, len(ways))
	copy(sorted, ways)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			a, b := sorted[j-1], sorted[j]
			aLat, aLon := wayStartCoord(a)
			bLat, bLon := wayStartCoord(b)
			if aLat > bLat || (aLat == bLat && aLon > bLon) {
				sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
			}
		}
	}
	return sorted
}

func wayStartCoord(w quality.Way) (lat, lon float64) {
	if len(w.Geometry) == 0 {
		return 0, 0
	}
	return w.Geometry[0].Lat, w.Geometry[0].Lon
}

// ---------------------------------------------------------------------------
// Full-pipeline simulation and road-level collection search
// ---------------------------------------------------------------------------

// SimulatePipelineResult extends PipelineResult with per-group detail so
// callers can inspect what happened to a specific road name/ref without
// re-running the pipeline.
type SimulatePipelineResult struct {
	PipelineResult

	// GroupDetail maps each name/ref key to the intermediate aggregation
	// state for that group. Populated only when DetailKeys is non-empty in
	// the options.
	GroupDetail map[string]*GroupAggregationDetail
}

// GroupAggregationDetail captures every intermediate step of the aggregation
// pipeline for a single name/ref group. It mirrors the stages in
// pipeline.go's processNameGroup.
type GroupAggregationDetail struct {
	// Name is the grouping key (a road name or ref tag value).
	Name string

	// InputWays is the deduplicated set of scored ways that entered the group.
	InputWays []quality.ScoredWay

	// Components is the result of FindConnectedComponents.
	Components [][]quality.ScoredWay

	// PerComponent holds the per-component processing details.
	PerComponent []ComponentDetail

	// Collections is the final set of RoadCollections produced by this group,
	// after penalties have been applied.
	Collections []quality.RoadCollection
}

// ComponentDetail captures the intermediate state of processing a single
// connected component within a name group.
type ComponentDetail struct {
	// InputWays is the ways in this component before ordering.
	InputWays []quality.ScoredWay

	// OrderedWays is the ways after OrderWays + SplitOrderingGaps.
	OrderedWays []quality.ScoredWay

	// DroppedByOrdering is the number of ways dropped by SplitOrderingGaps.
	DroppedByOrdering int

	// ScoreBeforeDeflection is the total score of all segments before the
	// deflection filter is applied.
	ScoreBeforeDeflection float64

	// ScoreAfterDeflection is the total score after deflection filtering.
	ScoreAfterDeflection float64

	// SegmentsZeroedByDeflection is the number of segments whose score was
	// zeroed by the deflection filter.
	SegmentsZeroedByDeflection int

	// SegmentGroups is the result of SplitAtStraightGaps: one or more
	// contiguous runs of segments, each becoming a RoadCollection.
	SegmentGroups [][]quality.ScoredSegment
}

// SimulatePipelineOptions controls what extra detail SimulatePipelineFull
// collects.
type SimulatePipelineOptions struct {
	// DetailKeys lists name/ref keys for which full aggregation detail should
	// be captured. If empty, no detail is collected (faster).
	DetailKeys []string
}

// SimulatePipelineFull runs the full scoring pipeline on cached tile data,
// exactly mirroring the production pipeline (processTilesConcurrently +
// processNameGroupsConcurrently), but single-threaded and deterministic. When
// opts.DetailKeys is non-empty, it also captures per-stage detail for the
// specified name/ref groups so callers can see exactly where roads are
// modified or dropped.
//
// This function reads from both the Overpass and score caches. Tiles not
// present in the Overpass cache are silently skipped. No network calls are
// made.
func SimulatePipelineFull(tiles []quality.Tile, cfg CacheConfig, opts SimulatePipelineOptions) (SimulatePipelineResult, error) {
	tc := cfg.tileCache()
	sc := cfg.scoreCache()

	// Phase A: load and score each tile, group by name+ref with WayID dedup.
	// This exactly mirrors processTilesConcurrently's collector loop.
	grouped := make(map[string][]quality.ScoredWay)
	seenPerGroup := make(map[string]map[int64]bool)

	for _, tile := range tiles {
		if !tc.Has(tile) {
			continue
		}
		rawData, err := tc.Read(tile)
		if err != nil {
			continue
		}

		var scoredWays []quality.ScoredWay
		if cached, hit := sc.Read(tile, rawData); hit {
			scoredWays = cached
		} else {
			ways, err := quality.ParseTileData(rawData)
			if err != nil {
				continue
			}
			result := quality.RunScorePipeline(ways)
			scoredWays = result.ScoredWays
		}

		for _, w := range scoredWays {
			for _, key := range groupKeys(w) {
				if seenPerGroup[key] == nil {
					seenPerGroup[key] = make(map[int64]bool)
				}
				if !seenPerGroup[key][w.WayID] {
					seenPerGroup[key][w.WayID] = true
					grouped[key] = append(grouped[key], w)
				}
			}
		}
	}

	// Build set of detail keys for quick lookup.
	wantDetail := make(map[string]bool, len(opts.DetailKeys))
	for _, k := range opts.DetailKeys {
		wantDetail[k] = true
	}

	// Phase B: aggregate each name group, optionally capturing detail.
	var collections []quality.RoadCollection
	details := make(map[string]*GroupAggregationDetail)

	for name, ways := range grouped {
		if wantDetail[name] {
			detail := aggregateNameGroupDetailed(name, ways)
			details[name] = detail
			collections = append(collections, detail.Collections...)
		} else {
			cols := aggregateNameGroup(name, ways)
			collections = append(collections, cols...)
		}
	}

	quality.ApplyPenalties(collections)

	// Update the detail collections with post-penalty values by matching.
	for _, detail := range details {
		for i := range detail.Collections {
			for j := range collections {
				if detail.Collections[i].Name == collections[j].Name &&
					detail.Collections[i].SubIndex == collections[j].SubIndex &&
					len(detail.Collections[i].WayIDs) > 0 &&
					len(collections[j].WayIDs) > 0 &&
					detail.Collections[i].WayIDs[0] == collections[j].WayIDs[0] {
					detail.Collections[i] = collections[j]
					break
				}
			}
		}
	}

	return SimulatePipelineResult{
		PipelineResult: PipelineResult{
			Grouped:     grouped,
			Collections: collections,
		},
		GroupDetail: details,
	}, nil
}

// aggregateNameGroupDetailed is like aggregateNameGroup but captures
// intermediate state at each processing step for diagnostic inspection.
func aggregateNameGroupDetailed(name string, ways []quality.ScoredWay) *GroupAggregationDetail {
	detail := &GroupAggregationDetail{
		Name:      name,
		InputWays: ways,
	}

	components := quality.FindConnectedComponents(ways, quality.ConnectedEndpointProximityM)
	detail.Components = components

	wayByID := make(map[int64]quality.ScoredWay, len(ways))
	for _, w := range ways {
		wayByID[w.WayID] = w
	}

	var nameCollections []quality.RoadCollection

	for _, comp := range components {
		cd := ComponentDetail{
			InputWays: comp,
		}

		compCopy := quality.DeepCopyWays(comp)
		ordered := quality.OrderWays(compCopy)
		chunks := quality.SplitOrderingGaps(ordered, quality.ConnectedEndpointProximityM)

		// Flatten all chunks into OrderedWays for diagnostic inspection.
		for _, chunk := range chunks {
			cd.OrderedWays = append(cd.OrderedWays, chunk...)
		}
		cd.DroppedByOrdering = 0 // no ways dropped; all chunks preserved

		for _, chunk := range chunks {
			// Deflection filter per chunk.
			flatSegs := quality.FlattenWaySegments(chunk)
			for _, seg := range flatSegs {
				cd.ScoreBeforeDeflection += seg.Score
			}
			quality.DeflectionFilterSegments(flatSegs)
			for _, seg := range flatSegs {
				cd.ScoreAfterDeflection += seg.Score
			}
			cd.SegmentsZeroedByDeflection += countZeroedSegments(flatSegs)
			quality.UnflattenWaySegments(chunk, flatSegs)

			// Straight-gap split.
			segGroups := quality.SplitAtStraightGaps(chunk, quality.StraightGapSplitM)
			cd.SegmentGroups = append(cd.SegmentGroups, segGroups...)

			for _, segs := range segGroups {
				rc := buildCollection(name, wayByID, segs)
				nameCollections = append(nameCollections, rc)
			}
		}

		detail.PerComponent = append(detail.PerComponent, cd)
	}

	for i := range nameCollections {
		nameCollections[i].SubIndex = i
	}
	detail.Collections = nameCollections

	return detail
}

// SegmentGroupBBox returns the bounding box of a segment group's coordinates.
func SegmentGroupBBox(segs []quality.ScoredSegment) BBox {
	var coords []geo.Coord
	for _, seg := range segs {
		coords = append(coords, seg.Start, seg.End)
	}
	return BBoxForCoords(coords, 0)
}

// SegmentGroupEndpoints returns the first start coord and last end coord of a
// segment group.
func SegmentGroupEndpoints(segs []quality.ScoredSegment) (start, end geo.Coord) {
	if len(segs) == 0 {
		return geo.Coord{}, geo.Coord{}
	}
	return segs[0].Start, segs[len(segs)-1].End
}

// SegmentGroupWayIDs returns the unique way IDs present in a segment group,
// in first-seen order.
func SegmentGroupWayIDs(segs []quality.ScoredSegment) []int64 {
	seen := make(map[int64]bool)
	var ids []int64
	for _, seg := range segs {
		if !seen[seg.WayID] {
			seen[seg.WayID] = true
			ids = append(ids, seg.WayID)
		}
	}
	return ids
}

// countZeroedSegments counts segments that have Tier==0 and Score==0 in a flat
// segment slice. This is an approximation of segments modified by the
// deflection filter (segments that were tier>0 before but got zeroed).
func countZeroedSegments(segs []quality.ScoredSegment) int {
	count := 0
	for _, seg := range segs {
		if seg.Tier == 0 && seg.Score == 0 {
			count++
		}
	}
	return count
}

// FindCollections searches a slice of RoadCollections for entries whose Name
// matches the given name exactly. Useful for finding a specific road's output
// after running SimulatePipelineFull.
func FindCollections(collections []quality.RoadCollection, name string) []quality.RoadCollection {
	var result []quality.RoadCollection
	for _, c := range collections {
		if c.Name == name {
			result = append(result, c)
		}
	}
	return result
}

// FindCollectionsContainingWay searches a slice of RoadCollections for entries
// that include segments from the given OSM way ID. This is useful for finding
// which output collection(s) a specific way ended up in, regardless of the
// collection's name.
func FindCollectionsContainingWay(collections []quality.RoadCollection, wayID int64) []quality.RoadCollection {
	var result []quality.RoadCollection
	for _, c := range collections {
		for _, id := range c.WayIDs {
			if id == wayID {
				result = append(result, c)
				break
			}
		}
	}
	return result
}

// CollectionPassesOutputFilter returns true if a RoadCollection passes the
// standard output filter (PenalizedScore >= minScore AND TotalLength >=
// MinRoadLengthM). When minScore is 0, any positive penalized score passes.
func CollectionPassesOutputFilter(c quality.RoadCollection, minScore float64) bool {
	return c.PenalizedScore >= minScore && c.TotalLength >= quality.MinRoadLengthM
}

// FormatCollectionSummary returns a human-readable one-line summary of a
// RoadCollection, including name, scores, length, highway types, and whether
// it passes the output filter.
func FormatCollectionSummary(c quality.RoadCollection, minScore float64) string {
	passes := CollectionPassesOutputFilter(c, minScore)
	passStr := "PASS"
	if !passes {
		passStr = "FAIL"
	}
	return fmt.Sprintf("[%s] %s (sub=%d): score=%.1f penalized=%.1f (factor=%.2f) length=%.0f m (%.2f km) scorePerKm=%.1f types=%v ways=%d",
		passStr,
		c.Name, c.SubIndex,
		c.TotalScore, c.PenalizedScore, c.HighwayPenaltyFactor,
		c.TotalLength, c.TotalLength/1000,
		c.PenalizedPerKm,
		c.HighwayTypes,
		len(c.WayIDs),
	)
}

// ---------------------------------------------------------------------------
// Way-level tile lookup and collection-to-KML comparison
// ---------------------------------------------------------------------------

// WayTileLocation records which tile a specific way ID was found in.
type WayTileLocation struct {
	WayID int64
	Tile  quality.Tile
	Found bool
}

// LocateWaysInCache looks up each way ID in the Overpass cache across the
// given tiles. Returns one entry per way ID indicating whether it was found
// and in which tile. This is useful for answering "does this way exist in the
// cache for a given tile grid?"
func LocateWaysInCache(wayIDs []int64, tiles []quality.Tile, cfg CacheConfig) ([]WayTileLocation, error) {
	tc := cfg.tileCache()

	// Build a set of target IDs.
	wanted := make(map[int64]bool, len(wayIDs))
	for _, id := range wayIDs {
		wanted[id] = true
	}

	found := make(map[int64]quality.Tile)

	for _, tile := range tiles {
		if !tc.Has(tile) {
			continue
		}
		data, err := tc.Read(tile)
		if err != nil {
			continue
		}
		ways, err := quality.ParseTileData(data)
		if err != nil {
			continue
		}
		for _, w := range ways {
			if wanted[w.ID] {
				if _, already := found[w.ID]; !already {
					found[w.ID] = tile
				}
			}
		}
		// Early exit if all found.
		if len(found) == len(wanted) {
			break
		}
	}

	results := make([]WayTileLocation, len(wayIDs))
	for i, id := range wayIDs {
		tile, ok := found[id]
		results[i] = WayTileLocation{WayID: id, Tile: tile, Found: ok}
	}
	return results, nil
}

// CompareCollectionsToKML compares a pipeline simulation's output collections
// against a set of known KML way IDs (extracted from a KML file). Returns
// information about which simulation collections have ways that are present or
// absent in the KML output.
type CollectionComparison struct {
	// Collection is the simulated RoadCollection.
	Collection quality.RoadCollection

	// WaysInKML lists way IDs from this collection that appear in the KML.
	WaysInKML []int64

	// WaysMissing lists way IDs from this collection that do NOT appear in the KML.
	WaysMissing []int64

	// InKML is true if at least one way from this collection is in the KML.
	InKML bool

	// PassesFilter is true if this collection passes the output filter.
	PassesFilter bool
}

// CompareCollectionsToKMLWays compares simulated collections for a given road
// name against the set of way IDs found in the KML output. kmlWayIDs should
// contain all way IDs present anywhere in the KML for this road name.
func CompareCollectionsToKMLWays(collections []quality.RoadCollection, kmlWayIDs []int64, minScore float64) []CollectionComparison {
	kmlSet := make(map[int64]bool, len(kmlWayIDs))
	for _, id := range kmlWayIDs {
		kmlSet[id] = true
	}

	var results []CollectionComparison
	for _, c := range collections {
		comp := CollectionComparison{
			Collection:   c,
			PassesFilter: CollectionPassesOutputFilter(c, minScore),
		}
		for _, id := range c.WayIDs {
			if kmlSet[id] {
				comp.WaysInKML = append(comp.WaysInKML, id)
				comp.InKML = true
			} else {
				comp.WaysMissing = append(comp.WaysMissing, id)
			}
		}
		results = append(results, comp)
	}
	return results
}

// SortCollectionsByScore sorts a slice of RoadCollections by PenalizedScore
// descending. Returns a new sorted slice.
func SortCollectionsByScore(collections []quality.RoadCollection) []quality.RoadCollection {
	sorted := make([]quality.RoadCollection, len(collections))
	copy(sorted, collections)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PenalizedScore > sorted[j].PenalizedScore
	})
	return sorted
}

// ---------------------------------------------------------------------------
// KML parsing and collection spacing analysis
// ---------------------------------------------------------------------------

// KMLRoadEntry represents a single road entry parsed from a KML file,
// including its name, description, way IDs, and the start/end coordinates of
// its geometry.
type KMLRoadEntry struct {
	Name        string
	Description string
	WayIDs      []int64
	StartCoord  geo.Coord // first coordinate in the KML linestring
	EndCoord    geo.Coord // last coordinate in the KML linestring
	BBox        BBox      // bounding box of all coordinates
	NumCoords   int       // total number of coordinate points
}

// ParseKMLRoadEntries parses a KML file and returns all road entries whose
// folder name starts with the given prefix. Each entry includes parsed way IDs
// from the description and the start/end coordinates of the geometry.
func ParseKMLRoadEntries(kmlPath, namePrefix string) ([]KMLRoadEntry, error) {
	data, err := os.ReadFile(kmlPath)
	if err != nil {
		return nil, fmt.Errorf("reading KML: %w", err)
	}

	type xmlPlacemark struct {
		LineString struct {
			Coordinates string `xml:"coordinates"`
		} `xml:"LineString"`
	}
	type xmlFolder struct {
		Name        string         `xml:"name"`
		Description string         `xml:"description"`
		Placemarks  []xmlPlacemark `xml:"Placemark"`
	}
	type xmlDoc struct {
		Folders []xmlFolder `xml:"Document>Folder"`
	}

	var doc xmlDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing KML XML: %w", err)
	}

	var entries []KMLRoadEntry
	for _, f := range doc.Folders {
		if !strings.HasPrefix(f.Name, namePrefix) {
			continue
		}

		entry := KMLRoadEntry{
			Name:        f.Name,
			Description: f.Description,
		}

		// Parse way IDs from description.
		if idx := strings.Index(f.Description, "Ways: "); idx >= 0 {
			waysStr := f.Description[idx+len("Ways: "):]
			for _, idStr := range strings.Split(waysStr, ", ") {
				if id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64); err == nil {
					entry.WayIDs = append(entry.WayIDs, id)
				}
			}
		}

		// Parse coordinates from all placemarks (multi-color KML may have
		// multiple placemarks per folder, one per tier run).
		var allCoords []geo.Coord
		for _, pm := range f.Placemarks {
			coords := parseKMLCoordinates(pm.LineString.Coordinates)
			allCoords = append(allCoords, coords...)
		}

		if len(allCoords) > 0 {
			entry.StartCoord = allCoords[0]
			entry.EndCoord = allCoords[len(allCoords)-1]
			entry.BBox = BBoxForCoords(allCoords, 0)
			entry.NumCoords = len(allCoords)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// parseKMLCoordinates parses a KML coordinate string ("lon,lat,alt lon,lat,alt ...")
// into a slice of Coords.
func parseKMLCoordinates(s string) []geo.Coord {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	var coords []geo.Coord
	for _, p := range parts {
		fields := strings.Split(p, ",")
		if len(fields) < 2 {
			continue
		}
		lon, err1 := strconv.ParseFloat(fields[0], 64)
		lat, err2 := strconv.ParseFloat(fields[1], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		coords = append(coords, geo.Coord{Lat: lat, Lon: lon})
	}
	return coords
}

// CollectionGap measures the distance between two KML road entries'
// nearest endpoints.
type CollectionGap struct {
	FromName  string
	FromCoord geo.Coord
	ToName    string
	ToCoord   geo.Coord
	DistanceM float64
}

// AnalyzeCollectionSpacing computes the minimum endpoint distance between
// every pair of KML road entries. This answers the question "how far apart
// are these collections?" Returns gaps sorted by distance ascending.
func AnalyzeCollectionSpacing(entries []KMLRoadEntry) []CollectionGap {
	var gaps []CollectionGap
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			// Check all four endpoint pairs.
			type pair struct {
				from, to geo.Coord
			}
			pairs := []pair{
				{a.StartCoord, b.StartCoord},
				{a.StartCoord, b.EndCoord},
				{a.EndCoord, b.StartCoord},
				{a.EndCoord, b.EndCoord},
			}
			bestDist := math.MaxFloat64
			var bestPair pair
			for _, p := range pairs {
				d := geo.Haversine(p.from, p.to)
				if d < bestDist {
					bestDist = d
					bestPair = p
				}
			}
			gaps = append(gaps, CollectionGap{
				FromName:  a.Name,
				FromCoord: bestPair.from,
				ToName:    b.Name,
				ToCoord:   bestPair.to,
				DistanceM: bestDist,
			})
		}
	}

	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].DistanceM < gaps[j].DistanceM
	})
	return gaps
}
