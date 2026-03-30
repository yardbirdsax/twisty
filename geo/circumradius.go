package geo

import "math"

// Circumradius returns the radius (in meters) of the unique circle passing
// through three geographic points. If the points are collinear (or nearly so),
// it returns +Inf.
func Circumradius(a, b, c Coord) float64 {
	sideA := Haversine(b, c)
	sideB := Haversine(a, c)
	sideC := Haversine(a, b)

	s := (sideA + sideB + sideC) / 2
	areaSquared := s * (s - sideA) * (s - sideB) * (s - sideC)
	// Guard against floating-point rounding errors on collinear or near-collinear points.
	if areaSquared < 0 {
		areaSquared = 0
	}
	area := math.Sqrt(areaSquared)

	if area < 1e-10 {
		return math.Inf(1)
	}

	return (sideA * sideB * sideC) / (4 * area)
}
