package quality

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapToGrid(t *testing.T) {
	tileSize := 0.05

	tests := []struct {
		name     string
		coord    float64
		expected float64
	}{
		{"positive value snaps down", 42.37, 42.35},
		{"negative value snaps down", -72.58, -72.60},
		{"exact boundary stays", 42.35, 42.35},
		{"exact negative boundary stays", -72.60, -72.60},
		{"zero stays", 0.0, 0.0},
		{"just above boundary snaps to boundary", 42.350001, 42.35},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := snapToGrid(tc.coord, tileSize)
			if math.Abs(got-tc.expected) > 1e-9 {
				t.Errorf("snapToGrid(%v, %v) = %v, want %v", tc.coord, tileSize, got, tc.expected)
			}
		})
	}
}

func TestComputeTilesSingleTile(t *testing.T) {
	// A very small radius (0.01 km) centered well inside a tile should produce exactly one tile.
	// Center at 42.36, -72.58; 0.01 km radius ~ 0.00009 degrees, fits inside a 0.05 tile.
	tiles := ComputeTiles(42.36, -72.58, 0.01, 0.05)
	if len(tiles) != 1 {
		t.Errorf("expected 1 tile, got %d", len(tiles))
	}
}

func TestComputeTilesMultipleTiles(t *testing.T) {
	// Center at 42.36, -72.58, 5 km radius, 0.05 degree tiles.
	// latOffset = 5/111.32 ≈ 0.0449 degrees
	// lonOffset = 5/(111.32 * cos(42.36°)) ≈ 0.0607 degrees
	// bounding box spans ~0.09 lat x ~0.12 lon, so we expect multiple tiles.
	tiles := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	if len(tiles) != 9 {
		t.Errorf("expected 9 tiles for 5km radius, got %d", len(tiles))
	}

	// Verify all tiles are properly sized.
	for i, tile := range tiles {
		if math.Abs((tile.North-tile.South)-0.05) > 1e-9 {
			t.Errorf("tile %d: height = %v, want 0.05", i, tile.North-tile.South)
		}
		if math.Abs((tile.East-tile.West)-0.05) > 1e-9 {
			t.Errorf("tile %d: width = %v, want 0.05", i, tile.East-tile.West)
		}
	}

	// Verify tiles are grid-aligned (South and West should be multiples of 0.05).
	// snapToGrid ensures the value is math.Floor(coord/tileSize)*tileSize; the inverse
	// check is: coord mod tileSize should be ~0 (or ~tileSize for negative modulo artifacts).
	for i, tile := range tiles {
		southMod := math.Abs(tile.South - snapToGrid(tile.South, 0.05))
		if southMod > 1e-9 {
			t.Errorf("tile %d: South=%v is not aligned to 0.05 grid (mod=%v)", i, tile.South, southMod)
		}
		westMod := math.Abs(tile.West - snapToGrid(tile.West, 0.05))
		if westMod > 1e-9 {
			t.Errorf("tile %d: West=%v is not aligned to 0.05 grid (mod=%v)", i, tile.West, westMod)
		}
	}
}

func TestComputeTilesDeterministic(t *testing.T) {
	// Same inputs must always produce the same tiles.
	tiles1 := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	tiles2 := ComputeTiles(42.36, -72.58, 5.0, 0.05)

	if len(tiles1) != len(tiles2) {
		t.Fatalf("non-deterministic: call 1 returned %d tiles, call 2 returned %d", len(tiles1), len(tiles2))
	}
	for i := range tiles1 {
		if tiles1[i] != tiles2[i] {
			t.Errorf("tile %d differs between calls: %v vs %v", i, tiles1[i], tiles2[i])
		}
	}
}

func TestComputeTilesShiftedCenter(t *testing.T) {
	// Two calls with slightly shifted centers but overlapping areas should share tiles.
	tiles1 := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	tiles2 := ComputeTiles(42.362, -72.578, 5.0, 0.05)

	set1 := make(map[Tile]bool)
	for _, tile := range tiles1 {
		set1[tile] = true
	}

	shared := 0
	for _, tile := range tiles2 {
		if set1[tile] {
			shared++
		}
	}

	if shared == 0 {
		t.Error("expected overlapping tile sets for nearby centers, but found no shared tiles")
	}
}

