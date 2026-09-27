package quality

// TileScoreResult is the outcome of scoring a single Tile.
//
// Found is false when the tile has no raw data in TileCache — the caller
// should treat this the same as a tile that hasn't been fetched yet.
// CacheHit is true when the result was served from ScoreCache without
// re-running ParseTileData/RunScorePipeline.
type TileScoreResult struct {
	ScoredWays ScoredWays
	Found      bool
	CacheHit   bool
}

// TileScorer scores a Tile's raw OSM data, transparently going through
// ScoreCache so that a tile scored once is not reparsed and rescored on
// subsequent calls with unchanged raw tile data.
type TileScorer struct {
	TileCache  *TileCache
	ScoreCache *ScoreCache
}

// Score returns the ScoredWays for a Tile, reading raw tile data from
// TileCache, consulting ScoreCache for a cached result, and falling back to
// ParseTileData + RunScorePipeline on a cache miss (writing the result back
// to ScoreCache).
//
// A tile whose raw data is missing, unreadable, or unparsable is reported as
// Found:false rather than as an error, so callers treat it the same as a
// tile that hasn't been fetched yet and retry it (matching the raw-tile-cache
// read failures already tolerated by the CLI's pipeline). A failure to write
// ScoreCache does not discard an already-computed result: it is returned
// alongside the valid ScoredWays so the caller can log it without losing or
// permanently dropping the tile.
func (s *TileScorer) Score(tile Tile) (TileScoreResult, error) {
	if !s.TileCache.Has(tile) {
		return TileScoreResult{}, nil
	}

	rawData, err := s.TileCache.Read(tile)
	if err != nil {
		return TileScoreResult{}, nil
	}

	if cachedWays, hit := s.ScoreCache.Read(tile, rawData); hit {
		return TileScoreResult{ScoredWays: cachedWays, Found: true, CacheHit: true}, nil
	}

	ways, err := ParseTileData(rawData)
	if err != nil {
		return TileScoreResult{}, nil
	}

	result := RunScorePipeline(ways)
	scoreResult := TileScoreResult{ScoredWays: result.ScoredWays, Found: true, CacheHit: false}

	if err := s.ScoreCache.Write(tile, rawData, result.ScoredWays); err != nil {
		return scoreResult, err
	}

	return scoreResult, nil
}
