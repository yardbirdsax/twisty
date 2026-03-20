package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/geocode"
	"github.com/yardbirdsax/twisty/gpx"
	"github.com/yardbirdsax/twisty/quality"
	"github.com/yardbirdsax/twisty/route"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: twisty <route|fetch|score> [flags]")
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
	ways, err := quality.FetchWays(south, west, north, east)
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
	tileSize := fs.Float64("tile-size", 0.05, "Tile size in degrees")
	cacheDir := fs.String("cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
	noCache := fs.Bool("no-cache", false, "Bypass cache reads (still writes)")
	clearCache := fs.Bool("clear-cache", false, "Delete all cached tiles before fetching")
	purgeOlderThan := fs.String("purge-older-than", "", "Purge cache files older than duration (e.g., 90d, 6m where m=months not minutes)")
	verbose := fs.Bool("v", false, "Enable verbose timing logs to stderr")
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

	if *radius <= 0 || *radius > 50 {
		fmt.Fprintln(os.Stderr, "Error: --radius must be between 0 and 50 km")
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
		progress = &termProgressBar{w: os.Stderr}
	}

	cfg := quality.TileFetchConfig{
		TileSize: *tileSize,
		Cache:    cache,
		NoCache:  *noCache,
		Logger:   logger,
		Progress: progress,
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
	tileSize := fs.Float64("tile-size", 0.05, "Tile size in degrees")
	cacheDir := fs.String("cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
	noCache := fs.Bool("no-cache", false, "Skip score cache reads (still writes)")
	clearScoreCache := fs.Bool("clear-score-cache", false, "Delete all score cache entries before running")
	verbose := fs.Bool("v", false, "Enable verbose logging to stderr")
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

	if *radius <= 0 || *radius > 50 {
		return fmt.Errorf("Error: -radius must be between 0 and 50 km")
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
		fmt.Fprintln(os.Stdout, "Score cache cleared.")
	}

	// Geocode the address.
	centerResult, err := geocode.Resolve(*address, "Center")
	if err != nil {
		return fmt.Errorf("Error resolving address: %w", err)
	}
	logger.Debug("geocoded address", "display_name", centerResult.DisplayName)

	// Compute tiles.
	tiles := quality.ComputeTiles(centerResult.Lat, centerResult.Lon, *radius, *tileSize)
	logger.Debug("computed tiles", "count", len(tiles))

	// Aggregate stats.
	totalTiles := len(tiles)
	cacheHits := 0
	totalWaysScored := 0
	totalSegments := 0
	totalZeroed := 0
	tilesWithData := 0

	for _, tile := range tiles {
		if !tileCache.Has(tile) {
			logger.Warn("no raw tile data, skipping", "south", tile.South, "west", tile.West)
			continue
		}

		rawData, err := tileCache.Read(tile)
		if err != nil {
			logger.Warn("failed to read tile data, skipping", "south", tile.South, "west", tile.West, "error", err)
			continue
		}

		if !*noCache {
			if cachedWays, zeroed, hit := scoreCache.Read(tile, rawData); hit {
				logger.Debug("score cache hit", "south", tile.South, "west", tile.West)
				cacheHits++
				tilesWithData++
				// Accumulate stats from cached ways.
				for _, w := range cachedWays {
					totalSegments += len(w.Segments)
				}
				totalWaysScored += len(cachedWays)
				totalZeroed += zeroed
				continue
			}
		}

		ways, err := quality.ParseTileData(rawData)
		if err != nil {
			logger.Warn("failed to parse tile data, skipping", "south", tile.South, "west", tile.West, "error", err)
			continue
		}
		tilesWithData++

		result := quality.RunScorePipeline(ways)

		if err := scoreCache.Write(tile, rawData, result.ScoredWays, result.ZeroedByDeflection); err != nil {
			logger.Warn("failed to write score cache", "south", tile.South, "west", tile.West, "error", err)
		}

		totalWaysScored += len(result.ScoredWays)
		totalSegments += result.TotalSegments
		totalZeroed += result.ZeroedByDeflection
	}

	if tilesWithData == 0 {
		return fmt.Errorf("No cached tile data found. Run 'twisty fetch -address \"...\"' first.")
	}

	fmt.Println("Score complete.")
	fmt.Printf("  Tiles processed: %d\n", totalTiles)
	fmt.Printf("  Cache hits:       %d\n", cacheHits)
	fmt.Printf("  Ways scored:      %d\n", totalWaysScored)
	fmt.Printf("  Segments scored:  %d\n", totalSegments)
	fmt.Printf("  Zeroed by deflection: %d\n", totalZeroed)
	return nil
}

// termProgressBar renders a progress bar to w using carriage-return overwriting.
// It implements quality.ProgressReporter.
type termProgressBar struct {
	w                  io.Writer
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
	b.total = n
	b.render()
}

func (b *termProgressBar) Tick(cached bool) {
	b.current++
	if cached {
		b.cached++
	} else {
		b.fetched++
	}
	b.render()
}

func (b *termProgressBar) Done() {
	b.render()
	fmt.Fprintln(b.w)
}

func (b *termProgressBar) Retry() {
	b.retries++
	b.render()
}

func (b *termProgressBar) FetchDuration(d time.Duration) {
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
	fmt.Fprintf(b.w, "\rFetching tiles: [%s] %d/%d (%s)",
		bar, b.current, b.total, b.statsString())
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
