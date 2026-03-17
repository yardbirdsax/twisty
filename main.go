package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/geocode"
	"github.com/yardbirdsax/twisty/gpx"
	"github.com/yardbirdsax/twisty/quality"
	"github.com/yardbirdsax/twisty/route"
)

func main() {
	origin := flag.String("origin", "", "Origin address or lat,lon")
	dest := flag.String("dest", "", "Destination address or lat,lon")
	twist := flag.Float64("twist", 0.5, "0.0 = fastest, 1.0 = twistiest")
	out := flag.String("out", "route.gpx", "Output file path")
	showAll := flag.Bool("show-all", false, "Print candidate comparison table")
	verbose := flag.Bool("v", false, "Enable verbose timing logs to stderr")

	flag.Parse()

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
		flag.Usage()
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
	printSummary(routes, selectedIdx, *twist, *out, *showAll)

	logger.Debug("pipeline done", "total_elapsed_ms", time.Since(pipelineStart).Milliseconds())
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
func printSummary(routes []route.Route, selectedIdx int, twist float64, outPath string, showAll bool) {
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