var testTile = Tile{South: 42.350, West: -72.600, North: 42.400, East: -72.550}

func newTestCache(t *testing.T) *TileCache {
	t.Helper()
	return &TileCache{Dir: t.TempDir(), Precision: 3}
}

func TestTileCacheWriteAndRead(t *testing.T) {
	c := newTestCache(t)
	data := []byte(`{"elements":[]}`)
	if err := c.Write(testTile, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := c.Read(testTile)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Read returned %q, want %q", got, data)
	}
}

func TestTileCacheHas(t *testing.T) {
	c := newTestCache(t)
	if c.Has(testTile) {
		t.Error("Has returned true before Write")
	}
	if err := c.Write(testTile, []byte(`{}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !c.Has(testTile) {
		t.Error("Has returned false after Write")
	}
}

func TestTileCacheReadUpdatesMtime(t *testing.T) {
	c := newTestCache(t)
	if err := c.Write(testTile, []byte(`{}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	past := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(c.Path(testTile), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if _, err := c.Read(testTile); err != nil {
		t.Fatalf("Read: %v", err)
	}
	info, err := os.Stat(c.Path(testTile))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if time.Since(info.ModTime()) > 5*time.Second {
		t.Errorf("mtime not updated after Read: mtime=%v", info.ModTime())
	}
}

func TestTileCacheAtomicWrite(t *testing.T) {
	c := newTestCache(t)
	want := []byte(`{"elements":[]}`)
	if err := c.Write(testTile, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file found: %s", e.Name())
		}
	}
	if !c.Has(testTile) {
		t.Error("Has returned false after Write")
	}
	got, err := c.Read(testTile)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Read returned %q, want %q", got, want)
	}
}

func TestTileCacheClearAll(t *testing.T) {
	c := newTestCache(t)
	if err := c.Write(testTile, []byte(`{}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := c.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatalf("ReadDir after ClearAll: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir after ClearAll, got %d entries", len(entries))
	}
}

func TestTileCachePurgeOlderThan(t *testing.T) {
	c := newTestCache(t)
	oldTile := Tile{South: 42.300, West: -72.600, North: 42.350, East: -72.550}
	newTile := Tile{South: 42.350, West: -72.600, North: 42.400, East: -72.550}

	for _, tile := range []Tile{oldTile, newTile} {
		if err := c.Write(tile, []byte(`{}`)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(c.Path(oldTile), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	n, err := c.PurgeOlderThan(1 * time.Hour)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 file purged, got %d", n)
	}
	if c.Has(oldTile) {
		t.Error("old tile still exists after purge")
	}
	if !c.Has(newTile) {
		t.Error("new tile was incorrectly purged")
	}
}

func TestTileCacheReadNonExistent(t *testing.T) {
	c := newTestCache(t)
	_, err := c.Read(testTile)
	if err == nil {
		t.Error("expected error reading non-existent tile, got nil")
	}
}

func TestTileCacheKey(t *testing.T) {
	tests := []struct {
		name      string
		tile      Tile
		precision int
		expected  string
	}{
		{
			name:      "positive coordinates",
			tile:      Tile{South: 42.350, West: 10.050, North: 42.400, East: 10.100},
			precision: 3,
			expected:  "tile_42.350_10.050.json",
		},
		{
			name:      "negative west coordinate",
			tile:      Tile{South: 42.350, West: -72.600, North: 42.400, East: -72.550},
			precision: 3,
			expected:  "tile_42.350_-72.600.json",
		},
		{
			name:      "negative south and west",
			tile:      Tile{South: -33.900, West: -70.650, North: -33.850, East: -70.600},
			precision: 3,
			expected:  "tile_-33.900_-70.650.json",
		},
		{
			name:      "higher precision",
			tile:      Tile{South: 42.3500, West: -72.6000, North: 42.4000, East: -72.5500},
			precision: 4,
			expected:  "tile_42.3500_-72.6000.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TileCacheKey(tc.tile, tc.precision)
			if got != tc.expected {
				t.Errorf("TileCacheKey(%v, %d) = %q, want %q", tc.tile, tc.precision, got, tc.expected)
			}
		})
	}
}

