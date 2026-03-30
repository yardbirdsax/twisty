package main

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/yardbirdsax/twisty/quality"
)

// tileResult holds the output of processing a single tile.
type tileResult struct {
	ways     []quality.ScoredWay
	wayCount int
	segCount int
	cacheHit bool
}

// pipelineStats accumulates aggregate statistics from tile processing.
type pipelineStats struct {
	cacheHits atomic.Int64
	totalWays atomic.Int64
	totalSegs atomic.Int64
}

// tileProcessorFunc is the function signature for processing a single tile.
// It returns nil result when the tile should be skipped (no data), or a non-nil
// error for unexpected failures that should abort the pipeline.
type tileProcessorFunc func(tile quality.Tile) (*tileResult, error)

// processTilesConcurrently processes all tiles using a worker pool and returns
// the scored ways grouped by name, along with aggregate statistics.
func processTilesConcurrently(
	ctx context.Context,
	tiles []quality.Tile,
	tileCache *quality.TileCache,
	scoreCache *quality.ScoreCache,
	noCache bool,
	logger *slog.Logger,
	progress quality.ProgressReporter,
) (map[string]quality.ScoredWays, *pipelineStats, error) {
	processor := func(tile quality.Tile) (*tileResult, error) {
		return processSingleTile(tile, tileCache, scoreCache, noCache, logger)
	}
	return processTilesConcurrentlyWith(ctx, tiles, processor, progress)
}

