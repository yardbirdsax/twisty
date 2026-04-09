package quality

import (
	"fmt"
	"sort"
	"strings"

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
func GroupWaysByName(ways ScoredWays) map[string]ScoredWays {
	result := make(map[string]ScoredWays)
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
//
// Semicolon-separated ref values (standard OSM tagging for roads with multiple
// route designations, e.g. "US 209;PA 901") are split so the way appears in
// each individual ref group.
func GroupWays(ways ScoredWays) map[string]ScoredWays {
	result := make(map[string]ScoredWays)
	for _, w := range ways {
		if name := w.Tags["name"]; name != "" {
			result[name] = append(result[name], w)
		}
		if ref := w.Tags["ref"]; ref != "" {
			for _, r := range strings.Split(ref, ";") {
				r = strings.TrimSpace(r)
				if r != "" {
					result[r] = append(result[r], w)
				}
			}
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

// WayEndpointsPublic is the exported version of wayEndpoints. It extracts the
// start and end coordinates of a ScoredWay from its first and last segments.
func WayEndpointsPublic(w ScoredWay) (start, end geo.Coord, ok bool) {
	return wayEndpoints(w)
}

// FindConnectedComponents groups ways by endpoint proximity.
// Two ways are connected if any endpoint of one is within proximityM meters
// of any endpoint of the other.
func FindConnectedComponents(ways ScoredWays, proximityM float64) []ScoredWays {
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
	compMap := make(map[int]ScoredWays)
	for i, w := range ways {
		root := find(i)
		compMap[root] = append(compMap[root], w)
	}

	roots := make([]int, 0, len(compMap))
	for k := range compMap {
		roots = append(roots, k)
	}
	sort.Ints(roots)

	components := make([]ScoredWays, 0, len(compMap))
	for _, k := range roots {
		components = append(components, compMap[k])
	}
	return components
}

// isOneway returns true if the way has a "oneway" tag set to "yes".
func isOneway(w ScoredWay) bool {
	return w.Tags["oneway"] == "yes"
}

// wayEnd identifies a way's presence at a coordinate.
type wayEnd struct {
	index   int  // index into the ways slice
	isStart bool // true if this is the way's start endpoint
}

// buildAdjacency builds a map from each endpoint coordinate to the ways that touch it.
func buildAdjacency(ways ScoredWays) map[geo.Coord][]wayEnd {
	adj := make(map[geo.Coord][]wayEnd)
	for i, w := range ways {
		start, end, ok := wayEndpoints(w)
		if !ok {
			continue
		}
		adj[start] = append(adj[start], wayEnd{index: i, isStart: true})
		adj[end] = append(adj[end], wayEnd{index: i, isStart: false})
	}
	return adj
}

// findStartIndex picks a way at a degree-1 node (route terminus) to begin traversal.
// Returns the way index and whether to start from its end (true) or start (false).
// Falls back to index 0, starting from start, if no degree-1 node is found.
//
// When a degree-1 node is at a way's start, we can begin traversal forward
// (startFromEnd=false). When it's at a way's end, we need to reverse the way
// to start from there — but only if the way is not oneway. Oneway degree-1
// ends are the route terminus (where we finish), not where we start, so they
// are skipped when looking for a start point.
func findStartIndex(ways ScoredWays, adj map[geo.Coord][]wayEnd) (index int, startFromEnd bool) {
	type candidate struct {
		coord        geo.Coord
		index        int
		startFromEnd bool
	}
	var candidates []candidate

	for coord, ends := range adj {
		if len(ends) != 1 {
			continue
		}
		we := ends[0]
		w := ways[we.index]
		if we.isStart {
			candidates = append(candidates, candidate{coord: coord, index: we.index, startFromEnd: false})
		} else if !isOneway(w) {
			candidates = append(candidates, candidate{coord: coord, index: we.index, startFromEnd: true})
		}
		// Oneway end — route terminus, not a valid start. Skip.
	}

	if len(candidates) == 0 {
		return 0, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].coord.Lat != candidates[j].coord.Lat {
			return candidates[i].coord.Lat < candidates[j].coord.Lat
		}
		return candidates[i].coord.Lon < candidates[j].coord.Lon
	})

	return candidates[0].index, candidates[0].startFromEnd
}

// wayExitBearing returns the bearing of the last segment of a way.
// Returns 0 if the way has no segments or only zero-length segments.
func wayExitBearing(w ScoredWay) float64 {
	if len(w.Segments) == 0 {
		return 0
	}
	last := w.Segments[len(w.Segments)-1]
	return geo.Bearing(last.Start, last.End)
}

// wayEntryBearing returns the bearing of the first segment of a way.
// Returns 0 if the way has no segments.
func wayEntryBearing(w ScoredWay) float64 {
	if len(w.Segments) == 0 {
		return 0
	}
	first := w.Segments[0]
	return geo.Bearing(first.Start, first.End)
}

// OrderWays arranges ways in a connected component into a continuous path
// using a directed-graph endpoint matching approach that respects oneway
// tags and uses bearing disambiguation to avoid U-turns.
func OrderWays(ways ScoredWays) ScoredWays {
	if len(ways) <= 1 {
		if len(ways) == 0 {
			return nil
		}
		return ways
	}

	adj := buildAdjacency(ways)
	startIdx, startFromEnd := findStartIndex(ways, adj)

	visited := make([]bool, len(ways))
	ordered := make(ScoredWays, 0, len(ways))

	// Prepare the first way
	first := ways[startIdx]
	if startFromEnd && !isOneway(first) {
		first = reverseWay(first)
	}
	ordered = append(ordered, first)
	visited[startIdx] = true

	// Traverse the graph
	for len(ordered) < len(ways) {
		cur := ordered[len(ordered)-1]
		_, chainEnd, ok := wayEndpoints(cur)
		if !ok {
			break
		}

		// Look up candidates at the chain end coordinate
		candidates := adj[chainEnd]
		bestIdx := -1
		bestReverse := false
		bestAngle := 360.0

		// Compute incoming bearing (bearing of current way's last segment)
		incomingBearing := wayExitBearing(cur)

		for _, cand := range candidates {
			if visited[cand.index] {
				continue
			}
			w := ways[cand.index]

			// Determine if we need to reverse and if it's allowed
			needsReverse := !cand.isStart // if chainEnd matches way's end, we need to reverse
			if needsReverse && isOneway(w) {
				continue // can't reverse a oneway
			}

			// Compute the bearing of this candidate
			candidate := w
			if needsReverse {
				candidate = reverseWay(candidate)
			}
			candBearing := wayEntryBearing(candidate)

			angleDiff := geo.AngleDiff(incomingBearing, candBearing)
			if bestIdx == -1 || angleDiff < bestAngle {
				bestIdx = cand.index
				bestReverse = needsReverse
				bestAngle = angleDiff
			}
		}

		if bestIdx == -1 {
			// No connected unvisited way found — break and append remaining
			break
		}

		next := ways[bestIdx]
		if bestReverse {
			next = reverseWay(next)
		}
		ordered = append(ordered, next)
		visited[bestIdx] = true
	}

	// Append any unvisited ways (disconnected subgraph fallback)
	for i, w := range ways {
		if !visited[i] {
			ordered = append(ordered, w)
		}
	}

	return ordered
}

// SplitOrderingGaps detects large gaps in an ordered way chain and returns
// all contiguous sub-chains as a slice of slices. A gap is defined as a
// distance between consecutive ways' shared endpoints exceeding maxGapM
// meters.
//
// This handles cases where OrderWays' greedy nearest-neighbor algorithm
// appends a way that jumps far from the chain end (e.g., overlapping OSM
// ways that retrace already-covered ground). All chunks are returned so that
// no legitimate road segments are discarded.
//
// Returns nil for nil/empty input; returns []ScoredWays{ordered} when there
// are no gaps.
func SplitOrderingGaps(ordered ScoredWays, maxGapM float64) []ScoredWays {
	if len(ordered) == 0 {
		return nil
	}
	if len(ordered) == 1 {
		return []ScoredWays{ordered}
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

	// Return all chunks
	result := make([]ScoredWays, len(chunks))
	for i, c := range chunks {
		result[i] = ordered[c.start : c.end+1]
	}
	return result
}

// SplitAtStraightGaps splits an ordered slice of ways at contiguous runs
// of zero-score (tier 0) segments exceeding the threshold distance.
// Returns one or more sub-slices of segments, each representing a
// contiguous section of the road. Straight segments that form the gap are
// excluded from both groups (dropped at the split point).
func SplitAtStraightGaps(ways ScoredWays, thresholdM float64) [][]ScoredSegment {
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

// GroupKeys returns the grouping keys (name and/or ref) for a scored way.
// Semicolon-separated ref values are split into individual keys.
func GroupKeys(w ScoredWay) []string {
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

// AggregateNameGroup runs the full per-name aggregation pipeline for a single
// road name group: connected-component analysis, ordering, gap splitting,
// deflection filtering, straight-gap splitting, and collection building.
func AggregateNameGroup(name string, namedWays ScoredWays) []RoadCollection {
	components := FindConnectedComponents(namedWays, ConnectedEndpointProximityM)

	var nameCollections []RoadCollection

	// Build a way-ID → ScoredWay lookup for tag resolution.
	wayByID := make(map[int64]ScoredWay, len(namedWays))
	for _, w := range namedWays {
		wayByID[w.WayID] = w
	}

	for _, component := range components {
		ordered := OrderWays(component)
		chunks := SplitOrderingGaps(ordered, ConnectedEndpointProximityM)

		for _, chunk := range chunks {
			// Apply deflection filter on each chunk before splitting.
			// This gives the 2400m look-ahead window cross-way-boundary visibility.
			flatSegs := FlattenWaySegments(chunk)
			DeflectionFilterSegments(flatSegs)
			UnflattenWaySegments(chunk, flatSegs)

			segGroups := SplitAtStraightGaps(chunk, StraightGapSplitM)

			for _, segs := range segGroups {
				rc := BuildRoadCollection(name, wayByID, segs)
				nameCollections = append(nameCollections, rc)
			}
		}
	}

	// Assign sub-indices.
	for i := range nameCollections {
		nameCollections[i].SubIndex = i
	}

	return nameCollections
}

// Aggregate processes all scored ways into road collections.
// This is the main entry point for stage 5.
func Aggregate(ways ScoredWays) []RoadCollection {
	if len(ways) == 0 {
		return nil
	}

	nameGroups := GroupWays(ways)

	var collections []RoadCollection

	for name, namedWays := range nameGroups {
		nameCollections := AggregateNameGroup(name, namedWays)
		collections = append(collections, nameCollections...)
	}

	return collections
}

// BuildRoadCollection constructs a RoadCollection from a segment group.
// wayByID is a map of way ID to ScoredWay used to look up tags for ways that
// contributed segments to segs. Only ways that appear in segs are included.
func BuildRoadCollection(name string, wayByID map[int64]ScoredWay, segs []ScoredSegment) RoadCollection {
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
func DeepCopyWays(ways ScoredWays) ScoredWays {
	result := make(ScoredWays, len(ways))
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
// Each returned segment's WayID is set from its parent way if not already set.
// The input ways are not modified.
func FlattenWaySegments(ways ScoredWays) []ScoredSegment {
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
func UnflattenWaySegments(ways ScoredWays, flat []ScoredSegment) {
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
