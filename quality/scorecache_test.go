package quality

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

func newTestScoreCache(t *testing.T) *ScoreCache {
	t.Helper()
	dir := t.TempDir()
	return &ScoreCache{Dir: dir, Precision: 3}
}

func sampleScoredWays() []ScoredWay {
	return []ScoredWay{
		{
			WayID: 12345,
			Tags:  map[string]string{"highway": "secondary", "name": "Test Road"},
			Segments: []ScoredSegment{
				{
					Start:  geo.Coord{Lat: 45.1, Lon: -122.5},
					End:    geo.Coord{Lat: 45.2, Lon: -122.6},
					Radius: 50.0,
					Tier:   3,
					Weight: TierWeight3,
					Length: 1000.0,
					Score:  1000.0 * TierWeight3,
				},
				{
					Start:  geo.Coord{Lat: 45.2, Lon: -122.6},
					End:    geo.Coord{Lat: 45.3, Lon: -122.7},
					Radius: math.Inf(1),
					Tier:   0,
					Weight: TierWeight0,
					Length: 500.0,
					Score:  0.0,
				},
			},
		},
	}
}

func sampleTile() Tile {
	return Tile{South: 45.0, West: -123.0, North: 45.05, East: -122.95}
}

func TestScoreCache_WriteAndRead(t *testing.T) {
	c := newTestScoreCache(t)
	tile := sampleTile()
	rawData := []byte(`{"raw":"tile data"}`)
	ways := sampleScoredWays()

	if err := c.Write(tile, rawData, ways, 3); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, zeroed, ok := c.Read(tile, rawData)
	if !ok {
		t.Fatal("Read returned false, expected true")
	}
	if zeroed != 3 {
		t.Errorf("zeroed: got %d, want 3", zeroed)
	}

	if len(got) != len(ways) {
		t.Fatalf("got %d ways, want %d", len(got), len(ways))
	}

	if got[0].WayID != ways[0].WayID {
		t.Errorf("WayID: got %d, want %d", got[0].WayID, ways[0].WayID)
	}

	if len(got[0].Segments) != len(ways[0].Segments) {
		t.Fatalf("segment count: got %d, want %d", len(got[0].Segments), len(ways[0].Segments))
	}

	// Verify +Inf radius round-trips correctly.
	seg1 := got[0].Segments[1]
	if !math.IsInf(seg1.Radius, 1) {
		t.Errorf("Radius: got %v, want +Inf", seg1.Radius)
	}

	// Verify tags round-trip.
	if len(got[0].Tags) != len(ways[0].Tags) {
		t.Errorf("Tags length: got %d, want %d", len(got[0].Tags), len(ways[0].Tags))
	}
	for k, want := range ways[0].Tags {
		if got[0].Tags[k] != want {
			t.Errorf("Tags[%q]: got %q, want %q", k, got[0].Tags[k], want)
		}
	}

	// Verify other fields.
	seg0 := got[0].Segments[0]
	if seg0.Radius != 50.0 {
		t.Errorf("Radius: got %v, want 50.0", seg0.Radius)
	}
	if seg0.Tier != 3 {
		t.Errorf("Tier: got %d, want 3", seg0.Tier)
	}
	if seg0.Start.Lat != 45.1 || seg0.Start.Lon != -122.5 {
		t.Errorf("Start: got %v, want {45.1, -122.5}", seg0.Start)
	}
}

func TestScoreCache_TagsRoundTrip(t *testing.T) {
	c := newTestScoreCache(t)
	tile := sampleTile()
	rawData := []byte(`{"raw":"tile data"}`)
	ways := sampleScoredWays()

	if err := c.Write(tile, rawData, ways, 0); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, _, ok := c.Read(tile, rawData)
	if !ok {
		t.Fatal("Read returned false, expected true")
	}

	if len(got) == 0 {
		t.Fatal("Read returned no ways")
	}

	wantTags := map[string]string{"highway": "secondary", "name": "Test Road"}
	gotTags := got[0].Tags
	if len(gotTags) != len(wantTags) {
		t.Fatalf("Tags: got %v, want %v", gotTags, wantTags)
	}
	for k, v := range wantTags {
		if gotTags[k] != v {
			t.Errorf("Tags[%q]: got %q, want %q", k, gotTags[k], v)
		}
	}
}

func TestScoreCache_RawTileDataChanged(t *testing.T) {
	c := newTestScoreCache(t)
	tile := sampleTile()
	rawData := []byte(`{"raw":"tile data"}`)
	ways := sampleScoredWays()

	if err := c.Write(tile, rawData, ways, 0); err != nil {
		t.Fatalf("Write: %v", err)
	}

	differentData := []byte(`{"raw":"different tile data"}`)
	got, _, ok := c.Read(tile, differentData)
	if ok {
		t.Error("Read returned true, expected false (raw tile data changed)")
	}
	if got != nil {
		t.Error("Read returned non-nil ways, expected nil")
	}
}

