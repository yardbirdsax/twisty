package quality

import (
	"fmt"

	"github.com/yardbirdsax/twisty/geo"
)

// RoadCollection represents a named group of contiguous road segments
// that have been aggregated from individual scored ways.
type RoadCollection struct {
	Name         string
	SubIndex     int      // 0-based index when a named road is split into multiple collections
	HighwayTypes []string // unique highway types present in this collection
	WayIDs       []int64
	Segments     []ScoredSegment

	TotalScore  float64
	TotalLength float64 // meters
	ScorePerKm  float64

	// Populated by penalty stage (stage 6)
	HighwayPenaltyFactor float64
	PenalizedScore       float64
	PenalizedPerKm       float64
}

// DisplayName returns the name of the collection, with a 1-based suffix in
// parentheses when SubIndex > 0.
//
//	SubIndex 0: "Route 100"
//	SubIndex 1: "Route 100 (2)"
//	SubIndex 2: "Route 100 (3)"
func (r RoadCollection) DisplayName() string {
	if r.SubIndex == 0 {
		return r.Name
	}
	return fmt.Sprintf("%s (%d)", r.Name, r.SubIndex+1)
}

// GroupWaysByName groups scored ways by their "name" tag.
// Ways without a name tag are excluded.
func GroupWaysByName(ways []ScoredWay) map[string][]ScoredWay {
	result := make(map[string][]ScoredWay)
	for _, w := range ways {
		name := w.Tags["name"]
		if name == "" {
			continue
		}
		result[name] = append(result[name], w)
	}
	return result
}

// GroupWays groups scored ways by both their "name" and "ref" tags.
// A way with both tags appears in both groups. Ways with neither tag are excluded.
// This enables roads like "PA 345" (which changes name along its length) to be
// treated as a single road via the ref grouping.
func GroupWays(ways []ScoredWay) map[string][]ScoredWay {
	result := make(map[string][]ScoredWay)
	for _, w := range ways {
		if name := w.Tags["name"]; name != "" {
			result[name] = append(result[name], w)
		}
		if ref := w.Tags["ref"]; ref != "" {
			result[ref] = append(result[ref], w)
		}
	}
	return result
}

// wayEndpoints extracts the start and end coordinates of a ScoredWay.
// Returns zero coords and false if the way has no segments.
func wayEndpoints(w ScoredWay) (start, end geo.Coord, ok bool) {
	if len(w.Segments) == 0 {
		return geo.Coord{}, geo.Coord{}, false
	}
	return w.Segments[0].Start, w.Segments[len(w.Segments)-1].End, true
}

// FindConnectedComponents groups ways by endpoint proximity.
// Two ways are connected if any endpoint of one is within proximityM meters
// of any endpoint of the other.
func FindConnectedComponents(ways []ScoredWay, proximityM float64) [][]ScoredWay {
	n := len(ways)
	if n == 0 {
		return nil
	}

	// Union-Find
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	union := func(x, y int) {
		px, py := find(x), find(y)
		if px != py {
			parent[px] = py
		}
	}

	// Precompute endpoints; skip ways with no segments.
	type endpoints struct {
		start, end geo.Coord
		valid      bool
	}
	eps := make([]endpoints, n)
	for i, w := range ways {
		s, e, ok := wayEndpoints(w)
		eps[i] = endpoints{start: s, end: e, valid: ok}
	}

	for i := range n {
		if !eps[i].valid {
			continue
		}
		for j := i + 1; j < n; j++ {
			if !eps[j].valid {
				continue
			}
			if find(i) == find(j) {
				continue
			}
			// Check all four endpoint pairs.
			if geo.Haversine(eps[i].start, eps[j].start) <= proximityM ||
				geo.Haversine(eps[i].start, eps[j].end) <= proximityM ||
				geo.Haversine(eps[i].end, eps[j].start) <= proximityM ||
				geo.Haversine(eps[i].end, eps[j].end) <= proximityM {
				union(i, j)
			}
		}
	}

	// Collect into components indexed by root.
	compMap := make(map[int][]ScoredWay)
	for i, w := range ways {
		root := find(i)
		compMap[root] = append(compMap[root], w)
	}

	components := make([][]ScoredWay, 0, len(compMap))
	for _, comp := range compMap {
		components = append(components, comp)
	}
	return components
}

