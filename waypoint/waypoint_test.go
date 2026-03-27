package waypoint

import (
	"math"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

func TestDeriveRadius(t *testing.T) {
	tests := []struct {
		name        string
		timeHours   float64
		avgSpeedMPH float64
		wantKm      float64
		tolKm       float64
	}{
		{
			name:        "2h at 35mph",
			timeHours:   2.0,
			avgSpeedMPH: 35.0,
			wantKm:      17.929,
			tolKm:       0.001,
		},
		{
			name:        "1h at 60mph",
			timeHours:   1.0,
			avgSpeedMPH: 60.0,
			wantKm:      15.368,
			tolKm:       0.001,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveRadius(tc.timeHours, tc.avgSpeedMPH)
			if math.Abs(got-tc.wantKm) > tc.tolKm {
				t.Errorf("DeriveRadius(%v, %v) = %v, want %v ± %v",
					tc.timeHours, tc.avgSpeedMPH, got, tc.wantKm, tc.tolKm)
			}
		})
	}
}

func TestCollectionMidpoint(t *testing.T) {
	t.Run("two segments returns midpoint of middle segment", func(t *testing.T) {
		c := quality.RoadCollection{
			Segments: []quality.ScoredSegment{
				{
					Start: geo.Coord{Lat: 40.0, Lon: -75.0},
					End:   geo.Coord{Lat: 40.1, Lon: -75.1},
				},
				{
					Start: geo.Coord{Lat: 40.1, Lon: -75.1},
					End:   geo.Coord{Lat: 40.2, Lon: -75.2},
				},
			},
		}
		// Middle element is index 1 (len=2, len/2=1)
		got := CollectionMidpoint(c)
		wantLat := (40.1 + 40.2) / 2
		wantLon := (-75.1 + -75.2) / 2
		if math.Abs(got.Lat-wantLat) > 1e-9 || math.Abs(got.Lon-wantLon) > 1e-9 {
			t.Errorf("CollectionMidpoint() = {%v, %v}, want {%v, %v}",
				got.Lat, got.Lon, wantLat, wantLon)
		}
	})

	t.Run("empty segments returns zero coord", func(t *testing.T) {
		c := quality.RoadCollection{}
		got := CollectionMidpoint(c)
		if got.Lat != 0 || got.Lon != 0 {
			t.Errorf("CollectionMidpoint(empty) = %v, want zero coord", got)
		}
	})
}

func TestSortByBearing(t *testing.T) {
	// Origin at 0,0; place three points at approximate bearings 270 (west), 0 (north), 90 (east)
	origin := geo.Coord{Lat: 0, Lon: 0}
	north := geo.Coord{Lat: 1, Lon: 0} // bearing ~0
	east := geo.Coord{Lat: 0, Lon: 1}  // bearing ~90
	west := geo.Coord{Lat: 0, Lon: -1} // bearing ~270

	coords := []geo.Coord{west, east, north}
	SortByBearing(origin, coords)

	// After sorting by ascending bearing: north (~0), east (~90), west (~270)
	expected := []geo.Coord{north, east, west}
	for i, c := range coords {
		if math.Abs(c.Lat-expected[i].Lat) > 1e-9 || math.Abs(c.Lon-expected[i].Lon) > 1e-9 {
			t.Errorf("SortByBearing position %d = {%v, %v}, want {%v, %v}",
				i, c.Lat, c.Lon, expected[i].Lat, expected[i].Lon)
		}
	}
}
