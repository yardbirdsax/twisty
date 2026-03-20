package quality

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
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

// TileCache is a file-based cache for raw Overpass JSON responses, keyed by tile coordinates.
type TileCache struct {
	Dir       string
	Precision int // decimal places for coordinate formatting
}

// EnsureDir creates the cache directory if it does not already exist.
func (c *TileCache) EnsureDir() error {
	return os.MkdirAll(c.Dir, 0o755)
}

// Path returns the full file path for the cache entry of the given tile.
func (c *TileCache) Path(t Tile) string {
	return filepath.Join(c.Dir, TileCacheKey(t, c.Precision))
}

// Has returns true if a cache file exists for the given tile.
func (c *TileCache) Has(t Tile) bool {
	_, err := os.Stat(c.Path(t))
	return err == nil
}

// Read reads the cached bytes for the given tile and updates the file's mtime.
func (c *TileCache) Read(t Tile) ([]byte, error) {
	data, err := os.ReadFile(c.Path(t))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	_ = os.Chtimes(c.Path(t), now, now)
	return data, nil
}

// Write atomically writes data to the cache file for the given tile.
func (c *TileCache) Write(t Tile, data []byte) error {
	target := c.Path(t)
	tmp, err := os.CreateTemp(c.Dir, "tile-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	renamed = true
	return nil
}

// ClearAll removes and recreates the cache directory.
func (c *TileCache) ClearAll() error {
	if err := os.RemoveAll(c.Dir); err != nil {
		return err
	}
	return os.MkdirAll(c.Dir, 0o755)
}

// PurgeOlderThan removes cache files whose mtime is older than the given age.
// It returns the number of files removed.
func (c *TileCache) PurgeOlderThan(age time.Duration) (int, error) {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return count, err
		}
		if time.Since(info.ModTime()) > age {
			if err := os.Remove(filepath.Join(c.Dir, entry.Name())); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

// TileCacheKey formats a tile's (South, West) corner into a deterministic string
// suitable for use as a file name. precision controls decimal places (default 3).
func TileCacheKey(t Tile, precision int) string {
	format := fmt.Sprintf("%%.%df", precision)
	south := fmt.Sprintf(format, t.South)
	west := fmt.Sprintf(format, t.West)
	return fmt.Sprintf("tile_%s_%s.json", south, west)
}