// runWorkerPool runs work items through a pool of GOMAXPROCS workers.
// The processor fn returns (*R, error); a nil *R means skip (no result emitted).
// The caller must drain resultCh fully before inspecting errCh; the resultCh is
// closed once all workers have exited. cancel must be called by the caller (via
// defer) to release the context. errCh is buffered to hold one error per worker.
func runWorkerPool[W, R any](
	ctx context.Context,
	items []W,
	processor func(W) (*R, error),
) (<-chan R, <-chan error, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)

	workerCount := runtime.GOMAXPROCS(0)
	workCh := make(chan W, len(items))
	resultCh := make(chan R, workerCount*2)
	errCh := make(chan error, workerCount)

	var wg sync.WaitGroup

	for range workerCount {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-workCh:
					if !ok {
						return
					}
					res, err := processor(item)
					if err != nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}
					if res == nil {
						continue
					}
					select {
					case resultCh <- *res:
					case <-ctx.Done():
						return
					}
				}
			}
		})
	}

	go func() {
		defer close(workCh)
		for _, item := range items {
			select {
			case workCh <- item:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	return resultCh, errCh, cancel
}

// drainPoolErr checks errCh and ctx after the result channel has been drained.
// It returns the first worker error if one was sent, the context error if the
// context was canceled, or nil otherwise.
func drainPoolErr(ctx context.Context, errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	default:
	}
	if ctx.Err() != nil {
		select {
		case err := <-errCh:
			return err
		default:
			return ctx.Err()
		}
	}
	return nil
}

// processTilesConcurrentlyWith is the internal implementation of tile processing
// that accepts a pluggable tileProcessorFunc. This enables unit tests to inject
// error-returning processors without requiring a real tile cache on disk.
func processTilesConcurrentlyWith(
	ctx context.Context,
	tiles []quality.Tile,
	processor tileProcessorFunc,
	progress quality.ProgressReporter,
) (map[string]quality.ScoredWays, *pipelineStats, error) {
	stats := &pipelineStats{}

	if progress != nil {
		progress.SetTotal(len(tiles))
	}

	resultCh, errCh, cancel := runWorkerPool(ctx, tiles, processor)
	defer cancel()

	// Collect results and group by name, deduplicating by WayID per group.
	grouped := make(map[string]quality.ScoredWays)
	seenPerGroup := make(map[string]map[int64]bool)
	for res := range resultCh {
		if res.cacheHit {
			stats.cacheHits.Add(1)
		}
		stats.totalWays.Add(int64(res.wayCount))
		stats.totalSegs.Add(int64(res.segCount))

		for _, w := range res.ways {
			for _, key := range quality.GroupKeys(w) {
				if seenPerGroup[key] == nil {
					seenPerGroup[key] = make(map[int64]bool)
				}
				if !seenPerGroup[key][w.WayID] {
					seenPerGroup[key][w.WayID] = true
					grouped[key] = append(grouped[key], w)
				}
			}
		}

		if progress != nil {
			progress.Tick(res.cacheHit)
		}
	}

	if progress != nil {
		progress.Done()
	}

	if err := drainPoolErr(ctx, errCh); err != nil {
		return nil, stats, err
	}

	return grouped, stats, nil
}

// processSingleTile processes one tile and returns scored ways. Returns nil if the tile
// should be skipped (no data). Returns an error only for unexpected failures.
func processSingleTile(
	tile quality.Tile,
	tileCache *quality.TileCache,
	scoreCache *quality.ScoreCache,
	noCache bool,
	logger *slog.Logger,
) (*tileResult, error) {
	if !tileCache.Has(tile) {
		logger.Warn("no raw tile data, skipping", "south", tile.South, "west", tile.West)
		return nil, nil
	}

	rawData, err := tileCache.Read(tile)
	if err != nil {
		logger.Warn("failed to read tile data, skipping", "south", tile.South, "west", tile.West, "error", err)
		return nil, nil
	}

	if !noCache {
		if cachedWays, hit := scoreCache.Read(tile, rawData); hit {
			logger.Debug("score cache hit", "south", tile.South, "west", tile.West)
			segCount := 0
			for _, w := range cachedWays {
				segCount += len(w.Segments)
			}
			return &tileResult{
				ways:     cachedWays,
				wayCount: len(cachedWays),
				segCount: segCount,
				cacheHit: true,
			}, nil
		}
	}

	ways, err := quality.ParseTileData(rawData)
	if err != nil {
		logger.Warn("failed to parse tile data, skipping", "south", tile.South, "west", tile.West, "error", err)
		return nil, nil
	}

	result := quality.RunScorePipeline(ways)

	if err := scoreCache.Write(tile, rawData, result.ScoredWays); err != nil {
		logger.Warn("failed to write score cache", "south", tile.South, "west", tile.West, "error", err)
	}

	return &tileResult{
		ways:     result.ScoredWays,
		wayCount: len(result.ScoredWays),
		segCount: result.TotalSegments,
		cacheHit: false,
	}, nil
}

// nameGroupWork is a unit of work for Phase B workers.
type nameGroupWork struct {
	name string
	ways []quality.ScoredWay
}

// processNameGroupsConcurrently processes name groups in parallel (Phase B).
// Each group runs connected-component analysis, ordering, splitting, and scoring.
// Results are collected and returned as a flat []RoadCollection.
func processNameGroupsConcurrently(ctx context.Context, groups map[string]quality.ScoredWays) ([]quality.RoadCollection, error) {
	if len(groups) == 0 {
		return nil, nil
	}

	items := make([]nameGroupWork, 0, len(groups))
	for name, ways := range groups {
		items = append(items, nameGroupWork{name: name, ways: ways})
	}

	processor := func(w nameGroupWork) (*[]quality.RoadCollection, error) {
		cols, err := processNameGroup(w.name, w.ways)
		if err != nil {
			return nil, err
		}
		return &cols, nil
	}

	resultCh, errCh, cancel := runWorkerPool(ctx, items, processor)
	defer cancel()

	var all []quality.RoadCollection
	for cols := range resultCh {
		all = append(all, cols...)
	}

	if err := drainPoolErr(ctx, errCh); err != nil {
		return nil, err
	}

	return all, nil
}

// scoreAndAggregateTiles scores a set of pre-fetched tiles, aggregates by name group,
// and applies penalties. It is the common Phase A+B pipeline used by both runScore
// and runRandom.
func scoreAndAggregateTiles(
	ctx context.Context,
	tiles []quality.Tile,
	tileCache *quality.TileCache,
	scoreCache *quality.ScoreCache,
	noCache bool,
	logger *slog.Logger,
	progress quality.ProgressReporter,
) ([]quality.RoadCollection, *pipelineStats, error) {
	grouped, stats, err := processTilesConcurrently(ctx, tiles, tileCache, scoreCache, noCache, logger, progress)
	if err != nil {
		return nil, stats, err
	}
	collections, err := processNameGroupsConcurrently(ctx, grouped)
	if err != nil {
		return nil, stats, err
	}
	quality.ApplyPenalties(collections)
	return collections, stats, nil
}

// processNameGroup runs the full per-name aggregation pipeline for a single road name.
func processNameGroup(name string, namedWays []quality.ScoredWay) ([]quality.RoadCollection, error) {
	// Deep-copy all input ways so that UnflattenWaySegments writes to freshly
	// allocated segment slices, preventing data races when multiple goroutines
	// process groups whose input ways share backing arrays.
	waysCopy := quality.DeepCopyWays(namedWays)
	return quality.AggregateNameGroup(name, waysCopy), nil
}