// OrderWays arranges ways in a connected component into a continuous path
// by chaining endpoints.
func OrderWays(ways []ScoredWay) []ScoredWay {
	if len(ways) == 0 {
		return nil
	}
	if len(ways) == 1 {
		return ways
	}

	remaining := make([]ScoredWay, len(ways))
	copy(remaining, ways)

	// Start with the first way.
	ordered := make([]ScoredWay, 0, len(ways))
	ordered = append(ordered, remaining[0])
	remaining = remaining[1:]

	for len(remaining) > 0 {
		// Current chain end.
		cur := ordered[len(ordered)-1]
		_, chainEnd, ok := wayEndpoints(cur)
		if !ok {
			// Can't extend from a zero-segment way; just append remaining in order.
			ordered = append(ordered, remaining...)
			break
		}

		bestIdx := -1
		bestDist := -1.0
		bestReverse := false

		for i, w := range remaining {
			s, e, wok := wayEndpoints(w)
			if !wok {
				continue
			}
			distStart := geo.Haversine(chainEnd, s)
			distEnd := geo.Haversine(chainEnd, e)
			d := distStart
			rev := false
			if distEnd < distStart {
				d = distEnd
				rev = true
			}
			if bestIdx == -1 || d < bestDist {
				bestIdx = i
				bestDist = d
				bestReverse = rev
			}
		}

		if bestIdx == -1 {
			// No valid way found (all remaining have zero segments); append rest.
			ordered = append(ordered, remaining...)
			break
		}

		next := remaining[bestIdx]
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)

		if bestReverse {
			next = reverseWay(next)
		}
		ordered = append(ordered, next)
	}

	return ordered
}

// SplitOrderingGaps detects large gaps in an ordered way chain and returns
// only the longest contiguous sub-chain. A gap is defined as a distance
// between consecutive ways' shared endpoints exceeding maxGapM meters.
//
// This handles cases where OrderWays' greedy nearest-neighbor algorithm
// appends a way that jumps far from the chain end (e.g., overlapping OSM
// ways that retrace already-covered ground).
func SplitOrderingGaps(ordered []ScoredWay, maxGapM float64) []ScoredWay {
	if len(ordered) <= 1 {
		return ordered
	}

	// Find gap positions
	type chunk struct {
		start, end int // indices into ordered, inclusive
	}
	var chunks []chunk
	chunkStart := 0

	for i := 0; i < len(ordered)-1; i++ {
		_, curEnd, curOK := wayEndpoints(ordered[i])
		nextStart, _, nextOK := wayEndpoints(ordered[i+1])
		if !curOK || !nextOK {
			continue
		}
		dist := geo.Haversine(curEnd, nextStart)
		if dist > maxGapM {
			chunks = append(chunks, chunk{start: chunkStart, end: i})
			chunkStart = i + 1
		}
	}
	chunks = append(chunks, chunk{start: chunkStart, end: len(ordered) - 1})

	// Return the longest chunk
	best := chunks[0]
	bestLen := best.end - best.start + 1
	for _, c := range chunks[1:] {
		l := c.end - c.start + 1
		if l > bestLen {
			best = c
			bestLen = l
		}
	}

	return ordered[best.start : best.end+1]
}

