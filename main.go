package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/geocode"
	"github.com/yardbirdsax/twisty/gpx"
	"github.com/yardbirdsax/twisty/osmconv"
	"github.com/yardbirdsax/twisty/quality"
	"github.com/yardbirdsax/twisty/route"
	"github.com/yardbirdsax/twisty/waypoint"
)

// builtInGoogleClientID and builtInGoogleClientSecret are reserved for a future
// OAuth flow. They are currently unused. Set at build time via -ldflags if needed.
var (
	builtInGoogleClientID     = ""
	builtInGoogleClientSecret = ""
)

// builtInGoogleAPIKey is the Routes API key injected at build time via:
// -ldflags "-X main.builtInGoogleAPIKey=<key>"
var builtInGoogleAPIKey = ""

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "twisty",
		Short:         "Twisty — motorcycle route finder",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		newRouteCmd(),
		newFetchCmd(),
		newScoreCmd(),
		newRandomCmd(),
		newOverpassCmd(),
		newGpxCmd(),
	)
	return cmd
}

// ─── execScore ───────────────────────────────────────────────────────────────

type execScoreParams struct {
	address         string
	radius          float64
	tileSize        float64
	cacheDir        string
	noCache         bool
	clearCache      bool
	clearScoreCache bool
	verbose         bool
	outPath         string
	minScore        float64
	minSpeed        float64
	multiColor      bool
	overpassURL     string
	fetchDelay      string
	stderr          io.Writer
}

func execScore(p execScoreParams) error {
	stderr := p.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	var logger *slog.Logger
	if p.verbose {
		handler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	if p.address == "" {
		return fmt.Errorf("-address is required")
	}

	if p.outPath == "" {
		return fmt.Errorf("-out is required")
	}

	if p.radius <= 0 {
		return fmt.Errorf("-radius must be > 0 km")
	}

	if p.tileSize <= 0 {
		return fmt.Errorf("-tile-size must be greater than 0")
	}

	cacheDir := p.cacheDir
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".twisty", "cache", "overpass")
	}

	scoreCacheDir := filepath.Join(filepath.Dir(cacheDir), "scores")

	tileCache := &quality.TileCache{
		Dir:       cacheDir,
		Precision: 3,
	}
	scoreCache := &quality.ScoreCache{
		Dir:       scoreCacheDir,
		Precision: 3,
	}

	if err := scoreCache.EnsureDir(); err != nil {
		return fmt.Errorf("creating score cache dir: %w", err)
	}

	if p.clearCache {
		logger.Info("clearing tile cache", "dir", cacheDir)
		if err := tileCache.ClearAll(); err != nil {
			return fmt.Errorf("clearing tile cache: %w", err)
		}
		fmt.Fprintln(stderr, "Tile cache cleared.")

		logger.Info("clearing score cache", "dir", scoreCacheDir)
		if err := scoreCache.ClearAll(); err != nil {
			return fmt.Errorf("clearing score cache: %w", err)
		}
		fmt.Fprintln(stderr, "Score cache cleared.")
	}

	if p.clearScoreCache {
		logger.Info("clearing score cache", "dir", scoreCacheDir)
		if err := scoreCache.ClearAll(); err != nil {
			return fmt.Errorf("clearing score cache: %w", err)
		}
		fmt.Fprintln(stderr, "Score cache cleared.")
	}

	// Geocode the address.
	centerResult, err := geocode.Resolve(p.address, "Center")
	if err != nil {
		return fmt.Errorf("resolving address: %w", err)
	}
	logger.Debug("geocoded address", "display_name", centerResult.DisplayName)

	// Fetch any missing tiles before scoring.
	var fetchProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !p.verbose && isTerminal(os.Stderr) {
		fetchProgress = &termProgressBar{w: os.Stderr, label: "Fetching tiles:"}
	}
	fetchCfg := quality.TileFetchConfig{
		Endpoint: p.overpassURL,
		TileSize: p.tileSize,
		Cache:    tileCache,
		Logger:   logger,
		Progress: fetchProgress,
	}
	if p.fetchDelay != "" {
		d, err := time.ParseDuration(p.fetchDelay)
		if err != nil {
			return fmt.Errorf("invalid -fetch-delay %q: %w", p.fetchDelay, err)
		}
		fetchCfg.RateLimitDelay = d
	}
	if _, err := quality.FetchTiledWays(context.Background(), centerResult.Lat, centerResult.Lon, p.radius, fetchCfg); err != nil {
		return fmt.Errorf("fetching tiles: %w", err)
	}

	// Compute tiles.
	tiles := quality.ComputeTiles(centerResult.Lat, centerResult.Lon, p.radius, p.tileSize)
	logger.Debug("computed tiles", "count", len(tiles))

	totalTiles := len(tiles)

	// Phase A + B: concurrent tile scoring, name-group aggregation, and penalties.
	var scoreProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !p.verbose && isTerminal(os.Stderr) {
		scoreProgress = &termProgressBar{w: os.Stderr, label: "Scoring tiles:"}
	}
	collections, stats, err := scoreAndAggregateTiles(
		context.Background(),
		tiles,
		tileCache,
		scoreCache,
		p.noCache,
		logger,
		scoreProgress,
	)
	if err != nil {
		return fmt.Errorf("processing tiles: %w", err)
	}

	if len(collections) == 0 {
		fmt.Fprintln(stderr, "WARNING: No road data found for this area. Writing empty KML.")
		f, err := os.Create(p.outPath)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer f.Close()
		if p.multiColor {
			return quality.WriteKML(f, nil, p.minScore, p.minSpeed)
		}
		return quality.WriteKMLSingleColor(f, nil, p.minScore, p.minSpeed)
	}

	fmt.Fprintln(stderr, "Score complete.")
	fmt.Fprintf(stderr, "  Tiles processed: %d\n", totalTiles)
	fmt.Fprintf(stderr, "  Cache hits:       %d\n", stats.cacheHits.Load())
	fmt.Fprintf(stderr, "  Ways scored:      %d\n", stats.totalWays.Load())
	fmt.Fprintf(stderr, "  Segments scored:  %d\n", stats.totalSegs.Load())

	// Stage 7: KML Output
	f, err := os.Create(p.outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	if p.multiColor {
		if err := quality.WriteKML(f, collections, p.minScore, p.minSpeed); err != nil {
			return fmt.Errorf("writing KML: %w", err)
		}
	} else {
		if err := quality.WriteKMLSingleColor(f, collections, p.minScore, p.minSpeed); err != nil {
			return fmt.Errorf("writing KML: %w", err)
		}
	}

	// Count collections that pass the min-score, min-length, and min-speed filters.
	inOutput := 0
	for _, c := range collections {
		if c.PenalizedScore < p.minScore || c.TotalLength < quality.MinRoadLengthM {
			continue
		}
		if p.minSpeed > 0 && quality.SpeedPassingFraction(c.WaySpeeds, c.TotalLength, p.minSpeed) < 0.5 {
			continue
		}
		inOutput++
	}

	if inOutput == 0 {
		fmt.Fprintf(stderr, "WARNING: No road collections passed the output filters. KML output is empty.\n")
	}

	filterDesc := fmt.Sprintf("min-score %.0f, min-length %.0f m", p.minScore, quality.MinRoadLengthM)
	if p.minSpeed > 0 {
		filterDesc += fmt.Sprintf(", min-speed %.0f mph", p.minSpeed)
	}
	fmt.Fprintf(stderr, "Aggregated %d road collections (%d in output after %s filters).\n", len(collections), inOutput, filterDesc)

	// Top 5 roads by penalized score (sorted descending)
	topN := min(5, len(collections))

	// Sort a copy for summary output
	sorted := make([]quality.RoadCollection, len(collections))
	copy(sorted, collections)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PenalizedScore > sorted[j].PenalizedScore
	})

	if topN > 0 {
		fmt.Fprintln(stderr, "Top roads:")
		for i := range topN {
			fmt.Fprintf(stderr, "  %d. %-20s — %d\n", i+1, sorted[i].DisplayName(), int(sorted[i].PenalizedScore))
		}
	}

	fmt.Fprintf(stderr, "Output: %s\n", p.outPath)
	return nil
}

