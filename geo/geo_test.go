package geo

import (
	"math"
	"testing"
)

func TestDecodePolyline_Empty(t *testing.T) {
	got := DecodePolyline("", 1e5)
	if got == nil {
		t.Error("DecodePolyline(\"\") returned nil, want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("DecodePolyline(\"\") = %v, want empty slice", got)
	}
}

func TestDecodePolyline_CanonicalGoogleExample(t *testing.T) {
	encoded := "_p~iF~ps|U_ulLnnqC_mqNvxq`@"
	want := []Coord{
		{Lat: 38.5, Lon: -120.2},
		{Lat: 40.7, Lon: -120.95},
		{Lat: 43.252, Lon: -126.453},
	}
	got := DecodePolyline(encoded, 1e5)
	if len(got) != len(want) {
		t.Fatalf("DecodePolyline canonical: got %d coords, want %d", len(got), len(want))
	}
	const epsilon = 1e-5
	for idx, w := range want {
		if math.Abs(got[idx].Lat-w.Lat) > epsilon || math.Abs(got[idx].Lon-w.Lon) > epsilon {
			t.Errorf("DecodePolyline canonical[%d] = %v, want %v", idx, got[idx], w)
		}
	}
}

func TestDecodePolyline_SinglePoint(t *testing.T) {
	// Encode (0.0, 0.0): both lat and lon encode to just "??" (0x3F 0x3F) but
	// the standard encoding for 0 is a single '?' byte per component.
	// Encoded form of (0,0) in Google polyline is "??"
	got := DecodePolyline("??", 1e5)
	if len(got) != 1 {
		t.Fatalf("DecodePolyline single point: got %d coords, want 1", len(got))
	}
	if got[0].Lat != 0 || got[0].Lon != 0 {
		t.Errorf("DecodePolyline single point = %v, want {0 0}", got[0])
	}
}

func TestDecodePolyline_MalformedNoPanic(t *testing.T) {
	// Should not panic on truncated/malformed input.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("DecodePolyline panicked on malformed input: %v", r)
		}
	}()
	DecodePolyline("_p~iF~ps|", 1e5)
}

func TestDecodePolyline_TruncatedAfterLat(t *testing.T) {
	// "_p~iF" encodes the lat portion of the first coord in the canonical
	// Google example (38.5). Truncating there means no lon bytes follow, so
	// no coord should be appended — the function must not emit a spurious
	// coord using a stale lon accumulator.
	got := DecodePolyline("_p~iF", 1e5)
	if len(got) != 0 {
		t.Errorf("DecodePolyline truncated after lat: got %d coords, want 0", len(got))
	}
}

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