// SplitAtStraightGaps splits an ordered slice of ways at contiguous runs
// of zero-score (tier 0) segments exceeding the threshold distance.
// Returns one or more sub-slices of segments, each representing a
// contiguous section of the road. Straight segments that form the gap are
// excluded from both groups (dropped at the split point).
func SplitAtStraightGaps(ways []ScoredWay, thresholdM float64) [][]ScoredSegment {
	// Flatten all segments from all ways in order, ensuring each segment
	// carries its parent way's ID (segments created outside ScoreWay, such as
	// in tests, may have WayID == 0).
	var all []ScoredSegment
	for _, w := range ways {
		for _, seg := range w.Segments {
			if seg.WayID == 0 {
				seg.WayID = w.WayID
			}
			all = append(all, seg)
		}
	}
	if len(all) == 0 {
		return nil
	}

	var groups [][]ScoredSegment
	current := make([]ScoredSegment, 0)

	i := 0
	for i < len(all) {
		seg := all[i]
		if seg.Tier != 0 {
			current = append(current, seg)
			i++
			continue
		}
		// Accumulate a run of tier-0 segments.
		runLen := 0.0
		j := i
		for j < len(all) && all[j].Tier == 0 {
			runLen += all[j].Length
			j++
		}
		if runLen > thresholdM {
			// Split: save current group (if non-empty) and start a new one.
			if len(current) > 0 {
				groups = append(groups, current)
				current = make([]ScoredSegment, 0)
			}
			// Skip the straight run entirely.
			i = j
		} else {
			// Short straight run — include all its segments in the current group.
			for k := i; k < j; k++ {
				current = append(current, all[k])
			}
			i = j
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	// If nothing was accumulated (e.g. all segments were straight), return a
	// single empty-segment group so callers always get at least one entry.
	if len(groups) == 0 {
		groups = append(groups, []ScoredSegment{})
	}

	return groups
}

// Aggregate processes all scored ways into road collections.
// This is the main entry point for stage 5.
func Aggregate(ways []ScoredWay) []RoadCollection {
	if len(ways) == 0 {
		return nil
	}

	nameGroups := GroupWays(ways)

	var collections []RoadCollection

	for name, namedWays := range nameGroups {
		components := FindConnectedComponents(namedWays, ConnectedEndpointProximityM)

		var nameCollections []RoadCollection

		// Build a way-ID → ScoredWay lookup for tag resolution.
		wayByID := make(map[int64]ScoredWay, len(namedWays))
		for _, w := range namedWays {
			wayByID[w.WayID] = w
		}

		for _, component := range components {
			ordered := OrderWays(component)
			ordered = SplitOrderingGaps(ordered, ConnectedEndpointProximityM)

			// Apply deflection filter on the full assembled chain before splitting.
			// This gives the 2400m look-ahead window cross-way-boundary visibility.
			flatSegs := FlattenWaySegments(ordered)
			DeflectionFilterSegments(flatSegs)
			UnflattenWaySegments(ordered, flatSegs)

			segGroups := SplitAtStraightGaps(ordered, StraightGapSplitM)

			for _, segs := range segGroups {
				rc := buildRoadCollection(name, wayByID, segs)
				nameCollections = append(nameCollections, rc)
			}
		}

		// Assign sub-indices.
		for i := range nameCollections {
			nameCollections[i].SubIndex = i
		}

		collections = append(collections, nameCollections...)
	}

	return collections
}

// buildRoadCollection constructs a RoadCollection from a segment group.
// wayByID is a map of way ID to ScoredWay used to look up tags for ways that
// contributed segments to segs. Only ways that appear in segs are included.
func buildRoadCollection(name string, wayByID map[int64]ScoredWay, segs []ScoredSegment) RoadCollection {
	rc := RoadCollection{
		Name:     name,
		Segments: segs,
	}

	// Derive WayIDs and HighwayTypes only from the ways whose segments appear
	// in this collection, preserving first-seen order.
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

	// Compute aggregate scores from this collection's segments.
	for _, seg := range segs {
		rc.TotalScore += seg.Score
		rc.TotalLength += seg.Length
	}
	if rc.TotalLength > 0 {
		rc.ScorePerKm = rc.TotalScore / (rc.TotalLength / 1000.0)
	}

	return rc
}

// DeepCopyWays returns a deep copy of the given ways, with each way's Segments
// slice freshly allocated. This prevents races when multiple goroutines process
// groups whose ways share segment slice backing arrays (e.g. when the same
// input map is passed to concurrent processNameGroup calls in tests).
func DeepCopyWays(ways []ScoredWay) []ScoredWay {
	result := make([]ScoredWay, len(ways))
	for i, w := range ways {
		segs := make([]ScoredSegment, len(w.Segments))
		copy(segs, w.Segments)
		result[i] = ScoredWay{
			WayID:    w.WayID,
			Tags:     w.Tags,
			Segments: segs,
		}
	}
	return result
}

// FlattenWaySegments returns all segments from ordered ways as a single slice.
// Each segment's WayID is set from its parent way if not already set.
func FlattenWaySegments(ways []ScoredWay) []ScoredSegment {
	var all []ScoredSegment
	for _, w := range ways {
		for _, seg := range w.Segments {
			if seg.WayID == 0 {
				seg.WayID = w.WayID
			}
			all = append(all, seg)
		}
	}
	return all
}

// UnflattenWaySegments writes a flat segment slice back into the ordered ways,
// preserving the original segment count per way.
func UnflattenWaySegments(ways []ScoredWay, flat []ScoredSegment) {
	idx := 0
	for i := range ways {
		for j := range ways[i].Segments {
			ways[i].Segments[j] = flat[idx]
			idx++
		}
	}
}

// reverseWay returns a copy of the ScoredWay with its segments reversed
// (and each segment's Start/End swapped).
func reverseWay(w ScoredWay) ScoredWay {
	n := len(w.Segments)
	segs := make([]ScoredSegment, n)
	for i, seg := range w.Segments {
		rev := seg
		rev.Start, rev.End = seg.End, seg.Start
		segs[n-1-i] = rev
	}
	return ScoredWay{
		WayID:    w.WayID,
		Tags:     w.Tags,
		Segments: segs,
	}
}
