package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/geocode"
	"github.com/yardbirdsax/twisty/gpx"
	"github.com/yardbirdsax/twisty/quality"
	"github.com/yardbirdsax/twisty/route"
	"github.com/yardbirdsax/twisty/waypoint"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: twisty <route|fetch|score|random|overpass> [flags]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "route":
		runRoute(os.Args[2:])
	case "fetch":
		runFetch(os.Args[2:])
	case "score":
		if err := runScore(os.Args[2:], os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "random":
		runRandom(os.Args[2:])
	case "overpass":
		runOverpass(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(1)
	}
}

func runRoute(args []string) {
	fs := flag.NewFlagSet("route", flag.ExitOnError)
	origin := fs.String("origin", "", "Origin address or lat,lon")
	dest := fs.String("dest", "", "Destination address or lat,lon")
	twist := fs.Float64("twist", 0.5, "0.0 = fastest, 1.0 = twistiest")
	out := fs.String("out", "route.gpx", "Output file path")
	showAll := fs.Bool("show-all", false, "Print candidate comparison table")
	verbose := fs.Bool("v", false, "Enable verbose timing logs to stderr")
	overpassURL := fs.String("overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
	fs.Parse(args)

	var logger *slog.Logger
	if *verbose {
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	pipelineStart := time.Now()

	if *origin == "" || *dest == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "Error: -origin and -dest are required")
		os.Exit(1)
	}

	if *twist < 0.0 || *twist > 1.0 {
		fmt.Fprintln(os.Stderr, "Error: -twist must be between 0.0 and 1.0")
		os.Exit(1)
	}

	// Stage 1: Resolve origin
	done := stageTimer(logger, "geocode-origin")
	originResult, err := geocode.Resolve(*origin, "Origin")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving origin: %v\n", err)
		os.Exit(1)
	}
	done("result", originResult.DisplayName)

	// Sleep between geocoding requests only when both inputs need geocoding
	if geocode.NeedsGeocode(*origin) && geocode.NeedsGeocode(*dest) {
		time.Sleep(1 * time.Second)
	}

	// Stage 2: Resolve destination
	done = stageTimer(logger, "geocode-dest")
	destResult, err := geocode.Resolve(*dest, "Destination")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving destination: %v\n", err)
		os.Exit(1)
	}
	done("result", destResult.DisplayName)

	// Stage 3: Fetch routes from OSRM
	fmt.Println("Fetching routes...")
	done = stageTimer(logger, "fetch-routes")
	routes, err := route.FetchRoutes(originResult.ToCoord(), destResult.ToCoord())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching routes: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Found %d route(s)\n", len(routes))
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
	ways, err := quality.FetchWays(*overpassURL, south, west, north, east)
	wayCount := len(ways)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Overpass API unavailable; road quality filtering skipped: %v\n", err)
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
	selectedIdx := route.SelectRoute(routes, *twist)
	done("selected_idx", selectedIdx)

	// Stage 7: Write GPX output
	done = stageTimer(logger, "write-gpx")
	selected := routes[selectedIdx]
	if err := gpx.WriteGPX(*out, selected.Points, "twisty route"); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing GPX: %v\n", err)
		os.Exit(1)
	}
	done("path", *out)

	// Stage 8: Print summary
	printSummary(routes, selectedIdx, *out, *showAll)

	logger.Debug("pipeline done", "total_elapsed_ms", time.Since(pipelineStart).Milliseconds())
}

