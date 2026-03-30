package quality

import (
	"math"

	"github.com/yardbirdsax/twisty/geo"
)

// SpatialGrid partitions Way nodes into fixed-size lat/lon cells so that
// NearestWay can restrict its search to the 3×3 neighborhood of any query point.
type SpatialGrid struct {
	cells    map[[2]int][]*Way // keyed by (cellX, cellY)
	cellSize float64           // cell edge length in degrees
}

// cellKey returns the grid cell key for a coordinate.
func (g *SpatialGrid) cellKey(c geo.Coord) [2]int {
	return [2]int{
		int(math.Floor(c.Lon / g.cellSize)),
		int(math.Floor(c.Lat / g.cellSize)),
	}
}

// BuildSpatialGrid indexes all nodes of every way into a grid with cells of
// cellSizeDeg degrees on each side. Recommended value: 0.01 (≈ 1.1 km).
func BuildSpatialGrid(ways []Way, cellSizeDeg float64) *SpatialGrid {
	g := &SpatialGrid{
		cells:    make(map[[2]int][]*Way),
		cellSize: cellSizeDeg,
	}

	for i := range ways {
		w := &ways[i]
		for _, node := range w.Geometry {
			key := g.cellKey(node)
			g.cells[key] = append(g.cells[key], w)
		}
	}

	return g
}

// NearestWayGrid returns the nearest Way within maxDist meters of p, searching
// only the 3×3 cell neighborhood. Returns nil if none is found within maxDist.
func (g *SpatialGrid) NearestWayGrid(p geo.Coord, maxDist float64) *Way {
	centerKey := g.cellKey(p)

	var best *Way
	bestDist := math.MaxFloat64

	// De-duplicate way pointers across cells.
	seen := make(map[*Way]bool)

	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			key := [2]int{centerKey[0] + dx, centerKey[1] + dy}
			cellWays, ok := g.cells[key]
			if !ok {
				continue
			}
			for _, w := range cellWays {
				if seen[w] {
					continue
				}
				seen[w] = true
				for _, node := range w.Geometry {
					d := geo.Haversine(p, node)
					if d < bestDist {
						bestDist = d
						best = w
					}
				}
			}
		}
	}

	if bestDist <= maxDist {
		return best
	}
	return nil
}