func newScoreCmd() *cobra.Command {
	var (
		address         string
		radius          float64
		tileSize        float64
		cacheDir        string
		noCache         bool
		clearCache      bool
		clearScoreCache bool
		verbose         bool
		outPath         string
		minScore        float64
		minSpeed        float64
		multiColor      bool
		overpassURL     string
		fetchDelay      string
	)
	cmd := &cobra.Command{
		Use:           "score",
		Short:         "Score road twistiness in a radius and output KML",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execScore(execScoreParams{
				address:         address,
				radius:          radius,
				tileSize:        tileSize,
				cacheDir:        cacheDir,
				noCache:         noCache,
				clearCache:      clearCache,
				clearScoreCache: clearScoreCache,
				verbose:         verbose,
				outPath:         outPath,
				minScore:        minScore,
				minSpeed:        minSpeed,
				multiColor:      multiColor,
				overpassURL:     overpassURL,
				fetchDelay:      fetchDelay,
				stderr:          cmd.ErrOrStderr(),
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&address, "address", "", "Location center address (required)")
	f.Float64Var(&radius, "radius", 25.0, "Search radius in km")
	f.Float64Var(&tileSize, "tile-size", 0.1, "Tile size in degrees")
	f.StringVar(&cacheDir, "cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
	f.BoolVar(&noCache, "no-cache", false, "Skip score cache reads (still writes)")
	f.BoolVar(&clearCache, "clear-cache", false, "Delete all cached tiles and score cache entries before running")
	f.BoolVar(&clearScoreCache, "clear-score-cache", false, "Delete all score cache entries before running")
	f.BoolVar(&verbose, "v", false, "Enable verbose logging to stderr")
	f.StringVar(&outPath, "out", "", "Output KML file path (required)")
	f.Float64Var(&minScore, "min-score", 0, "Minimum penalized score to include in output")
	f.Float64Var(&minSpeed, "min-speed", 0, "Minimum speed limit in mph; exclude roads where majority of length is below this")
	f.BoolVar(&multiColor, "multi-color", false, "Use per-segment tier coloring instead of single-color per-road")
	f.StringVar(&overpassURL, "overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
	f.StringVar(&fetchDelay, "fetch-delay", "", "delay between tile fetches in Go duration format (e.g. 500ms, 2s); default 1s")
	return cmd
}

// ─── execFetch ────────────────────────────────────────────────────────────────

type execFetchParams struct {
	address        string
	radius         float64
	tileSize       float64
	cacheDir       string
	noCache        bool
	clearCache     bool
	purgeOlderThan string
	verbose        bool
	overpassURL    string
	fetchDelay     string
	stderr         io.Writer
}

func execFetch(p execFetchParams) error {
	stderr := p.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	var logger *slog.Logger
	if p.verbose {
		handler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	pipelineStart := time.Now()

	if p.address == "" {
		return fmt.Errorf("--address is required")
	}

	if p.radius <= 0 {
		return fmt.Errorf("--radius must be greater than 0")
	}

	if p.tileSize <= 0 {
		return fmt.Errorf("--tile-size must be greater than 0")
	}

	cacheDir := p.cacheDir
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".twisty", "cache", "overpass")
	}

	cache := &quality.TileCache{
		Dir:       cacheDir,
		Precision: 3,
	}

	// Handle --clear-cache
	if p.clearCache {
		logger.Info("clearing cache", "dir", cacheDir)
		if err := cache.ClearAll(); err != nil {
			return fmt.Errorf("clearing cache: %w", err)
		}
		fmt.Fprintln(stderr, "Cache cleared.")
	}

	// Handle --purge-older-than
	if p.purgeOlderThan != "" {
		dur, err := parseDuration(p.purgeOlderThan)
		if err != nil {
			return fmt.Errorf("parsing --purge-older-than: %w", err)
		}
		count, err := cache.PurgeOlderThan(dur)
		if err != nil {
			return fmt.Errorf("purging cache: %w", err)
		}
		logger.Info("purged old cache files", "count", count, "older_than", p.purgeOlderThan)
		fmt.Fprintf(stderr, "Purged %d old cache file(s).\n", count)
	}

	// Geocode the address
	logger.Info("pipeline start", "mode", "tiled-fetch", "address", p.address)
	done := stageTimer(logger, "geocode-center")
	centerResult, err := geocode.Resolve(p.address, "Center")
	if err != nil {
		return fmt.Errorf("resolving address: %w", err)
	}
	done("result", centerResult.DisplayName)

	// Run tiled fetch
	var progress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !p.verbose && isTerminal(os.Stderr) {
		progress = &termProgressBar{w: os.Stderr, label: "Fetching tiles:"}
	}

	cfg := quality.TileFetchConfig{
		Endpoint: p.overpassURL,
		TileSize: p.tileSize,
		Cache:    cache,
		NoCache:  p.noCache,
		Logger:   logger,
		Progress: progress,
	}
	if p.fetchDelay != "" {
		d, err := time.ParseDuration(p.fetchDelay)
		if err != nil {
			return fmt.Errorf("invalid -fetch-delay %q: %w", p.fetchDelay, err)
		}
		cfg.RateLimitDelay = d
	}

	done = stageTimer(logger, "fetch-tiled-ways")
	ways, err := quality.FetchTiledWays(context.Background(), centerResult.Lat, centerResult.Lon, p.radius, cfg)
	if err != nil {
		return fmt.Errorf("fetching tiled ways: %w", err)
	}
	done("ways", len(ways))

	fmt.Fprintf(stderr, "Tiled fetch complete: %d deduplicated ways found.\n", len(ways))
	logger.Debug("pipeline done", "total_elapsed_ms", time.Since(pipelineStart).Milliseconds())
	return nil
}

func newFetchCmd() *cobra.Command {
	var (
		address        string
		radius         float64
		tileSize       float64
		cacheDir       string
		noCache        bool
		clearCache     bool
		purgeOlderThan string
		verbose        bool
		overpassURL    string
		fetchDelay     string
	)
	cmd := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch and cache Overpass tile data for a region",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execFetch(execFetchParams{
				address:        address,
				radius:         radius,
				tileSize:       tileSize,
				cacheDir:       cacheDir,
				noCache:        noCache,
				clearCache:     clearCache,
				purgeOlderThan: purgeOlderThan,
				verbose:        verbose,
				overpassURL:    overpassURL,
				fetchDelay:     fetchDelay,
				stderr:         cmd.ErrOrStderr(),
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&address, "address", "", "Address for curvature pipeline center point")
	f.Float64Var(&radius, "radius", 25.0, "Search radius in km (max 50)")
	f.Float64Var(&tileSize, "tile-size", 0.1, "Tile size in degrees")
	f.StringVar(&cacheDir, "cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
	f.BoolVar(&noCache, "no-cache", false, "Bypass cache reads (still writes)")
	f.BoolVar(&clearCache, "clear-cache", false, "Delete all cached tiles before fetching")
	f.StringVar(&purgeOlderThan, "purge-older-than", "", "Purge cache files older than duration (e.g., 90d, 6m where m=months not minutes)")
	f.BoolVar(&verbose, "v", false, "Enable verbose timing logs to stderr")
	f.StringVar(&overpassURL, "overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
	f.StringVar(&fetchDelay, "fetch-delay", "", "delay between tile fetches in Go duration format (e.g. 500ms, 2s); default 1s")
	return cmd
}

// ─── execRoute ────────────────────────────────────────────────────────────────

type execRouteParams struct {
	origin      string
	dest        string
	twist       float64
	out         string
	showAll     bool
	verbose     bool
	overpassURL string
	stderr      io.Writer
}

func execRoute(p execRouteParams) error {
	stderr := p.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	var logger *slog.Logger
	if p.verbose {
		handler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	pipelineStart := time.Now()

	if p.origin == "" || p.dest == "" {
		return fmt.Errorf("-origin and -dest are required")
	}

	if p.twist < 0.0 || p.twist > 1.0 {
		return fmt.Errorf("-twist must be between 0.0 and 1.0")
	}

	// Stage 1: Resolve origin
	done := stageTimer(logger, "geocode-origin")
	originResult, err := geocode.Resolve(p.origin, "Origin")
	if err != nil {
		return fmt.Errorf("resolving origin: %w", err)
	}
	done("result", originResult.DisplayName)

	// Sleep between geocoding requests only when both inputs need geocoding
	if geocode.NeedsGeocode(p.origin) && geocode.NeedsGeocode(p.dest) {
		time.Sleep(1 * time.Second)
	}

	// Stage 2: Resolve destination
	done = stageTimer(logger, "geocode-dest")
	destResult, err := geocode.Resolve(p.dest, "Destination")
	if err != nil {
		return fmt.Errorf("resolving destination: %w", err)
	}
	done("result", destResult.DisplayName)

	// Stage 3: Fetch routes from OSRM
	fmt.Fprintln(stderr, "Fetching routes...")
	done = stageTimer(logger, "fetch-routes")
	routes, err := route.FetchRoutes(originResult.ToCoord(), destResult.ToCoord())
	if err != nil {
		return fmt.Errorf("fetching routes: %w", err)
	}
	fmt.Fprintf(stderr, "Found %d route(s)\n", len(routes))
	totalPts := 0
	for _, r := range routes {
		totalPts += len(r.Points)
	}
	done("routes", len(routes), "total_points", totalPts)

	// Stage 4: Score all routes
	done = stageTimer(logger, "score-routes")
	route.ScoreAll(routes)
	done("routes", len(routes))

	// Stage 5: Check road quality (soft failure)
	allPoints := collectAllPoints(routes)
	south, west, north, east := quality.BoundingBox(allPoints, 0.01)
	done = stageTimer(logger, "fetch-ways")
	ways, err := quality.FetchWays(p.overpassURL, south, west, north, east)
	wayCount := len(ways)
	if err != nil {
		fmt.Fprintf(stderr, "WARNING: Overpass API unavailable; road quality filtering skipped: %v\n", err)
		ways = nil
		wayCount = 0
	}
	done("ways", wayCount)

	totalPts = 0
	for _, r := range routes {
		totalPts += len(r.Points)
	}
	done = stageTimer(logger, "apply-quality", "routes", len(routes), "ways", wayCount, "total_points", totalPts)
	quality.ApplyQuality(routes, ways)
	done()

	// Stage 6: Select route by twist factor
	done = stageTimer(logger, "select-route")
	selectedIdx := route.SelectRoute(routes, p.twist)
	done("selected_idx", selectedIdx)

	// Stage 7: Write GPX output
	done = stageTimer(logger, "write-gpx")
	selected := routes[selectedIdx]
	if err := gpx.WriteGPX(p.out, selected.Points, "twisty route"); err != nil {
		return fmt.Errorf("writing GPX: %w", err)
	}
	done("path", p.out)

	// Stage 8: Print summary
	printSummary(routes, selectedIdx, p.out, p.showAll)

	logger.Debug("pipeline done", "total_elapsed_ms", time.Since(pipelineStart).Milliseconds())
	return nil
}

func newRouteCmd() *cobra.Command {
	var (
		origin      string
		dest        string
		twist       float64
		out         string
		showAll     bool
		verbose     bool
		overpassURL string
	)
	cmd := &cobra.Command{
		Use:           "route",
		Short:         "Find a twisty route between two addresses",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execRoute(execRouteParams{
				origin:      origin,
				dest:        dest,
				twist:       twist,
				out:         out,
				showAll:     showAll,
				verbose:     verbose,
				overpassURL: overpassURL,
				stderr:      cmd.ErrOrStderr(),
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&origin, "origin", "", "Origin address or lat,lon")
	f.StringVar(&dest, "dest", "", "Destination address or lat,lon")
	f.Float64Var(&twist, "twist", 0.5, "0.0 = fastest, 1.0 = twistiest")
	f.StringVar(&out, "out", "route.gpx", "Output file path")
	f.BoolVar(&showAll, "show-all", false, "Print candidate comparison table")
	f.BoolVar(&verbose, "v", false, "Enable verbose timing logs to stderr")
	f.StringVar(&overpassURL, "overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
	return cmd
}

// ─── execRandom ───────────────────────────────────────────────────────────────

type execRandomParams struct {
	start       string
	timeDur     time.Duration
	avgSpeed    float64
	nWaypoints  int
	minUnder    int
	maxOver     int
	radiusStep  float64
	maxAttempts int
	out         string
	verbose     bool
	overpassURL string
	cacheDir    string
	tileSize    float64
	noCache     bool
	valhallaURL string
	stderr      io.Writer
}

func execRandom(p execRandomParams) error {
	stderr := p.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if p.start == "" || p.timeDur == 0 {
		return fmt.Errorf("-start and -time are required")
	}

	var logger *slog.Logger
	if p.verbose {
		handler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	cacheDir := p.cacheDir
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".twisty", "cache", "overpass")
	}
	scoreCacheDir := filepath.Join(filepath.Dir(cacheDir), "scores")

	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	scoreCache := &quality.ScoreCache{Dir: scoreCacheDir, Precision: 3}
	if err := scoreCache.EnsureDir(); err != nil {
		return fmt.Errorf("creating score cache dir: %w", err)
	}

	ctx := context.Background()

	// Geocode the start point.
	startResult, err := geocode.Resolve(p.start, "Start")
	if err != nil {
		return fmt.Errorf("geocode: %w", err)
	}
	startCoord := startResult.ToCoord()

	// Derive the search radius.
	timeHours := p.timeDur.Hours()
	radiusKm := waypoint.DeriveRadius(timeHours, p.avgSpeed)
	if p.verbose {
		fmt.Fprintf(stderr, "Derived radius: %.1f km\n", radiusKm)
	}

	// Fetch tiles for the initial radius, then score and aggregate.
	var fetchProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !p.verbose && isTerminal(os.Stderr) {
		fetchProgress = &termProgressBar{w: os.Stderr, label: "Fetching tiles:"}
	}
	cfg := quality.TileFetchConfig{
		Endpoint: p.overpassURL,
		TileSize: p.tileSize,
		Cache:    tileCache,
		NoCache:  p.noCache,
		Logger:   logger,
		Progress: fetchProgress,
	}

	if _, err := quality.FetchTiledWays(ctx, startCoord.Lat, startCoord.Lon, radiusKm, cfg); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	tiles := quality.ComputeTiles(startCoord.Lat, startCoord.Lon, radiusKm, p.tileSize)
	var scoreProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !p.verbose && isTerminal(os.Stderr) {
		scoreProgress = &termProgressBar{w: os.Stderr, label: "Scoring tiles:"}
	}
	collections, _, err := scoreAndAggregateTiles(ctx, tiles, tileCache, scoreCache, p.noCache, logger, scoreProgress)
	if err != nil {
		return fmt.Errorf("score: %w", err)
	}

	// Compute tolerance window.
	targetSec := p.timeDur.Seconds()
	minSec := targetSec - float64(p.minUnder)*60
	maxSec := targetSec + float64(p.maxOver)*60

	// Initialize retry state.
	type attemptResult struct {
		r         route.Route
		waypoints []geo.Coord
		radius    float64
	}

	selector := &waypoint.ChainSelector{
		Start:         startCoord,
		ArcWidth:      120.0,
		AvgSpeedMPH:   p.avgSpeed,
		TimeBudgetSec: targetSec,
	}

	currentRadius := radiusKm
	scoredCollections := collections
	fetchedTiles := quality.ComputeTiles(startCoord.Lat, startCoord.Lon, radiusKm, p.tileSize)
	var bestResult *attemptResult
	lastDirection := ""

	for attempt := 1; attempt <= p.maxAttempts; attempt++ {
		r, wps, err := randomAttempt(scoredCollections, p.nWaypoints, startCoord, currentRadius, selector, p.valhallaURL)
		if err != nil {
			return fmt.Errorf("attempt %d: %w", attempt, err)
		}

		// Track best result.
		if bestResult == nil || math.Abs(r.Duration-targetSec) < math.Abs(bestResult.r.Duration-targetSec) {
			bestResult = &attemptResult{r: r, waypoints: wps, radius: currentRadius}
		}

		// Check tolerance window.
		if r.Duration >= minSec && r.Duration <= maxSec {
			bestResult = &attemptResult{r: r, waypoints: wps, radius: currentRadius}
			break
		}

		if attempt == p.maxAttempts {
			fmt.Fprintf(stderr,
				"Warning: max attempts (%d) reached; using closest result (actual %s)\n",
				p.maxAttempts, formatDuration(bestResult.r.Duration))
			break
		}

		// Determine adjustment direction.
		var direction string
		if r.Duration < minSec {
			direction = "expand"
		} else {
			direction = "contract"
		}

		// Oscillation detection.
		if lastDirection != "" && direction != lastDirection {
			fmt.Fprintf(stderr, "Oscillation detected; accepting best result (%s)\n",
				formatDuration(bestResult.r.Duration))
			break
		}
		lastDirection = direction

		if direction == "expand" {
			newRadius := currentRadius * (1 + p.radiusStep)
			fmt.Fprintf(stderr,
				"Attempt %d: route too short (%s), expanding radius to %.1fkm...\n",
				attempt+1, formatDuration(r.Duration), newRadius)

			// Fetch only the new outer annular ring.
			newTiles := quality.ComputeTiles(startCoord.Lat, startCoord.Lon, newRadius, p.tileSize)
			outerTiles := tileSetDifference(newTiles, fetchedTiles)

			if len(outerTiles) > 0 {
				var expandFetchProgress quality.ProgressReporter = quality.NoopProgressReporter{}
				if !p.verbose && isTerminal(os.Stderr) {
					expandFetchProgress = &termProgressBar{w: os.Stderr, label: "Expanding fetch:"}
				}
				outerCfg := quality.TileFetchConfig{
					Endpoint: p.overpassURL,
					TileSize: p.tileSize,
					Cache:    tileCache,
					NoCache:  p.noCache,
					Logger:   logger,
					Progress: expandFetchProgress,
				}
				if _, err := quality.FetchTiledWaysForTiles(ctx, outerTiles, outerCfg); err != nil {
					return fmt.Errorf("expand fetch: %w", err)
				}
				var expandScoreProgress quality.ProgressReporter = quality.NoopProgressReporter{}
				if !p.verbose && isTerminal(os.Stderr) {
					expandScoreProgress = &termProgressBar{w: os.Stderr, label: "Expanding score:"}
				}
				outerCollections, _, err := scoreAndAggregateTiles(ctx, outerTiles, tileCache, scoreCache, p.noCache, logger, expandScoreProgress)
				if err != nil {
					return fmt.Errorf("expand score: %w", err)
				}
				scoredCollections = append(scoredCollections, outerCollections...)
			}

			fetchedTiles = newTiles
			currentRadius = newRadius

		} else { // contract
			newRadius := currentRadius * (1 - p.radiusStep)
			fmt.Fprintf(stderr,
				"Attempt %d: route too long (%s), contracting radius to %.1fkm...\n",
				attempt+1, formatDuration(r.Duration), newRadius)
			// No tile work needed. Just tighten the effective radius.
			currentRadius = newRadius
		}
	}

	// Write GPX and print summary using best result.
	gpxWaypoints := make([]gpx.Waypoint, 0, len(bestResult.r.Maneuvers))
	for _, m := range bestResult.r.Maneuvers {
		name := m.Instruction
		if len(name) > 40 {
			name = name[:40]
		}
		desc := m.Instruction + " — " + formatDuration(m.TimeSec) + ", " + fmt.Sprintf("%.2fkm", m.LengthKm)
		gpxWaypoints = append(gpxWaypoints, gpx.Waypoint{
			Lat:  m.Location.Lat,
			Lon:  m.Location.Lon,
			Name: name,
			Desc: desc,
		})
	}
	if err := gpx.WriteGPXWithWaypoints(p.out, bestResult.r.Points, gpxWaypoints, "twisty random"); err != nil {
		return fmt.Errorf("write GPX: %w", err)
	}

	// Write waypoints GPX showing what we sent to Valhalla.
	if len(bestResult.waypoints) > 0 {
		wpGPX := make([]gpx.Waypoint, len(bestResult.waypoints))
		for i, wp := range bestResult.waypoints {
			wpGPX[i] = gpx.Waypoint{
				Lat:  wp.Lat,
				Lon:  wp.Lon,
				Name: fmt.Sprintf("WP%d", i+1),
			}
		}
		if err := gpx.WriteGPXWithWaypoints("random_waypoints.gpx", bestResult.waypoints, wpGPX, "waypoints"); err != nil {
			return fmt.Errorf("write waypoints GPX: %w", err)
		}
		fmt.Fprintf(stderr, "Waypoints GPX written to random_waypoints.gpx (%d waypoints)\n", len(bestResult.waypoints))
	}

	distKm := bestResult.r.Distance / 1000
	fmt.Fprintf(stderr, "Route: distance=%.1fkm  duration=%s  angular=%.1f/km  score=%.1f  points=%d\n",
		distKm,
		formatDuration(bestResult.r.Duration),
		bestResult.r.Stats.AngularDensity,
		bestResult.r.Stats.AdjustedScore,
		len(bestResult.r.Points),
	)
	fmt.Fprintf(stderr, "GPX written to %s\n", p.out)
	return nil
}

func newRandomCmd() *cobra.Command {
	var (
		start       string
		timeDur     time.Duration
		avgSpeed    float64
		nWaypoints  int
		minUnder    int
		maxOver     int
		radiusStep  float64
		maxAttempts int
		out         string
		verbose     bool
		overpassURL string
		cacheDir    string
		tileSize    float64
		noCache     bool
		valhallaURL string
	)
	cmd := &cobra.Command{
		Use:           "random",
		Short:         "Generate a random twisty loop route",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execRandom(execRandomParams{
				start:       start,
				timeDur:     timeDur,
				avgSpeed:    avgSpeed,
				nWaypoints:  nWaypoints,
				minUnder:    minUnder,
				maxOver:     maxOver,
				radiusStep:  radiusStep,
				maxAttempts: maxAttempts,
				out:         out,
				verbose:     verbose,
				overpassURL: overpassURL,
				cacheDir:    cacheDir,
				tileSize:    tileSize,
				noCache:     noCache,
				valhallaURL: valhallaURL,
				stderr:      cmd.ErrOrStderr(),
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&start, "start", "", "start/end address or lat,lon (required)")
	f.DurationVar(&timeDur, "time", 0, "target ride duration, e.g. 2h (required)")
	f.Float64Var(&avgSpeed, "avg-speed", waypoint.DefaultAvgSpeedMPH, "average speed in mph")
	f.IntVar(&nWaypoints, "waypoints", 5, "number of intermediate waypoints")
	f.IntVar(&minUnder, "min-under", 15, "acceptable shortfall in minutes")
	f.IntVar(&maxOver, "max-over", 5, "acceptable overage in minutes")
	f.Float64Var(&radiusStep, "radius-step", 0.25, "radius adjustment factor per retry (0 < x < 1)")
	f.IntVar(&maxAttempts, "max-attempts", 3, "maximum Valhalla calls before accepting best result")
	f.StringVar(&out, "out", "random_loop.gpx", "output GPX file path")
	f.BoolVar(&verbose, "v", false, "verbose output")
	f.StringVar(&overpassURL, "overpass-url", quality.OverpassBaseURL, "Overpass API URL")
	f.StringVar(&cacheDir, "cache-dir", "", "tile cache directory (default: ~/.twisty/cache/overpass/)")
	f.Float64Var(&tileSize, "tile-size", 0.1, "tile size in degrees")
	f.BoolVar(&noCache, "no-cache", false, "disable tile and score cache reads")
	f.StringVar(&valhallaURL, "valhalla-url", "https://valhalla1.openstreetmap.de", "Valhalla routing API URL")
	return cmd
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// termProgressBar renders a progress bar to w using carriage-return overwriting.
// It implements quality.ProgressReporter.
//
// All exported methods are safe for concurrent use; an internal mutex serializes
// state updates and rendering. This is required because termProgressBar may be
// passed into processTilesConcurrently, which calls Tick from its collector
// goroutine while SetTotal and Done are called from the calling goroutine.
type termProgressBar struct {
	mu                 sync.Mutex
	w                  io.Writer
	label              string
	total              int
	current            int
	cached             int
	fetched            int
	retries            int
	lastFetchDuration  time.Duration
	totalFetchDuration time.Duration
	fetchCount         int
}

func (b *termProgressBar) SetTotal(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total = n
	b.render()
}

func (b *termProgressBar) Tick(cached bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current++
	if cached {
		b.cached++
	} else {
		b.fetched++
	}
	b.render()
}

func (b *termProgressBar) Done() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.render()
	fmt.Fprintln(b.w)
}

func (b *termProgressBar) Retry() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.retries++
	b.render()
}

func (b *termProgressBar) FetchDuration(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastFetchDuration = d
	b.totalFetchDuration += d
	b.fetchCount++
}

func (b *termProgressBar) render() {
	const width = 30
	filled := 0
	if b.total > 0 {
		filled = width * b.current / b.total
	}
	var bar string
	if filled == 0 {
		bar = strings.Repeat(" ", width)
	} else {
		bar = strings.Repeat("=", filled-1) + ">" + strings.Repeat(" ", width-filled)
	}
	fmt.Fprintf(b.w, "\r%s [%s] %d/%d (%s)",
		b.label, bar, b.current, b.total, b.statsString())
}

func (b *termProgressBar) statsString() string {
	parts := []string{
		fmt.Sprintf("%d cached", b.cached),
		fmt.Sprintf("%d fetched", b.fetched),
	}
	if b.retries > 0 {
		noun := "retries"
		if b.retries == 1 {
			noun = "retry"
		}
		parts = append(parts, fmt.Sprintf("%d %s", b.retries, noun))
	}
	if b.fetchCount > 0 {
		avg := time.Duration(int64(b.totalFetchDuration) / int64(b.fetchCount))
		parts = append(parts, fmt.Sprintf("last: %.1fs", b.lastFetchDuration.Seconds()))
		parts = append(parts, fmt.Sprintf("avg: %.1fs", avg.Seconds()))
	}
	return strings.Join(parts, ", ")
}

// termConvertBar is an in-place terminal progress bar for PBF-to-BZ2 conversion.
// It implements osmconv.ConvertProgress.
type termConvertBar struct {
	mu          sync.Mutex
	w           io.Writer
	fileNames   []string
	fileSizes   []int64
	bytesRead   []int64
	totalSize   int64
	totalRead   int64
	objectCount int
	startTime   time.Time
	lastRender  time.Time
	currentFile int
}

func (b *termConvertBar) SetFiles(names []string, sizes []int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fileNames = names
	b.fileSizes = sizes
	b.bytesRead = make([]int64, len(sizes))
	b.totalSize = 0
	for _, s := range sizes {
		b.totalSize += s
	}
	b.startTime = time.Now()
}

func (b *termConvertBar) BytesRead(fileIndex int, n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if fileIndex >= 0 && fileIndex < len(b.bytesRead) {
		b.bytesRead[fileIndex] += n
		b.currentFile = fileIndex
		b.totalRead += n
	}
	if time.Since(b.lastRender) >= 100*time.Millisecond {
		b.render()
	}
}

func (b *termConvertBar) ObjectsWritten(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objectCount += n
}

func (b *termConvertBar) Done() {
	b.mu.Lock()
	defer b.mu.Unlock()
	elapsed := time.Since(b.startTime)
	fmt.Fprintf(b.w, "\rConversion complete: %s objects written, %s\n",
		formatInt(b.objectCount), formatElapsed(elapsed))
}

func (b *termConvertBar) render() {
	const width = 30
	var pct int
	if b.totalSize > 0 {
		pct = int(b.totalRead * 100 / b.totalSize)
		if pct > 100 {
			pct = 100
		}
	}
	filled := width * pct / 100
	var bar string
	if filled == 0 {
		bar = strings.Repeat(" ", width)
	} else {
		bar = strings.Repeat("=", filled-1) + ">" + strings.Repeat(" ", width-filled)
	}

	label := ""
	total := len(b.fileNames)
	if total > 0 && b.currentFile < total {
		label = fmt.Sprintf("%s (%d/%d)", b.fileNames[b.currentFile], b.currentFile+1, total)
	}

	elapsed := time.Since(b.startTime)
	fmt.Fprintf(b.w, "\rConverting %s: [%s] %d%% | %s objects | %s",
		label, bar, pct, formatInt(b.objectCount), formatElapsed(elapsed))
	b.lastRender = time.Now()
}

// logConvertProgress is a fallback ConvertProgress for non-terminal stderr that
// prints a line every 100,000 objects.
type logConvertProgress struct {
	mu          sync.Mutex
	w           io.Writer
	objectCount int
	lastReport  int
}

func (p *logConvertProgress) SetFiles([]string, []int64) {}
func (p *logConvertProgress) BytesRead(int, int64)       {}

func (p *logConvertProgress) ObjectsWritten(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.objectCount += n
	if p.objectCount-p.lastReport >= 100000 {
		fmt.Fprintf(p.w, "  %s objects written\n", formatInt(p.objectCount))
		p.lastReport = p.objectCount
	}
}

func (p *logConvertProgress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "  %s objects written\n", formatInt(p.objectCount))
}

// newConvertProgress returns a terminal progress bar if w is a terminal,
// otherwise a log-line fallback.
func newConvertProgress(w *os.File) osmconv.ConvertProgress {
	if isTerminal(w) {
		return &termConvertBar{w: w}
	}
	return &logConvertProgress{w: w}
}

// termDownloadBar is an in-place terminal progress bar for PBF file downloads.
// It implements DownloadProgress.
type termDownloadBar struct {
	mu              sync.Mutex
	w               io.Writer
	fileName        string
	fileIndex       int
	totalFiles      int
	totalBytes      int64
	downloaded      int64
	totalDownloaded int64
	startTime       time.Time
	lastRender      time.Time
}

func (b *termDownloadBar) StartFile(name string, fileIndex int, totalFiles int, totalBytes int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fileName = name
	b.fileIndex = fileIndex
	b.totalFiles = totalFiles
	b.totalBytes = totalBytes
	b.downloaded = 0
	if b.startTime.IsZero() {
		b.startTime = time.Now()
	}
}

func (b *termDownloadBar) BytesDownloaded(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.downloaded += n
	b.totalDownloaded += n
	if time.Since(b.lastRender) >= 100*time.Millisecond {
		b.render()
	}
}

func (b *termDownloadBar) FileComplete() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.render()
}

func (b *termDownloadBar) Done() {
	b.mu.Lock()
	defer b.mu.Unlock()
	elapsed := time.Since(b.startTime)
	fmt.Fprintf(b.w, "\rDownloads complete: %d files, %.1f MB, %s\n",
		b.totalFiles, float64(b.totalDownloaded)/(1024*1024), formatElapsed(elapsed))
}

func (b *termDownloadBar) render() {
	const width = 30
	label := fmt.Sprintf("%s (%d/%d)", b.fileName, b.fileIndex+1, b.totalFiles)
	elapsed := time.Since(b.startTime)

	if b.totalBytes < 0 {
		// No Content-Length: show bytes downloaded without percentage.
		fmt.Fprintf(b.w, "\rDownloading %s: %.1f MB | %s",
			label, float64(b.downloaded)/(1024*1024), formatElapsed(elapsed))
		b.lastRender = time.Now()
		return
	}

	var pct int
	if b.totalBytes > 0 {
		pct = int(b.downloaded * 100 / b.totalBytes)
		if pct > 100 {
			pct = 100
		}
	}
	filled := width * pct / 100
	var bar string
	if filled == 0 {
		bar = strings.Repeat(" ", width)
	} else {
		bar = strings.Repeat("=", filled-1) + ">" + strings.Repeat(" ", width-filled)
	}

	downloadedMB := float64(b.downloaded) / (1024 * 1024)
	totalMB := float64(b.totalBytes) / (1024 * 1024)
	fmt.Fprintf(b.w, "\rDownloading %s: [%s] %d%% | %.1f / %.1f MB | %s",
		label, bar, pct, downloadedMB, totalMB, formatElapsed(elapsed))
	b.lastRender = time.Now()
}

var _ DownloadProgress = &termDownloadBar{}

// logDownloadProgress is a fallback DownloadProgress for non-terminal stderr that
// prints a line every 10 MB.
type logDownloadProgress struct {
	mu              sync.Mutex
	w               io.Writer
	downloaded      int64
	lastReport      int64
	totalDownloaded int64
	fileName        string
	totalBytes      int64
	startTime       time.Time
	totalFiles      int
}

func (p *logDownloadProgress) StartFile(name string, _ int, totalFiles int, totalBytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fileName = name
	p.totalBytes = totalBytes
	p.downloaded = 0
	p.lastReport = 0
	p.totalFiles = totalFiles
	if p.startTime.IsZero() {
		p.startTime = time.Now()
	}
}

func (p *logDownloadProgress) BytesDownloaded(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.downloaded += n
	p.totalDownloaded += n
	const tenMB = 10 * 1024 * 1024
	if p.downloaded-p.lastReport >= tenMB {
		p.lastReport = p.downloaded
		downloadedMB := float64(p.downloaded) / (1024 * 1024)
		if p.totalBytes > 0 {
			totalMB := float64(p.totalBytes) / (1024 * 1024)
			pct := int(p.downloaded * 100 / p.totalBytes)
			fmt.Fprintf(p.w, "  downloaded %.1f MB / %.1f MB (%d%%)\n", downloadedMB, totalMB, pct)
		} else {
			fmt.Fprintf(p.w, "  downloaded %.1f MB\n", downloadedMB)
		}
	}
}

func (p *logDownloadProgress) FileComplete() {
	p.mu.Lock()
	defer p.mu.Unlock()
	downloadedMB := float64(p.downloaded) / (1024 * 1024)
	fmt.Fprintf(p.w, "  done (%.1f MB)\n", downloadedMB)
}

func (p *logDownloadProgress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	elapsed := time.Since(p.startTime)
	fmt.Fprintf(p.w, "Downloads complete: %d files, %.1f MB, %s\n",
		p.totalFiles, float64(p.totalDownloaded)/(1024*1024), formatElapsed(elapsed))
}

var _ DownloadProgress = &logDownloadProgress{}

// newDownloadProgress returns a terminal progress bar if w is a terminal,
// otherwise a log-line fallback.
func newDownloadProgress(w *os.File) DownloadProgress {
	if isTerminal(w) {
		return &termDownloadBar{w: w}
	}
	return &logDownloadProgress{w: w}
}

// formatInt formats n with comma separators (e.g. 1,234,567).
func formatInt(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	start := len(s) % 3
	if start == 0 {
		start = 3
	}
	b.WriteString(s[:start])
	for i := start; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// formatElapsed formats a duration as "Xm Ys" or "Xs".
func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// isTerminal reports whether the given file is connected to a terminal.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// parseDuration parses a duration string supporting Nd (days), Nm (months),
// and standard Go durations (e.g., 24h).
func parseDuration(s string) (time.Duration, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration string")
	}

	suffix := s[len(s)-1]
	switch suffix {
	case 'd':
		prefix := s[:len(s)-1]
		n, err := strconv.Atoi(prefix)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: prefix must be a positive integer", s)
		}
		if n <= 0 {
			return 0, fmt.Errorf("invalid duration %q: prefix must be a positive integer", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	case 'm':
		// Treat Nm as months (approximate: 1 month = 30 days).
		// e.g., "6m" → 180 days. Use Go duration parsing for other forms (e.g., "24h").
		// Note: "m" means months here, not minutes. Use "Nh" for hour-based durations.
		prefix := s[:len(s)-1]
		n, err := strconv.Atoi(prefix)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: prefix must be a positive integer (note: 'm' means months, not minutes; use e.g. '24h' for Go durations)", s)
		}
		if n <= 0 {
			return 0, fmt.Errorf("invalid duration %q: prefix must be a positive integer", s)
		}
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	default:
		// Try standard Go duration
		return time.ParseDuration(s)
	}
}

// stageTimer logs the start of a pipeline stage and returns a closure that logs
// the elapsed duration when called with optional extra key-value fields.
func stageTimer(logger *slog.Logger, stage string, fields ...any) func(...any) {
	start := time.Now()
	logger.Debug("stage start", append([]any{"stage", stage}, fields...)...)
	return func(extra ...any) {
		args := append([]any{"stage", stage, "elapsed_ms", time.Since(start).Milliseconds()}, extra...)
		logger.Debug("stage done", args...)
	}
}

// formatDuration formats seconds as "XhYm" or "Ym" if under one hour.
func formatDuration(secs float64) string {
	total := int(secs)
	h := total / 3600
	m := (total % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// randomAttempt runs waypoint selection and FetchLoopRouteFromURL for a single attempt.
// collections is the full scored set; effectiveRadius constrains the selector.
func randomAttempt(
	collections []quality.RoadCollection,
	nWaypoints int,
	start geo.Coord,
	effectiveRadius float64,
	selector waypoint.WaypointSelector,
	valhallaURL string,
) (route.Route, []geo.Coord, error) {
	wps := selector.Select(collections, nWaypoints, start, effectiveRadius)
	r, err := route.FetchLoopRouteFromURL(valhallaURL, start, wps)
	return r, wps, err
}

// tileSetDifference returns tiles in expanded that are not in existing.
func tileSetDifference(expanded, existing []quality.Tile) []quality.Tile {
	seen := make(map[string]bool, len(existing))
	for _, t := range existing {
		seen[tileKey(t)] = true
	}
	var result []quality.Tile
	for _, t := range expanded {
		if !seen[tileKey(t)] {
			result = append(result, t)
		}
	}
	return result
}

func tileKey(t quality.Tile) string {
	return fmt.Sprintf("%.6f,%.6f", t.South, t.West)
}

// collectAllPoints returns all route point slices for use with quality.BoundingBox.
func collectAllPoints(routes []route.Route) [][]geo.Coord {
	all := make([][]geo.Coord, len(routes))
	for i, r := range routes {
		all[i] = r.Points
	}
	return all
}

// printSummary prints the route comparison table (if showAll) and the selected route summary.
func printSummary(routes []route.Route, selectedIdx int, outPath string, showAll bool) {
	if showAll {
		fmt.Println("Route comparison:")
		fmt.Println("--------")
		fmt.Printf("%-8s %-12s %-10s %-16s %s\n", "Route", "Distance", "Duration", "Angular/km", "Twist Score")
		fmt.Println("--------")
		for i, r := range routes {
			distKm := r.Distance / 1000
			durMin := r.Duration / 60
			marker := ""
			if i == selectedIdx {
				marker = " *"
			}
			fmt.Printf("%-8d %-12.1f %-10.0f %-16.1f %.1f%s\n",
				i+1,
				distKm,
				durMin,
				r.Stats.AngularDensity,
				r.Stats.AdjustedScore,
				marker,
			)
		}
		fmt.Println("--------")
		fmt.Println("Distance in km, Duration in minutes")
		fmt.Println()
	}

	selected := routes[selectedIdx]
	distKm := selected.Distance / 1000
	durMin := selected.Duration / 60
	fmt.Printf("Selected route: distance=%.1fkm duration=%.0fmin angular=%.1f/km score=%.1f points=%d\n",
		distKm,
		durMin,
		selected.Stats.AngularDensity,
		selected.Stats.AdjustedScore,
		len(selected.Points),
	)
	fmt.Printf("GPX written to %s\n", outPath)
}

func newGpxCmd() *cobra.Command {
	var mapsURL string
	var outPath string

	cmd := &cobra.Command{
		Use:   "gpx",
		Short: "Export a Google Maps driving route as a GPX file",
		Long: `Export a driving route from a Google Maps shared link as a GPX file
for use in offline navigation applications like OSMAnd.

AUTHENTICATION:

The first time you run this command, you will be prompted to authenticate
with Google via a browser window. Your credentials will be securely stored
for future use (in the system keychain on macOS). You will not need to
re-authenticate for 24 hours under normal use.

FLAGS:

  --maps-url (required)
    Google Maps shared link. Supported formats:
      https://maps.app.goo.gl/abc123
      https://maps.google.com/maps/dir/origin/destination

  --out (required)
    Output file path for the GPX file.

EXAMPLES:

  # Convert a simple route
  twisty gpx --maps-url "https://maps.app.goo.gl/abc123" --out my-route.gpx

  # Convert a route with multiple stops
  twisty gpx \
    --maps-url "https://maps.google.com/maps/dir/Home/Stop1/Work" \
    --out commute.gpx

LIMITATIONS:

  Only driving routes are supported.
  If multiple route options exist in Google Maps, the primary route is used.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGpx(cmd.Context(), mapsURL, outPath)
		},
	}

	cmd.Flags().StringVar(&mapsURL, "maps-url", "", "Google Maps shared link URL (required)")
	cmd.Flags().StringVar(&outPath, "out", "", "Output GPX file path (required)")
	cmd.MarkFlagRequired("maps-url") //nolint:errcheck
	cmd.MarkFlagRequired("out")      //nolint:errcheck

	return cmd
}

func runGpx(ctx context.Context, mapsURL, outPath string) error {
	if builtInGoogleAPIKey == "" {
		return fmt.Errorf("no Google API key configured; rebuild with make build")
	}

	service := gpx.NewService(builtInGoogleAPIKey)

	fmt.Fprintln(os.Stderr, "Converting Google Maps route to GPX...")

	if err := service.ConvertToFile(ctx, mapsURL, outPath); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Route exported: %s\n", outPath)
	return nil
}
