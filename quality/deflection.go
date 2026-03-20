package quality

import "github.com/yardbirdsax/twisty/geo"

// DeflectionFilter zeroes out curvature scores for segments that are minor
// heading deviations in an otherwise straight road. It modifies the scored
// segments in place.
//
// The algorithm uses a sliding look-ahead window of DeflectionLookAheadM
// (2400 m, from the Curvature project). For each segment with a non-zero
// score, it accumulates segments forward until the cumulative distance reaches
// 2400 m or the end of the way. If the overall bearing change across the window
// is less than DeflectionMinHeadingChange (20°, from the Curvature project),
// all segments in the window are zeroed out.
func DeflectionFilter(sw *ScoredWay) {
	segs := sw.Segments
	n := len(segs)
	i := 0
	for i < n {
		seg := segs[i]
		if seg.Score == 0 {
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
		// windowEnd is exclusive: window covers segs[i..windowEnd-1].
		last := windowEnd - 1

		startBearing := geo.Bearing(segs[i].Start, segs[i].End)
		endBearing := geo.Bearing(segs[last].Start, segs[last].End)
		bearingChange := geo.AngleDiff(startBearing, endBearing)

		if bearingChange < DeflectionMinHeadingChange {
			for j := i; j < windowEnd; j++ {
				segs[j].Score = 0
				segs[j].Tier = 0
				segs[j].Weight = 0
			}
			// Advance past the zeroed window to avoid double-processing.
			i = windowEnd
		} else {
			// Window passed: advance one segment so interior scored
			// segments can anchor their own look-ahead evaluation.
			i++
		}
	}
}

// ApplyDeflectionFilter applies the deflection filter to all scored ways.
func ApplyDeflectionFilter(ways []ScoredWay) {
	for i := range ways {
		DeflectionFilter(&ways[i])
	}
}
