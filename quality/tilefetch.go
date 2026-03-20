package quality

import (
	"fmt"
	"math"
)

// Tile represents a rectangular geographic tile identified by its (South, West) corner.
type Tile struct {
	South float64
	West  float64
	North float64
	East  float64
}

// snapToGrid snaps a coordinate down to the nearest tile boundary.
func snapToGrid(coord, tileSize float64) float64 {
	return math.Floor(coord/tileSize) * tileSize
}

// ComputeTiles converts a center point and radius into a deterministic set of tile
// coordinates aligned to a global grid. tileSizeDeg is the size of each tile in degrees.
func ComputeTiles(centerLat, centerLon, radiusKm, tileSizeDeg float64) []Tile {
	latOffset := radiusKm / 111.32
	lonOffset := radiusKm / (111.32 * math.Cos(centerLat*math.Pi/180.0))

	south := centerLat - latOffset
	north := centerLat + latOffset
	west := centerLon - lonOffset
	east := centerLon + lonOffset

	gridSouth := snapToGrid(south, tileSizeDeg)
	gridWest := snapToGrid(west, tileSizeDeg)

	// Use index-based iteration to avoid floating-point accumulation errors.
	southIdx := int(math.Round(gridSouth / tileSizeDeg))
	westIdx := int(math.Round(gridWest / tileSizeDeg))

	// Count how many steps are needed in each direction.
	latSteps := 0
	for float64(southIdx+latSteps)*tileSizeDeg < north {
		latSteps++
	}
	lonSteps := 0
	for float64(westIdx+lonSteps)*tileSizeDeg < east {
		lonSteps++
	}

	var tiles []Tile
	for latI := 0; latI < latSteps; latI++ {
		s := float64(southIdx+latI) * tileSizeDeg
		for lonI := 0; lonI < lonSteps; lonI++ {
			w := float64(westIdx+lonI) * tileSizeDeg
			tiles = append(tiles, Tile{
				South: s,
				West:  w,
				North: s + tileSizeDeg,
				East:  w + tileSizeDeg,
			})
		}
	}
	return tiles
}

// TileCacheKey formats a tile's (South, West) corner into a deterministic string
// suitable for use as a file name. precision controls decimal places (default 3).
func TileCacheKey(t Tile, precision int) string {
	format := fmt.Sprintf("%%.%df", precision)
	south := fmt.Sprintf(format, t.South)
	west := fmt.Sprintf(format, t.West)
	return fmt.Sprintf("tile_%s_%s.json", south, west)
}
