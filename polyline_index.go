package main

import (
	"math"

	"github.com/yardbirdsax/twisty/geo"
)

const (
	// indexCellDeg is the grid cell size in degrees (about 111 m of latitude).
	indexCellDeg = 0.001
	// indexMetersPerDegLat matches the Earth radius geo.Haversine uses.
	indexMetersPerDegLat = 111194.9
	// indexPadFactor widens each segment's box beyond the threshold so the
	// flat-earth projection in distPointToSegment can never place a match
	// outside the cells the segment was filed under.
	indexPadFactor = 1.25
	// indexMaxCellsPerSegment bounds the memory one very long segment can take.
	indexMaxCellsPerSegment = 4096
	// indexMaxFiledLat is where longitude padding would have to grow without
	// bound (cos of latitude tends to 0), so segments reaching past it are not filed.
	indexMaxFiledLat = 89.0
	// indexMaxLon is the antimeridian. Haversine wraps across it but grid cells
	// do not, so segments whose padded box reaches it are not filed.
	indexMaxLon = 180.0
)

type cellKey struct{ y, x int32 }

// polylineIndex answers the same question as the linear scan in
// isNearPolyline (see polyline_index_test.go) — is a point within thresholdM of
// a polyline — but files each segment under the grid cells it can reach, so a
// query tests only the few segments near the point instead of the whole route.
// Scoring asks it once per ScoredSegment in every tile, against routes of
// ~18,000 points, so the linear scan dominated CPU.
//
// It must return exactly what the linear scan returns, on any input. Segments it
// cannot file exactly (see unfiled) are checked on every query instead.
type polylineIndex struct {
	poly      [][2]float64
	threshold float64
	cells     map[cellKey][]int32 // segment i runs poly[i] -> poly[i+1]
	// unfiled holds segments that cannot be filed in cells exactly: non-finite
	// or out-of-range coordinates, boxes touching the poles or the antimeridian,
	// and boxes covering too many cells.
	unfiled []int32
}

func newPolylineIndex(polyline [][2]float64, thresholdM float64) *polylineIndex {
	idx := &polylineIndex{poly: polyline, threshold: thresholdM, cells: map[cellKey][]int32{}}
	latPad := thresholdM * indexPadFactor / indexMetersPerDegLat
	for i := 0; i+1 < len(polyline); i++ {
		a, b := polyline[i], polyline[i+1]
		if !isFinite(a[0], a[1], b[0], b[1]) {
			idx.unfiled = append(idx.unfiled, int32(i))
			continue
		}
		minLat, maxLat := math.Min(a[0], b[0])-latPad, math.Max(a[0], b[0])+latPad
		if minLat < -indexMaxFiledLat || maxLat > indexMaxFiledLat {
			idx.unfiled = append(idx.unfiled, int32(i))
			continue
		}
		// Degrees of longitude shrink toward the poles, so pad by the worst case in range.
		cosLat := math.Cos(math.Max(math.Abs(minLat), math.Abs(maxLat)) * math.Pi / 180)
		lonPad := thresholdM * indexPadFactor / (indexMetersPerDegLat * cosLat)
		minLon, maxLon := math.Min(a[1], b[1])-lonPad, math.Max(a[1], b[1])+lonPad
		if minLon < -indexMaxLon || maxLon > indexMaxLon {
			idx.unfiled = append(idx.unfiled, int32(i))
			continue
		}
		// Counted in floats first: the bounds are now within +-90/+-180, so the
		// cell indexes below fit in int32, but a huge box must not be walked.
		cellCount := ((maxLat-minLat)/indexCellDeg + 2) * ((maxLon-minLon)/indexCellDeg + 2)
		if cellCount > indexMaxCellsPerSegment {
			idx.unfiled = append(idx.unfiled, int32(i))
			continue
		}
		for y := cellIndex(minLat); y <= cellIndex(maxLat); y++ {
			for x := cellIndex(minLon); x <= cellIndex(maxLon); x++ {
				k := cellKey{y, x}
				idx.cells[k] = append(idx.cells[k], int32(i))
			}
		}
	}
	return idx
}

func cellIndex(deg float64) int32 { return int32(math.Floor(deg / indexCellDeg)) }

func isFinite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Near reports whether p is within the threshold of the polyline.
func (idx *polylineIndex) Near(p geo.Coord) bool {
	if len(idx.poly) == 0 {
		return false
	}
	// The linear scan tests the first vertex on its own as well; keep the same
	// check so the two never diverge.
	if geo.Haversine(p, geo.Coord{Lat: idx.poly[0][0], Lon: idx.poly[0][1]}) <= idx.threshold {
		return true
	}
	if isFinite(p.Lat, p.Lon) {
		for _, i := range idx.cells[cellKey{cellIndex(p.Lat), cellIndex(p.Lon)}] {
			if idx.segmentNear(p, i) {
				return true
			}
		}
	}
	for _, i := range idx.unfiled {
		if idx.segmentNear(p, i) {
			return true
		}
	}
	return false
}

func (idx *polylineIndex) segmentNear(p geo.Coord, i int32) bool {
	a := geo.Coord{Lat: idx.poly[i][0], Lon: idx.poly[i][1]}
	b := geo.Coord{Lat: idx.poly[i+1][0], Lon: idx.poly[i+1][1]}
	return distPointToSegment(p, a, b) <= idx.threshold
}
