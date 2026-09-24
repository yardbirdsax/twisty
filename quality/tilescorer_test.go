package quality

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTileScorer_Score_MissingRawTile_ReturnsMiss(t *testing.T) {
	dir := t.TempDir()
	scorer := &TileScorer{
		TileCache:  &TileCache{Dir: dir, Precision: 3},
		ScoreCache: &ScoreCache{Dir: dir + "/scores", Precision: 3},
	}

	tile := Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	result, err := scorer.Score(tile)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Found {
		t.Fatalf("expected Found=false for a tile with no raw data")
	}
	if len(result.ScoredWays) != 0 {
		t.Fatalf("expected no scored ways, got %d", len(result.ScoredWays))
	}
}

func TestTileScorer_Score_FirstCallParsesAndScoresThenWritesScoreCache(t *testing.T) {
	dir := t.TempDir()
	tileCache := &TileCache{Dir: dir, Precision: 3}
	scoreCache := &ScoreCache{Dir: dir + "/scores", Precision: 3}
	scorer := &TileScorer{TileCache: tileCache, ScoreCache: scoreCache}

	tile := Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := tileCache.Write(tile, tileData); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := scorer.Score(tile)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Found {
		t.Fatalf("expected Found=true")
	}
	if result.CacheHit {
		t.Fatalf("expected CacheHit=false on first score")
	}
	if len(result.ScoredWays) != 1 {
		t.Fatalf("expected 1 scored way, got %d", len(result.ScoredWays))
	}

	if !scoreCache.Has(tile) {
		t.Fatalf("expected score cache to be populated after Score")
	}
}

func TestTileScorer_Score_ScoreCacheWriteFailure_StillReturnsScoredWays(t *testing.T) {
	dir := t.TempDir()
	tileCache := &TileCache{Dir: dir, Precision: 3}

	// Point ScoreCache.Dir at a path that can never be created as a
	// directory (a regular file sits where a path segment is needed), so
	// ScoreCache.Write fails deterministically.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	scoreCache := &ScoreCache{Dir: filepath.Join(blocker, "scores"), Precision: 3}
	scorer := &TileScorer{TileCache: tileCache, ScoreCache: scoreCache}

	tile := Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := tileCache.Write(tile, tileData); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := scorer.Score(tile)
	if err == nil {
		t.Fatalf("expected an error from a failed ScoreCache.Write")
	}
	if !result.Found {
		t.Fatalf("expected Found=true: a write-cache failure must not discard an already-scored tile")
	}
	if len(result.ScoredWays) != 1 {
		t.Fatalf("expected 1 scored way despite write failure, got %d", len(result.ScoredWays))
	}
}

func TestTileScorer_Score_RawTileReadFailure_ReturnsMissRatherThanError(t *testing.T) {
	dir := t.TempDir()
	tileCache := &TileCache{Dir: dir, Precision: 3}
	scoreCache := &ScoreCache{Dir: dir + "/scores", Precision: 3}
	scorer := &TileScorer{TileCache: tileCache, ScoreCache: scoreCache}

	tile := Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := tileCache.Write(tile, []byte("valid")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Replace the cache file with a directory so TileCache.Has (os.Stat)
	// still succeeds but TileCache.Read (os.ReadFile) fails.
	path := tileCache.Path(tile)
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	result, err := scorer.Score(tile)
	if err != nil {
		t.Fatalf("expected a raw-tile read failure to be treated as a miss, not an error: %v", err)
	}
	if result.Found {
		t.Fatalf("expected Found=false when raw tile data cannot be read, so the caller retries it")
	}
}

func TestTileScorer_Score_SecondCallServesFromScoreCache(t *testing.T) {
	dir := t.TempDir()
	tileCache := &TileCache{Dir: dir, Precision: 3}
	scoreCache := &ScoreCache{Dir: dir + "/scores", Precision: 3}
	scorer := &TileScorer{TileCache: tileCache, ScoreCache: scoreCache}

	tile := Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := tileCache.Write(tile, tileData); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := scorer.Score(tile); err != nil {
		t.Fatalf("first Score: %v", err)
	}

	result, err := scorer.Score(tile)
	if err != nil {
		t.Fatalf("second Score: %v", err)
	}
	if !result.Found {
		t.Fatalf("expected Found=true")
	}
	if !result.CacheHit {
		t.Fatalf("expected CacheHit=true on second score (should be served from ScoreCache)")
	}
	if len(result.ScoredWays) != 1 {
		t.Fatalf("expected 1 scored way, got %d", len(result.ScoredWays))
	}
}
