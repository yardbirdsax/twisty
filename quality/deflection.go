package quality

import "github.com/yardbirdsax/twisty/geo"

// DeflectionFilterSegments zeroes out curvature scores for segments that are
// minor heading deviations in an otherwise straight section of road. It
// modifies the segments in place.
//
// This function is intended to be called on the assembled, ordered segment
// chain of a road (after connected-component analysis and way ordering) so
// that the 2400 m look-ahead window can see across OSM way boundaries.
func DeflectionFilterSegments(segs []ScoredSegment) {
	n := len(segs)
	i := 0
	for i < n {
		if segs[i].Score == 0 {
			i++
			continue
		}

		// Build look-ahead window starting at i.
		windowEnd := i
		cumDist := 0.0
		for windowEnd < n && cumDist < DeflectionLookAheadM {
			cumDist += segs[windowEnd].Length
			windowEnd++
		}

		// Compute cumulative heading change across the window.
		cumBearingChange := 0.0
		for j := i; j < windowEnd-1; j++ {
			bj := geo.Bearing(segs[j].Start, segs[j].End)
			bk := geo.Bearing(segs[j+1].Start, segs[j+1].End)
			cumBearingChange += geo.AngleDiff(bj, bk)
		}

		if cumBearingChange < DeflectionMinHeadingChange {
			for j := i; j < windowEnd; j++ {
				segs[j].Score = 0
				segs[j].Tier = 0
				segs[j].Weight = 0
			}
			i = windowEnd
		} else {
			i++
		}
	}
}