func runFetch(args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	address := fs.String("address", "", "Address for curvature pipeline center point")
	radius := fs.Float64("radius", 25.0, "Search radius in km (max 50)")
	tileSize := fs.Float64("tile-size", 0.1, "Tile size in degrees")
	cacheDir := fs.String("cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
	noCache := fs.Bool("no-cache", false, "Bypass cache reads (still writes)")
	clearCache := fs.Bool("clear-cache", false, "Delete all cached tiles before fetching")
	purgeOlderThan := fs.String("purge-older-than", "", "Purge cache files older than duration (e.g., 90d, 6m where m=months not minutes)")
	verbose := fs.Bool("v", false, "Enable verbose timing logs to stderr")
	overpassURL := fs.String("overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
	fetchDelay := fs.String("fetch-delay", "", "delay between tile fetches in Go duration format (e.g. 500ms, 2s); default 1s")
	fs.Parse(args)

	var logger *slog.Logger
	if *verbose {
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	pipelineStart := time.Now()

	if *address == "" {
		fmt.Fprintln(os.Stderr, "Error: --address is required")
		os.Exit(1)
	}

	if *radius <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --radius must be greater than 0")
		os.Exit(1)
	}

	if *tileSize <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --tile-size must be greater than 0")
		os.Exit(1)
	}

	if *cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("cannot determine home directory: %v", err)
		}
		*cacheDir = filepath.Join(home, ".twisty", "cache", "overpass")
	}

	cache := &quality.TileCache{
		Dir:       *cacheDir,
		Precision: 3,
	}

	// Handle --clear-cache
	if *clearCache {
		logger.Info("clearing cache", "dir", *cacheDir)
		if err := cache.ClearAll(); err != nil {
			fmt.Fprintf(os.Stderr, "Error clearing cache: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Cache cleared.")
	}

	// Handle --purge-older-than
	if *purgeOlderThan != "" {
		dur, err := parseDuration(*purgeOlderThan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing --purge-older-than: %v\n", err)
			os.Exit(1)
		}
		count, err := cache.PurgeOlderThan(dur)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error purging cache: %v\n", err)
			os.Exit(1)
		}
		logger.Info("purged old cache files", "count", count, "older_than", *purgeOlderThan)
		fmt.Printf("Purged %d old cache file(s).\n", count)
	}

	// Geocode the address
	logger.Info("pipeline start", "mode", "tiled-fetch", "address", *address)
	done := stageTimer(logger, "geocode-center")
	centerResult, err := geocode.Resolve(*address, "Center")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving address: %v\n", err)
		os.Exit(1)
	}
	done("result", centerResult.DisplayName)

	// Run tiled fetch
	var progress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !*verbose && isTerminal(os.Stderr) {
		progress = &termProgressBar{w: os.Stderr, label: "Fetching tiles:"}
	}

	cfg := quality.TileFetchConfig{
		Endpoint: *overpassURL,
		TileSize: *tileSize,
		Cache:    cache,
		NoCache:  *noCache,
		Logger:   logger,
		Progress: progress,
	}
	if *fetchDelay != "" {
		d, err := time.ParseDuration(*fetchDelay)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid -fetch-delay %q: %v\n", *fetchDelay, err)
			os.Exit(1)
		}
		cfg.RateLimitDelay = d
	}

	done = stageTimer(logger, "fetch-tiled-ways")
	ways, err := quality.FetchTiledWays(context.Background(), centerResult.Lat, centerResult.Lon, *radius, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching tiled ways: %v\n", err)
		os.Exit(1)
	}
	done("ways", len(ways))

	fmt.Printf("Tiled fetch complete: %d deduplicated ways found.\n", len(ways))
	logger.Debug("pipeline done", "total_elapsed_ms", time.Since(pipelineStart).Milliseconds())
}

