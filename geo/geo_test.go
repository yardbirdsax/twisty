package geo

import (
	"math"
	"testing"
)

func TestHaversine_SFtoLA(t *testing.T) {
	// San Francisco to Los Angeles: ~559 km great-circle distance
	sf := Coord{Lat: 37.7749, Lon: -122.4194}
	la := Coord{Lat: 34.0522, Lon: -118.2437}
	got := Haversine(sf, la)
	const expected = 559_000.0 // meters
	const tolerance = 0.01     // 1%
	if math.Abs(got-expected)/expected > tolerance {
		t.Errorf("Haversine(SF, LA) = %.0f m, want ~%.0f m (within 1%%)", got, expected)
	}
}

func TestHaversine_SamePoint(t *testing.T) {
	c := Coord{Lat: 40.0, Lon: -74.0}
	got := Haversine(c, c)
	if got != 0 {
		t.Errorf("Haversine(same, same) = %v, want 0", got)
	}
}

func TestBearing_Cardinals(t *testing.T) {
	// At the equator, cardinal directions are exact.
	origin := Coord{Lat: 0, Lon: 0}
	tests := []struct {
		name string
		dest Coord
		want float64
	}{
		{"north", Coord{Lat: 1, Lon: 0}, 0},
		{"east", Coord{Lat: 0, Lon: 1}, 90},
		{"south", Coord{Lat: -1, Lon: 0}, 180},
		{"west", Coord{Lat: 0, Lon: -1}, 270},
	}
	const epsilon = 0.001
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Bearing(origin, tc.dest)
			if math.Abs(got-tc.want) > epsilon {
				t.Errorf("Bearing(%v) = %.6f, want %.1f", tc.name, got, tc.want)
			}
		})
	}
}

func TestAngleDiff_Wraparound(t *testing.T) {
	tests := []struct {
		a, b float64
		want float64
	}{
		{350, 10, 20},
		{10, 350, 20},
		{0, 180, 180},
		{180, 0, 180},
		{0, 90, 90},
		{90, 0, 90},
		{45, 45, 0},
		{359, 1, 2},
		{1, 359, 2},
	}
	for _, tc := range tests {
		got := AngleDiff(tc.a, tc.b)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("AngleDiff(%.0f, %.0f) = %.10f, want %.1f", tc.a, tc.b, got, tc.want)
		}
	}
}