func TestScoreCache_ParamsHashChanged(t *testing.T) {
	c := newTestScoreCache(t)
	tile := sampleTile()
	rawData := []byte(`{"raw":"tile data"}`)
	ways := sampleScoredWays()

	// Build a cache entry with a wrong params hash and write it directly.
	entry := scoreCacheEntryJSON{
		RawTileHash: hashBytes(rawData),
		ParamsHash:  "sha256:wronghashvalue",
		Ways:        scoredWaysToJSON(ways),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(c.Path(tile), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, _, ok := c.Read(tile, rawData)
	if ok {
		t.Error("Read returned true, expected false (params hash mismatch)")
	}
	if got != nil {
		t.Error("Read returned non-nil ways, expected nil")
	}
}

func TestScoreCache_CacheMiss_NoFile(t *testing.T) {
	c := newTestScoreCache(t)
	tile := sampleTile()
	rawData := []byte(`{"raw":"tile data"}`)

	got, _, ok := c.Read(tile, rawData)
	if ok {
		t.Error("Read returned true on empty cache, expected false")
	}
	if got != nil {
		t.Error("Read returned non-nil ways, expected nil")
	}
}

func TestScoreCache_ClearAll(t *testing.T) {
	c := newTestScoreCache(t)
	rawData := []byte(`{"raw":"tile data"}`)
	ways := sampleScoredWays()

	tiles := []Tile{
		{South: 45.0, West: -123.0, North: 45.05, East: -122.95},
		{South: 45.05, West: -123.0, North: 45.10, East: -122.95},
	}

	for _, tile := range tiles {
		if err := c.Write(tile, rawData, ways, 0); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	// Verify both exist.
	for _, tile := range tiles {
		if !c.Has(tile) {
			t.Errorf("expected cache to have tile %v before ClearAll", tile)
		}
	}

	if err := c.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}

	// Verify both are gone.
	for _, tile := range tiles {
		if c.Has(tile) {
			t.Errorf("expected cache to NOT have tile %v after ClearAll", tile)
		}
		got, _, ok := c.Read(tile, rawData)
		if ok || got != nil {
			t.Errorf("Read after ClearAll should return nil,false for tile %v", tile)
		}
	}
}

func TestScoreCache_AtomicWrite(t *testing.T) {
	c := newTestScoreCache(t)
	tile := sampleTile()
	rawData := []byte(`{"raw":"tile data"}`)
	ways := sampleScoredWays()

	if err := c.Write(tile, rawData, ways, 0); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify file exists.
	path := c.Path(tile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after Write: %v", err)
	}
	if info.Size() == 0 {
		t.Error("cache file is empty after Write")
	}

	// Verify file contains valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var entry scoreCacheEntryJSON
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Errorf("cache file is not valid JSON: %v", err)
	}

	// Verify no temp files remain.
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file %q remains after Write", e.Name())
		}
	}
}

func TestScoreCache_PathFormat(t *testing.T) {
	c := &ScoreCache{Dir: "/some/cache/dir", Precision: 3}
	tile := Tile{South: 45.0, West: -123.0, North: 45.05, East: -122.95}

	got := c.Path(tile)
	want := "/some/cache/dir/tile_45.000_-123.000.json"

	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}

	// Verify it matches TileCache format.
	tc := &TileCache{Dir: "/some/cache/dir", Precision: 3}
	tcPath := tc.Path(tile)
	if got != tcPath {
		t.Errorf("ScoreCache.Path() = %q, TileCache.Path() = %q — should match", got, tcPath)
	}
}

func TestHashBytes(t *testing.T) {
	data1 := []byte("hello world")
	data2 := []byte("hello world")
	data3 := []byte("different")

	h1 := hashBytes(data1)
	h2 := hashBytes(data2)
	h3 := hashBytes(data3)

	if h1 != h2 {
		t.Errorf("same input gives different hashes: %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different inputs give same hash: %q", h1)
	}
	if len(h1) < 8 || h1[:7] != "sha256:" {
		t.Errorf("hash does not start with 'sha256:': %q", h1)
	}
}

func TestScoreCache_JSONRoundTrip(t *testing.T) {
	ways := []ScoredWay{
		{
			WayID: 99,
			Tags:  map[string]string{"highway": "primary", "name": "Main St"},
			Segments: []ScoredSegment{
				{
					Start:  geo.Coord{Lat: 1.23, Lon: 4.56},
					End:    geo.Coord{Lat: 7.89, Lon: 10.11},
					Radius: math.Inf(1),
					Tier:   0,
					Weight: 0.0,
					Length: 200.0,
					Score:  0.0,
				},
				{
					Start:  geo.Coord{Lat: 7.89, Lon: 10.11},
					End:    geo.Coord{Lat: 12.13, Lon: 14.15},
					Radius: 25.5,
					Tier:   4,
					Weight: TierWeight4,
					Length: 150.0,
					Score:  150.0 * TierWeight4,
				},
			},
		},
	}

	jsonWays := scoredWaysToJSON(ways)
	restored := scoredWaysFromJSON(jsonWays)

	if len(restored) != len(ways) {
		t.Fatalf("way count: got %d, want %d", len(restored), len(ways))
	}
	if restored[0].WayID != 99 {
		t.Errorf("WayID: got %d, want 99", restored[0].WayID)
	}
	if restored[0].Tags["highway"] != "primary" || restored[0].Tags["name"] != "Main St" {
		t.Errorf("Tags: got %v, want {highway:primary name:Main St}", restored[0].Tags)
	}
	if len(restored[0].Segments) != 2 {
		t.Fatalf("segment count: got %d, want 2", len(restored[0].Segments))
	}

	s0 := restored[0].Segments[0]
	if !math.IsInf(s0.Radius, 1) {
		t.Errorf("Segments[0].Radius: got %v, want +Inf", s0.Radius)
	}
	if s0.Start.Lat != 1.23 || s0.Start.Lon != 4.56 {
		t.Errorf("Segments[0].Start: got %v", s0.Start)
	}

	s1 := restored[0].Segments[1]
	if s1.Radius != 25.5 {
		t.Errorf("Segments[1].Radius: got %v, want 25.5", s1.Radius)
	}
	if s1.Score != 150.0*TierWeight4 {
		t.Errorf("Segments[1].Score: got %v, want %v", s1.Score, 150.0*TierWeight4)
	}
}
