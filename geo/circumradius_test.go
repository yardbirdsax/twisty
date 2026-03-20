package geo

import (
	"math"
	"testing"
)

func TestCircumradius(t *testing.T) {
	// tolerance for relative error checks
	const tol = 0.02

	tests := []struct {
		name           string
		a, b, c        Coord
		wantInf        bool
		want           float64
		wantLowerBound bool
	}{
		{
			// Equilateral triangle near the equator.
			// Side length roughly 1000 m. Circumradius = side / sqrt(3).
			// We place points so each side is approximately 1000 m.
			// Moving ~0.009 degrees lat ≈ 1000 m.
			// For an equilateral triangle in a flat approximation:
			//   A = (0, 0), B = (0.009, 0), C = (0.0045, 0.0078)
			// side AB ≈ 1000 m, AC ≈ 1000 m, BC ≈ 1000 m
			// circumradius = side / sqrt(3) ≈ 577 m
			name:    "equilateral triangle ~1000m side",
			a:       Coord{Lat: 0, Lon: 0},
			b:       Coord{Lat: 0.009, Lon: 0},
			c:       Coord{Lat: 0.0045, Lon: 0.0078},
			wantInf: false,
			want:    1000.0 / math.Sqrt(3),
		},
		{
			// Right triangle: circumradius = hypotenuse / 2.
			// A = (0,0), B = (0,0.009), C = (0.009, 0)
			// AB ≈ 1000 m (east), AC ≈ 1000 m (north), BC = hypotenuse ≈ 1414 m
			// circumradius ≈ 707 m
			name:    "right triangle",
			a:       Coord{Lat: 0, Lon: 0},
			b:       Coord{Lat: 0, Lon: 0.009},
			c:       Coord{Lat: 0.009, Lon: 0},
			wantInf: false,
			want:    Haversine(Coord{Lat: 0, Lon: 0.009}, Coord{Lat: 0.009, Lon: 0}) / 2,
		},
		{
			// Collinear: same longitude, increasing latitude.
			name:    "collinear same longitude",
			a:       Coord{Lat: 0, Lon: 0},
			b:       Coord{Lat: 1, Lon: 0},
			c:       Coord{Lat: 2, Lon: 0},
			wantInf: true,
		},
		{
			// Nearly collinear: very slight deviation.
			// Expect a very large radius (> 1e6 m).
			name:           "nearly collinear",
			a:              Coord{Lat: 0, Lon: 0},
			b:              Coord{Lat: 1, Lon: 0.0001},
			c:              Coord{Lat: 2, Lon: 0},
			wantInf:        false,
			want:           1e6, // just a lower bound; actual result >> 1e6
			wantLowerBound: true,
		},
		{
			// Tight curve: three points forming a small circle.
			// Place points on a circle of radius ~20 m.
			// Using flat-earth approximation: 1 degree lat ≈ 111320 m.
			// 20 m ≈ 0.0001797 degrees.
			// Equilateral triangle inscribed in circle of radius R:
			//   side = R * sqrt(3), so for R=20m, side ≈ 34.6 m
			//   side in degrees ≈ 34.6 / 111320 ≈ 0.000311 degrees
			// A = (0, 0)
			// B = (0.000311, 0)  -- roughly 34.6 m north
			// C = (0.0001555, 0.000269) -- third vertex of equilateral triangle
			name:    "tight curve ~20m radius",
			a:       Coord{Lat: 0, Lon: 0},
			b:       Coord{Lat: 0.000311, Lon: 0},
			c:       Coord{Lat: 0.0001555, Lon: 0.000269},
			wantInf: false,
			want:    20.0,
		},
		{
			// Duplicate points: two identical points — degenerate triangle.
			name:    "duplicate points a==b",
			a:       Coord{Lat: 10, Lon: 20},
			b:       Coord{Lat: 10, Lon: 20},
			c:       Coord{Lat: 11, Lon: 20},
			wantInf: true,
		},
		{
			// All three identical.
			name:    "all three identical",
			a:       Coord{Lat: 5, Lon: 5},
			b:       Coord{Lat: 5, Lon: 5},
			c:       Coord{Lat: 5, Lon: 5},
			wantInf: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Circumradius(tc.a, tc.b, tc.c)
			if tc.wantInf {
				if !math.IsInf(got, 1) {
					t.Errorf("Circumradius(%v, %v, %v) = %v, want +Inf", tc.a, tc.b, tc.c, got)
				}
				return
			}
			// wantLowerBound: just check the result exceeds the threshold.
			if tc.wantLowerBound {
				if got < tc.want {
					t.Errorf("Circumradius nearly collinear = %.0f m, want > %.0f m", got, tc.want)
				}
				return
			}
			relErr := math.Abs(got-tc.want) / tc.want
			if relErr > tol {
				t.Errorf("Circumradius(%v, %v, %v) = %.4f m, want ~%.4f m (rel err %.4f > %.4f)",
					tc.a, tc.b, tc.c, got, tc.want, relErr, tol)
			}
		})
	}
}
