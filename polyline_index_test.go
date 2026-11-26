package main

import (
	"math"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/geo"
)

// isNearPolyline is the linear-scan reference for polylineIndex.Near: it checks
// every segment, so it is only suitable for small inputs. The index must agree
// with it on every input.
func isNearPolyline(point geo.Coord, polyline [][2]float64, thresholdM float64) bool {
	if len(polyline) == 0 {
		return false
	}
	// Check distance to first vertex.
	if geo.Haversine(point, geo.Coord{Lat: polyline[0][0], Lon: polyline[0][1]}) <= thresholdM {
		return true
	}
	// Check each segment.
	for i := 1; i < len(polyline); i++ {
		a := geo.Coord{Lat: polyline[i-1][0], Lon: polyline[i-1][1]}
		b := geo.Coord{Lat: polyline[i][0], Lon: polyline[i][1]}
		if distPointToSegment(point, a, b) <= thresholdM {
			return true
		}
	}
	return false
}

// offsetM moves c by north/east meters using the same local approximation the
// tests' expected values are worked out with.
func offsetM(c geo.Coord, northM, eastM float64) geo.Coord {
	return geo.Coord{
		Lat: c.Lat + northM/indexMetersPerDegLat,
		Lon: c.Lon + eastM/(indexMetersPerDegLat*math.Cos(c.Lat*math.Pi/180)),
	}
}

func coordsToPolyline(cs []geo.Coord) [][2]float64 {
	out := make([][2]float64, len(cs))
	for i, c := range cs {
		out[i] = [2]float64{c.Lat, c.Lon}
	}
	return out
}

func TestPolylineIndex_Near(t *testing.T) {
	// An 850 m east-west line at 40°N.
	start := geo.Coord{Lat: 40.0, Lon: -75.5}
	end := offsetM(start, 0, 850)
	line := coordsToPolyline([]geo.Coord{start, offsetM(start, 0, 400), end})

	tests := []struct {
		name string
		poly [][2]float64
		p    geo.Coord
		want bool
	}{
		{"on the line", line, offsetM(start, 0, 200), true},
		{"40 m beside the line", line, offsetM(start, 40, 600), true},
		{"60 m beside the line", line, offsetM(start, 60, 600), false},
		{"40 m past the end", line, offsetM(end, 0, 40), true},
		{"60 m past the end", line, offsetM(end, 0, 60), false},
		{"40 m before the start", line, offsetM(start, -10, -40), true},
		{"far away", line, geo.Coord{Lat: 41.0, Lon: -74.0}, false},
		{"single vertex, 10 m away", coordsToPolyline([]geo.Coord{start}), offsetM(start, 10, 0), true},
		{"single vertex, 100 m away", coordsToPolyline([]geo.Coord{start}), offsetM(start, 100, 0), false},
		{"empty polyline", nil, start, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newPolylineIndex(tt.poly, 50).Near(tt.p); got != tt.want {
				t.Errorf("Near = %v, want %v", got, tt.want)
			}
		})
	}
}

// The index is an optimization of isNearPolyline, so on any input it must give
// the same answer. Run it across latitudes (the longitude padding depends on
// them) with random-walk routes and query points on both sides of the 50 m line.
func TestPolylineIndex_agreesWithLinearScan(t *testing.T) {
	for _, lat := range []float64{0, 40, 60} {
		t.Run("lat "+strconv.Itoa(int(lat)), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(lat) + 1))
			pos := geo.Coord{Lat: lat, Lon: -75.5}
			poly := []geo.Coord{pos}
			heading := rng.Float64() * 2 * math.Pi
			for range 400 {
				heading += (rng.Float64() - 0.5) * 1.2
				step := 20 + rng.Float64()*60
				pos = offsetM(pos, step*math.Cos(heading), step*math.Sin(heading))
				poly = append(poly, pos)
			}
			polyline := coordsToPolyline(poly)
			idx := newPolylineIndex(polyline, 50)

			var near, far int
			for range 5000 {
				anchor := poly[rng.Intn(len(poly))]
				q := offsetM(anchor, (rng.Float64()-0.5)*240, (rng.Float64()-0.5)*240)
				want := isNearPolyline(q, polyline, 50)
				if got := idx.Near(q); got != want {
					t.Fatalf("Near(%v) = %v, linear scan says %v", q, got, want)
				}
				if want {
					near++
				} else {
					far++
				}
			}
			if near < 200 || far < 200 {
				t.Fatalf("test is vacuous: %d near / %d far", near, far)
			}
		})
	}
}

func TestPolylineIndex_longSegmentSpanningManyCells(t *testing.T) {
	a := geo.Coord{Lat: 40.0, Lon: -75.5}
	b := offsetM(a, 3000, 4000) // a single 5 km diagonal segment
	polyline := coordsToPolyline([]geo.Coord{a, b})
	idx := newPolylineIndex(polyline, 50)

	for i := 0; i <= 100; i++ {
		f := float64(i) / 100
		onLine := geo.Coord{Lat: a.Lat + (b.Lat-a.Lat)*f, Lon: a.Lon + (b.Lon-a.Lon)*f}
		for _, q := range []geo.Coord{onLine, offsetM(onLine, 30, 0), offsetM(onLine, 0, -70), offsetM(onLine, 200, 200)} {
			if got, want := idx.Near(q), isNearPolyline(q, polyline, 50); got != want {
				t.Fatalf("Near(%v) = %v, linear scan says %v", q, got, want)
			}
		}
	}
}

