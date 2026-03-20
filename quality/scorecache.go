package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/yardbirdsax/twisty/geo"
)

// infiniteRadiusSentinel is the JSON sentinel value used to represent +Inf radius.
// +Inf does not serialize cleanly in JSON, so we use -1 as a sentinel.
const infiniteRadiusSentinel = -1.0

// scoredSegmentJSON is the on-disk JSON representation of a ScoredSegment.
// It uses -1 as a sentinel for +Inf radius.
type scoredSegmentJSON struct {
	Start  coordJSON `json:"start"`
	End    coordJSON `json:"end"`
	Radius float64   `json:"radius"`
	Tier   int       `json:"tier"`
	Weight float64   `json:"weight"`
	Length float64   `json:"length"`
	Score  float64   `json:"score"`
}

type coordJSON struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type scoredWayJSON struct {
	WayID    int64                `json:"way_id"`
	Segments []scoredSegmentJSON `json:"segments"`
}

// scoreCacheEntryJSON is the on-disk format for a cached tile's scored segments.
// Uses JSON-safe representations for floating-point values.
type scoreCacheEntryJSON struct {
	RawTileHash string          `json:"raw_tile_hash"`
	ParamsHash  string          `json:"params_hash"`
	Ways        []scoredWayJSON `json:"ways"`
}

// ScoreCacheEntry is the on-disk format for a cached tile's scored segments.
type ScoreCacheEntry struct {
	RawTileHash string      `json:"raw_tile_hash"`
	ParamsHash  string      `json:"params_hash"`
	Ways        []ScoredWay `json:"ways"`
}

// ScoreCache manages cached scoring results per tile.
type ScoreCache struct {
	Dir       string
	Precision int
}

// EnsureDir creates the cache directory if it does not already exist.
func (c *ScoreCache) EnsureDir() error {
	return os.MkdirAll(c.Dir, 0o755)
}

// Path returns the full file path for the cache entry of the given tile.
func (c *ScoreCache) Path(t Tile) string {
	return filepath.Join(c.Dir, TileCacheKey(t, c.Precision))
}

// Has returns true if a cache file exists for the given tile.
func (c *ScoreCache) Has(t Tile) bool {
	_, err := os.Stat(c.Path(t))
	return err == nil
}

// ClearAll removes and recreates the cache directory.
func (c *ScoreCache) ClearAll() error {
	if err := os.RemoveAll(c.Dir); err != nil {
		return err
	}
	return os.MkdirAll(c.Dir, 0o755)
}

// Read loads a cache entry and validates both hashes. Returns the cached
// scored ways and true if valid, or nil and false if the cache misses.
func (c *ScoreCache) Read(t Tile, rawTileData []byte) ([]ScoredWay, bool) {
	if !c.Has(t) {
		return nil, false
	}

	data, err := os.ReadFile(c.Path(t))
	if err != nil {
		return nil, false
	}

	var entry scoreCacheEntryJSON
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if entry.RawTileHash != hashBytes(rawTileData) {
		return nil, false
	}
	if entry.ParamsHash != ScoringParamsHash() {
		return nil, false
	}

	ways := scoredWaysFromJSON(entry.Ways)
	return ways, true
}

// Write stores scored ways along with the current raw tile hash and params hash.
func (c *ScoreCache) Write(t Tile, rawTileData []byte, ways []ScoredWay) error {
	if err := c.EnsureDir(); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}

	entry := scoreCacheEntryJSON{
		RawTileHash: hashBytes(rawTileData),
		ParamsHash:  ScoringParamsHash(),
		Ways:        scoredWaysToJSON(ways),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal cache entry: %w", err)
	}

	target := c.Path(t)
	tmp, err := os.CreateTemp(c.Dir, "score-*.tmp")
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
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

// hashBytes returns "sha256:<hex>" for the given data.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// scoredWaysToJSON converts a ScoredWay slice to the JSON-safe representation,
// encoding +Inf radius as the sentinel value -1.
func scoredWaysToJSON(ways []ScoredWay) []scoredWayJSON {
	result := make([]scoredWayJSON, len(ways))
	for i, w := range ways {
		segs := make([]scoredSegmentJSON, len(w.Segments))
		for j, seg := range w.Segments {
			r := seg.Radius
			if math.IsInf(r, 1) {
				r = infiniteRadiusSentinel
			}
			segs[j] = scoredSegmentJSON{
				Start:  coordJSON{Lat: seg.Start.Lat, Lon: seg.Start.Lon},
				End:    coordJSON{Lat: seg.End.Lat, Lon: seg.End.Lon},
				Radius: r,
				Tier:   seg.Tier,
				Weight: seg.Weight,
				Length: seg.Length,
				Score:  seg.Score,
			}
		}
		result[i] = scoredWayJSON{WayID: w.WayID, Segments: segs}
	}
	return result
}

// scoredWaysFromJSON converts the JSON-safe representation back to a ScoredWay slice,
// restoring the sentinel value -1 to +Inf radius.
func scoredWaysFromJSON(ways []scoredWayJSON) []ScoredWay {
	result := make([]ScoredWay, len(ways))
	for i, w := range ways {
		segs := make([]ScoredSegment, len(w.Segments))
		for j, seg := range w.Segments {
			r := seg.Radius
			if r == infiniteRadiusSentinel {
				r = math.Inf(1)
			}
			segs[j] = ScoredSegment{
				Start:  geo.Coord{Lat: seg.Start.Lat, Lon: seg.Start.Lon},
				End:    geo.Coord{Lat: seg.End.Lat, Lon: seg.End.Lon},
				Radius: r,
				Tier:   seg.Tier,
				Weight: seg.Weight,
				Length: seg.Length,
				Score:  seg.Score,
			}
		}
		result[i] = ScoredWay{WayID: w.WayID, Segments: segs}
	}
	return result
}