func runScore(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	fs.SetOutput(stderr)
	address := fs.String("address", "", "Location center address")
	radius := fs.Float64("radius", 25.0, "Search radius in km (max 50)")
	tileSize := fs.Float64("tile-size", 0.1, "Tile size in degrees")
	cacheDir := fs.String("cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
	noCache := fs.Bool("no-cache", false, "Skip score cache reads (still writes)")
	clearScoreCache := fs.Bool("clear-score-cache", false, "Delete all score cache entries before running")
	verbose := fs.Bool("v", false, "Enable verbose logging to stderr")
	outPath := fs.String("out", "", "output KML file path (required)")
	minScore := fs.Float64("min-score", 0, "minimum penalized score (after highway-type penalties) to include in output; "+
		"in single-color mode roads graduate green→yellow→red→magenta over scores 0–8000; "+
		"in multi-color mode colors reflect per-segment curve tightness (green=straight, red=tightest) regardless of this threshold")
	multiColor := fs.Bool("multi-color", false, "Use per-segment tier coloring instead of single-color per-road")
	overpassURL := fs.String("overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
	fetchDelay := fs.String("fetch-delay", "", "delay between tile fetches in Go duration format (e.g. 500ms, 2s); default 1s")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var logger *slog.Logger
	if *verbose {
		handler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	if *address == "" {
		fs.Usage()
		return fmt.Errorf("Error: -address is required")
	}

	if *outPath == "" {
		fs.Usage()
		return fmt.Errorf("Error: -out is required")
	}

	if *radius <= 0 {
		return fmt.Errorf("Error: -radius must be > 0 km")
	}

	if *tileSize <= 0 {
		return fmt.Errorf("Error: -tile-size must be greater than 0")
	}

	if *cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		*cacheDir = filepath.Join(home, ".twisty", "cache", "overpass")
	}

	scoreCacheDir := filepath.Join(filepath.Dir(*cacheDir), "scores")

	tileCache := &quality.TileCache{
		Dir:       *cacheDir,
		Precision: 3,
	}
	scoreCache := &quality.ScoreCache{
		Dir:       scoreCacheDir,
		Precision: 3,
	}

	if err := scoreCache.EnsureDir(); err != nil {
		return fmt.Errorf("Error creating score cache dir: %w", err)
	}

	if *clearScoreCache {
		logger.Info("clearing score cache", "dir", scoreCacheDir)
		if err := scoreCache.ClearAll(); err != nil {
			return fmt.Errorf("Error clearing score cache: %w", err)
		}
		fmt.Fprintln(stderr, "Score cache cleared.")
	}

	// Geocode the address.
	centerResult, err := geocode.Resolve(*address, "Center")
	if err != nil {
		return fmt.Errorf("Error resolving address: %w", err)
	}
	logger.Debug("geocoded address", "display_name", centerResult.DisplayName)

	// Fetch any missing tiles before scoring.
	var fetchProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !*verbose && isTerminal(os.Stderr) {
		fetchProgress = &termProgressBar{w: os.Stderr, label: "Fetching tiles:"}
	}
	fetchCfg := quality.TileFetchConfig{
		Endpoint: *overpassURL,
		TileSize: *tileSize,
		Cache:    tileCache,
		Logger:   logger,
		Progress: fetchProgress,
	}
	if *fetchDelay != "" {
		d, err := time.ParseDuration(*fetchDelay)
		if err != nil {
			return fmt.Errorf("invalid -fetch-delay %q: %w", *fetchDelay, err)
		}
		fetchCfg.RateLimitDelay = d
	}
	if _, err := quality.FetchTiledWays(context.Background(), centerResult.Lat, centerResult.Lon, *radius, fetchCfg); err != nil {
		return fmt.Errorf("fetching tiles: %w", err)
	}

	// Compute tiles.
	tiles := quality.ComputeTiles(centerResult.Lat, centerResult.Lon, *radius, *tileSize)
	logger.Debug("computed tiles", "count", len(tiles))

	totalTiles := len(tiles)

	// Phase A + B: concurrent tile scoring, name-group aggregation, and penalties.
	var scoreProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !*verbose && isTerminal(os.Stderr) {
		scoreProgress = &termProgressBar{w: os.Stderr, label: "Scoring tiles:"}
	}
	collections, stats, err := scoreAndAggregateTiles(
		context.Background(),
		tiles,
		tileCache,
		scoreCache,
		*noCache,
		logger,
		scoreProgress,
	)
	if err != nil {
		return fmt.Errorf("processing tiles: %w", err)
	}

	if len(collections) == 0 {
		fmt.Fprintln(stderr, "WARNING: No road data found for this area. Writing empty KML.")
		f, err := os.Create(*outPath)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer f.Close()
		if *multiColor {
			return quality.WriteKML(f, nil, *minScore)
		}
		return quality.WriteKMLSingleColor(f, nil, *minScore)
	}

	fmt.Fprintln(stderr, "Score complete.")
	fmt.Fprintf(stderr, "  Tiles processed: %d\n", totalTiles)
	fmt.Fprintf(stderr, "  Cache hits:       %d\n", stats.cacheHits.Load())
	fmt.Fprintf(stderr, "  Ways scored:      %d\n", stats.totalWays.Load())
	fmt.Fprintf(stderr, "  Segments scored:  %d\n", stats.totalSegs.Load())

	// Stage 7: KML Output
	f, err := os.Create(*outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	if *multiColor {
		if err := quality.WriteKML(f, collections, *minScore); err != nil {
			return fmt.Errorf("writing KML: %w", err)
		}
	} else {
		if err := quality.WriteKMLSingleColor(f, collections, *minScore); err != nil {
			return fmt.Errorf("writing KML: %w", err)
		}
	}

	// Count collections that pass both the min-score and min-length filters.
	inOutput := 0
	for _, c := range collections {
		if c.PenalizedScore >= *minScore && c.TotalLength >= quality.MinRoadLengthM {
			inOutput++
		}
	}

	if inOutput == 0 {
		fmt.Fprintf(stderr, "WARNING: No road collections passed the min-score (%.0f) and min-length (%.0f m) filters. KML output is empty.\n", *minScore, quality.MinRoadLengthM)
	}

	fmt.Fprintf(stderr, "Aggregated %d road collections (%d in output after min-score %.0f and min-length %.0f m filters).\n", len(collections), inOutput, *minScore, quality.MinRoadLengthM)

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

	fmt.Fprintf(stderr, "Output: %s\n", *outPath)
	return nil
}

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

func runRandom(args []string) {
	fs := flag.NewFlagSet("random", flag.ExitOnError)

	start := fs.String("start", "", "start/end address or lat,lon (required)")
	timeDur := fs.Duration("time", 0, "target ride duration, e.g. 2h (required)")
	avgSpeed := fs.Float64("avg-speed", waypoint.DefaultAvgSpeedMPH, "average speed in mph")
	nWaypoints := fs.Int("waypoints", 5, "number of intermediate waypoints")
	minUnder := fs.Int("min-under", 15, "acceptable shortfall in minutes")
	maxOver := fs.Int("max-over", 5, "acceptable overage in minutes")
	radiusStep := fs.Float64("radius-step", 0.25, "radius adjustment factor per retry (0 < x < 1)")
	maxAttempts := fs.Int("max-attempts", 3, "maximum Valhalla calls before accepting best result")
	out := fs.String("out", "random_loop.gpx", "output GPX file path")
	verbose := fs.Bool("v", false, "verbose output")
	overpassURL := fs.String("overpass-url", quality.OverpassBaseURL, "Overpass API URL")
	cacheDir := fs.String("cache-dir", "", "tile cache directory (default: ~/.twisty/cache/overpass/)")
	tileSize := fs.Float64("tile-size", 0.1, "tile size in degrees")
	noCache := fs.Bool("no-cache", false, "disable tile and score cache reads")
	valhallaURL := fs.String("valhalla-url", "https://valhalla1.openstreetmap.de", "Valhalla routing API URL")

	fs.Parse(args)

	if *start == "" || *timeDur == 0 {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "Error: -start and -time are required")
		os.Exit(1)
	}

	var logger *slog.Logger
	if *verbose {
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger = slog.New(handler)
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	if *cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("cannot determine home directory: %v", err)
		}
		*cacheDir = filepath.Join(home, ".twisty", "cache", "overpass")
	}
	scoreCacheDir := filepath.Join(filepath.Dir(*cacheDir), "scores")

	tileCache := &quality.TileCache{Dir: *cacheDir, Precision: 3}
	scoreCache := &quality.ScoreCache{Dir: scoreCacheDir, Precision: 3}
	if err := scoreCache.EnsureDir(); err != nil {
		log.Fatalf("create score cache dir: %v", err)
	}

	ctx := context.Background()

	// Geocode the start point.
	startResult, err := geocode.Resolve(*start, "Start")
	if err != nil {
		log.Fatalf("geocode: %v", err)
	}
	startCoord := startResult.ToCoord()

	// Derive the search radius.
	timeHours := timeDur.Hours()
	radiusKm := waypoint.DeriveRadius(timeHours, *avgSpeed)
	if *verbose {
		fmt.Fprintf(os.Stderr, "Derived radius: %.1f km\n", radiusKm)
	}

	// Fetch tiles for the initial radius, then score and aggregate.
	var fetchProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !*verbose && isTerminal(os.Stderr) {
		fetchProgress = &termProgressBar{w: os.Stderr, label: "Fetching tiles:"}
	}
	cfg := quality.TileFetchConfig{
		Endpoint: *overpassURL,
		TileSize: *tileSize,
		Cache:    tileCache,
		NoCache:  *noCache,
		Logger:   logger,
		Progress: fetchProgress,
	}

	if _, err := quality.FetchTiledWays(ctx, startCoord.Lat, startCoord.Lon, radiusKm, cfg); err != nil {
		log.Fatalf("fetch: %v", err)
	}
	tiles := quality.ComputeTiles(startCoord.Lat, startCoord.Lon, radiusKm, *tileSize)
	var scoreProgress quality.ProgressReporter = quality.NoopProgressReporter{}
	if !*verbose && isTerminal(os.Stderr) {
		scoreProgress = &termProgressBar{w: os.Stderr, label: "Scoring tiles:"}
	}
	collections, _, err := scoreAndAggregateTiles(ctx, tiles, tileCache, scoreCache, *noCache, logger, scoreProgress)
	if err != nil {
		log.Fatalf("score: %v", err)
	}

	// Compute tolerance window.
	targetSec := timeDur.Seconds()
	minSec := targetSec - float64(*minUnder)*60
	maxSec := targetSec + float64(*maxOver)*60

	// Initialize retry state.
	type attemptResult struct {
		r         route.Route
		waypoints []geo.Coord
		radius    float64
	}

	selector := &waypoint.ChainSelector{
		Start:         startCoord,
		ArcWidth:      120.0,
		AvgSpeedMPH:   *avgSpeed,
		TimeBudgetSec: targetSec,
	}

	currentRadius := radiusKm
	scoredCollections := collections
	fetchedTiles := quality.ComputeTiles(startCoord.Lat, startCoord.Lon, radiusKm, *tileSize)
	var bestResult *attemptResult
	lastDirection := ""

	for attempt := 1; attempt <= *maxAttempts; attempt++ {
		r, wps, err := randomAttempt(scoredCollections, *nWaypoints, startCoord, currentRadius, selector, *valhallaURL)
		if err != nil {
			log.Fatalf("attempt %d: %v", attempt, err)
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

		if attempt == *maxAttempts {
			fmt.Fprintf(os.Stderr,
				"Warning: max attempts (%d) reached; using closest result (actual %s)\n",
				*maxAttempts, formatDuration(bestResult.r.Duration))
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
			fmt.Fprintf(os.Stderr, "Oscillation detected; accepting best result (%s)\n",
				formatDuration(bestResult.r.Duration))
			break
		}
		lastDirection = direction

		if direction == "expand" {
			newRadius := currentRadius * (1 + *radiusStep)
			fmt.Fprintf(os.Stderr,
				"Attempt %d: route too short (%s), expanding radius to %.1fkm…\n",
				attempt+1, formatDuration(r.Duration), newRadius)

			// Fetch only the new outer annular ring.
			newTiles := quality.ComputeTiles(startCoord.Lat, startCoord.Lon, newRadius, *tileSize)
			outerTiles := tileSetDifference(newTiles, fetchedTiles)

			if len(outerTiles) > 0 {
				var expandFetchProgress quality.ProgressReporter = quality.NoopProgressReporter{}
				if !*verbose && isTerminal(os.Stderr) {
					expandFetchProgress = &termProgressBar{w: os.Stderr, label: "Expanding fetch:"}
				}
				outerCfg := quality.TileFetchConfig{
					Endpoint: *overpassURL,
					TileSize: *tileSize,
					Cache:    tileCache,
					NoCache:  *noCache,
					Logger:   logger,
					Progress: expandFetchProgress,
				}
				if _, err := quality.FetchTiledWaysForTiles(ctx, outerTiles, outerCfg); err != nil {
					log.Fatalf("expand fetch: %v", err)
				}
				var expandScoreProgress quality.ProgressReporter = quality.NoopProgressReporter{}
				if !*verbose && isTerminal(os.Stderr) {
					expandScoreProgress = &termProgressBar{w: os.Stderr, label: "Expanding score:"}
				}
				outerCollections, _, err := scoreAndAggregateTiles(ctx, outerTiles, tileCache, scoreCache, *noCache, logger, expandScoreProgress)
				if err != nil {
					log.Fatalf("expand score: %v", err)
				}
				scoredCollections = append(scoredCollections, outerCollections...)
			}

			fetchedTiles = newTiles
			currentRadius = newRadius

		} else { // contract
			newRadius := currentRadius * (1 - *radiusStep)
			fmt.Fprintf(os.Stderr,
				"Attempt %d: route too long (%s), contracting radius to %.1fkm…\n",
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
	if err := gpx.WriteGPXWithWaypoints(*out, bestResult.r.Points, gpxWaypoints, "twisty random"); err != nil {
		log.Fatalf("write GPX: %v", err)
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
			log.Fatalf("write waypoints GPX: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Waypoints GPX written to random_waypoints.gpx (%d waypoints)\n", len(bestResult.waypoints))
	}

	distKm := bestResult.r.Distance / 1000
	fmt.Printf("Route: distance=%.1fkm  duration=%s  angular=%.1f/km  score=%.1f  points=%d\n",
		distKm,
		formatDuration(bestResult.r.Duration),
		bestResult.r.Stats.AngularDensity,
		bestResult.r.Stats.AdjustedScore,
		len(bestResult.r.Points),
	)
	fmt.Printf("GPX written to %s\n", *out)
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