// A segment too long to file in grid cells takes the always-check path; it must
// still match exactly like the linear scan.
func TestPolylineIndex_veryLongSegmentFallsBackToChecking(t *testing.T) {
	a := geo.Coord{Lat: 40.0, Lon: -75.5}
	b := offsetM(a, 30000, 40000) // a 50 km segment: far more cells than the index will file
	polyline := coordsToPolyline([]geo.Coord{a, b})
	idx := newPolylineIndex(polyline, 50)
	// Asserts on an internal field on purpose: without it this test would pass
	// even if the always-check path were never exercised.
	if len(idx.unfiled) != 1 {
		t.Fatalf("expected the segment to take the unfiled path, got %d unfiled segments", len(idx.unfiled))
	}

	for i := 0; i <= 50; i++ {
		f := float64(i) / 50
		onLine := geo.Coord{Lat: a.Lat + (b.Lat-a.Lat)*f, Lon: a.Lon + (b.Lon-a.Lon)*f}
		for _, q := range []geo.Coord{onLine, offsetM(onLine, 30, 0), offsetM(onLine, 0, -70)} {
			if got, want := idx.Near(q), isNearPolyline(q, polyline, 50); got != want {
				t.Fatalf("Near(%v) = %v, linear scan says %v", q, got, want)
			}
		}
	}
}

// The grid pads each segment's box by the threshold; near the antimeridian a
// match can lie on the other side of the ±180° seam, where Haversine wraps but
// grid cells do not.
func TestPolylineIndex_matchesAcrossTheAntimeridian(t *testing.T) {
	polyline := [][2]float64{{0, -179.9999}, {0.001, -179.9999}}
	idx := newPolylineIndex(polyline, 50)
	q := geo.Coord{Lat: 0.0005, Lon: 179.9999} // about 22 m away across the seam

	if !isNearPolyline(q, polyline, 50) {
		t.Fatal("test premise broken: the linear scan should match across the seam")
	}
	if !idx.Near(q) {
		t.Error("Near must match across the antimeridian like the linear scan")
	}
}

// Above ~89° the longitude padding would need to be enormous; those segments
// must still match exactly like the linear scan.
func TestPolylineIndex_agreesNearThePoles(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	pos := geo.Coord{Lat: 89.9, Lon: 10}
	poly := []geo.Coord{pos}
	for range 60 {
		pos = offsetM(pos, (rng.Float64()-0.5)*40, (rng.Float64()-0.5)*40)
		poly = append(poly, pos)
	}
	polyline := coordsToPolyline(poly)
	idx := newPolylineIndex(polyline, 50)

	var near int
	for range 2000 {
		anchor := poly[rng.Intn(len(poly))]
		q := geo.Coord{Lat: anchor.Lat + (rng.Float64()-0.5)*0.002, Lon: anchor.Lon + (rng.Float64()-0.5)*0.05}
		want := isNearPolyline(q, polyline, 50)
		if got := idx.Near(q); got != want {
			t.Fatalf("Near(%v) = %v, linear scan says %v", q, got, want)
		}
		if want {
			near++
		}
	}
	if near == 0 {
		t.Fatal("test is vacuous: no query matched")
	}
}

// /api/score does not validate coordinates, so the index must terminate and
// agree with the linear scan on garbage rather than hang the server.
func TestPolylineIndex_terminatesOnOutOfRangeCoordinates(t *testing.T) {
	inf := math.Inf(1)
	polylines := map[string][][2]float64{
		"huge latitude":    {{3e6, 0}, {3e6, 0.0001}},
		"huge longitude":   {{0, 3e6}, {0.0001, 3e6}},
		"negative huge":    {{-3e6, -3e6}, {-3e6, -3e6 + 0.0001}},
		"infinite":         {{inf, 0}, {inf, 1}},
		"NaN":              {{math.NaN(), 0}, {0, math.NaN()}},
		"beyond the pole":  {{95, 10}, {95.001, 10}},
		"mixed sane/awful": {{40, -75}, {3e6, -75}, {40.001, -75}},
	}
	queries := []geo.Coord{{Lat: 40, Lon: -75}, {Lat: 3e6, Lon: 0}, {Lat: 95, Lon: 10}, {Lat: 0, Lon: 0}}

	for name, polyline := range polylines {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				idx := newPolylineIndex(polyline, 50)
				for _, q := range queries {
					if got, want := idx.Near(q), isNearPolyline(q, polyline, 50); got != want {
						t.Errorf("Near(%v) = %v, linear scan says %v", q, got, want)
					}
				}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("newPolylineIndex/Near did not terminate")
			}
		})
	}
}

func BenchmarkNearPolyline(b *testing.B) {
	rng := rand.New(rand.NewSource(7))
	pos := geo.Coord{Lat: 40.0, Lon: -75.7}
	poly := []geo.Coord{pos}
	heading := 0.0
	for range 18000 { // the route size seen in the logs
		heading += (rng.Float64() - 0.5) * 0.4
		pos = offsetM(pos, 30*math.Cos(heading), 30*math.Sin(heading))
		poly = append(poly, pos)
	}
	polyline := coordsToPolyline(poly)
	queries := make([]geo.Coord, 1000)
	for i := range queries { // mostly far from the route, as in a tile of unrelated roads
		anchor := poly[rng.Intn(len(poly))]
		queries[i] = offsetM(anchor, (rng.Float64()-0.5)*20000, (rng.Float64()-0.5)*20000)
	}

	b.Run("linear", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			isNearPolyline(queries[i%len(queries)], polyline, 50)
		}
	})
	b.Run("index", func(b *testing.B) {
		idx := newPolylineIndex(polyline, 50)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			idx.Near(queries[i%len(queries)])
		}
	})
}
