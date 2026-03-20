package geo

import "math"

// Coord represents a geographic coordinate.
type Coord struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Haversine returns the great-circle distance in meters between two coordinates.
func Haversine(a, b Coord) float64 {
	const R = 6371000
	dLat := (b.Lat - a.Lat) * math.Pi / 180
	dLon := (b.Lon - a.Lon) * math.Pi / 180
	sinDLat := math.Sin(dLat / 2)
	sinDLon := math.Sin(dLon / 2)
	h := sinDLat*sinDLat + math.Cos(a.Lat*math.Pi/180)*math.Cos(b.Lat*math.Pi/180)*sinDLon*sinDLon
	return 2 * R * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}

// Bearing returns the initial bearing in degrees [0, 360) from a to b.
func Bearing(a, b Coord) float64 {
	dLon := (b.Lon - a.Lon) * math.Pi / 180
	y := math.Sin(dLon) * math.Cos(b.Lat*math.Pi/180)
	x := math.Cos(a.Lat*math.Pi/180)*math.Sin(b.Lat*math.Pi/180) -
		math.Sin(a.Lat*math.Pi/180)*math.Cos(b.Lat*math.Pi/180)*math.Cos(dLon)
	bearing := math.Atan2(y, x) * 180 / math.Pi
	return math.Mod(bearing+360, 360)
}

// DecodePolyline decodes a Google Encoded Polyline string into a slice of Coord.
// precision is the divisor used to recover floating-point values; use 1e5 for
// standard Google/OSRM encoding and 1e6 for Valhalla encoding.
func DecodePolyline(encoded string, precision float64) []Coord {
	coords := make([]Coord, 0)
	i := 0
	latAcc := 0
	lonAcc := 0
	for i < len(encoded) {
		lonDecoded := false
		for idx, acc := range []*int{&latAcc, &lonAcc} {
			if i >= len(encoded) {
				break
			}
			result := 0
			shift := 0
			for i < len(encoded) {
				b := int(encoded[i]) - 63
				i++
				result |= (b & 0x1F) << shift
				shift += 5
				if b < 0x20 {
					break
				}
			}
			if result&1 != 0 {
				result = ^result
			}
			*acc += result >> 1
			if idx == 1 {
				lonDecoded = true
			}
		}
		if lonDecoded {
			coords = append(coords, Coord{Lat: float64(latAcc) / precision, Lon: float64(lonAcc) / precision})
		}
	}
	return coords
}

// DestinationPoint returns the point that is distM meters from origin along bearingDeg.
// bearingDeg follows the compass convention (0 = north, 90 = east).
// Uses the spherical-earth destination formula with the same earth radius as Haversine.
func DestinationPoint(origin Coord, bearingDeg, distM float64) Coord {
	const R = 6371000.0
	δ := distM / R
	θ := bearingDeg * math.Pi / 180
	φ1 := origin.Lat * math.Pi / 180
	λ1 := origin.Lon * math.Pi / 180

	φ2 := math.Asin(math.Sin(φ1)*math.Cos(δ) + math.Cos(φ1)*math.Sin(δ)*math.Cos(θ))
	λ2 := λ1 + math.Atan2(
		math.Sin(θ)*math.Sin(δ)*math.Cos(φ1),
		math.Cos(δ)-math.Sin(φ1)*math.Sin(φ2),
	)
	return Coord{
		Lat: φ2 * 180 / math.Pi,
		Lon: λ2 * 180 / math.Pi,
	}
}

// AngleDiff returns the absolute difference between two bearings, handling
// 360°/0° wraparound. Result is in [0, 180].
func AngleDiff(a, b float64) float64 {
	diff := b - a
	for diff > 180 {
		diff -= 360
	}
	for diff < -180 {
		diff += 360
	}
	return math.Abs(diff)
}
