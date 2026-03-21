// Command diagkml renders raw OSM ways from the Overpass cache as a KML file.
// Each way is shown as a separate labeled polyline with rotating colors, making
// it easy to see overlapping ways, gaps, and dual-carriageway pairs.
//
// Usage:
//
//	go run ./diag/cmd/diagkml -ref "PA 901" -out roads.kml
//	go run ./diag/cmd/diagkml -ref "PA 901" -radius 15 -center "40.6906,-76.2622" -out roads.kml
//	go run ./diag/cmd/diagkml -name "Sunbury Road" -out roads.kml
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/yardbirdsax/twisty/diag"
	"github.com/yardbirdsax/twisty/quality"
)

func main() {
	ref := flag.String("ref", "", "OSM ref tag to match (e.g. 'PA 901')")
	name := flag.String("name", "", "OSM name tag to match (e.g. 'Sunbury Road')")
	center := flag.String("center", "", "Center point as 'lat,lon' (limits to tiles in radius)")
	radius := flag.Float64("radius", 0, "Search radius in km (requires -center)")
	outPath := flag.String("out", "diag_ways.kml", "Output KML file path")
	flag.Parse()

	if *ref == "" && *name == "" {
		fmt.Fprintln(os.Stderr, "Error: -ref or -name is required")
		flag.Usage()
		os.Exit(1)
	}

	cfg := diag.DefaultCacheConfig()

	// Determine which tiles to search.
	var tiles []quality.Tile
	var err error

	if *center != "" && *radius > 0 {
		lat, lon, parseErr := parseLatLon(*center)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "Error parsing -center: %v\n", parseErr)
			os.Exit(1)
		}
		coverage := diag.TileCoverage(lat, lon, *radius, 0.05, cfg)
		tiles = coverage.Present
		fmt.Fprintf(os.Stderr, "Using %d tiles (%.0f km radius from %.4f, %.4f)\n",
			len(tiles), *radius, lat, lon)
	} else {
		tiles, err = diag.AllCachedTiles(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing cached tiles: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Using all %d cached tiles\n", len(tiles))
	}

	// Build match predicate.
	var match func(quality.Way) bool
	var label string
	switch {
	case *ref != "":
		label = "ref=" + *ref
		match = diag.WayMatchesRef(*ref)
	case *name != "":
		label = "name=" + *name
		match = func(w quality.Way) bool {
			return strings.Contains(w.Tags["name"], *name)
		}
	}

	// Find matching ways.
	matches, err := diag.FindWaysInOverpassCache(tiles, cfg, match)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error searching cache: %v\n", err)
		os.Exit(1)
	}
	deduped := diag.DeduplicateWayMatches(matches)

	if len(deduped) == 0 {
		fmt.Fprintf(os.Stderr, "No ways found matching %s\n", label)
		os.Exit(0)
	}

	// Order geographically for consistent coloring.
	ways := make([]quality.Way, len(deduped))
	for i, m := range deduped {
		ways[i] = m.Way
	}
	ways = diag.OrderWaysByGeography(ways)

	fmt.Fprintf(os.Stderr, "Found %d ways matching %s\n", len(ways), label)

	scoredResult := quality.RunScorePipeline(ways)
	orderedWays := quality.OrderWays((scoredResult.ScoredWays))
	splitWays := quality.SplitOrderingGaps(orderedWays, 100)
	finalWays := make(quality.ScoredWays, 0)
	for _, s := range splitWays {
		finalWays = append(finalWays, s...)
	}

	// Write KML.
	f, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if err := diag.WriteRawWaysKML(f, finalWays.ToWays(quality.NewWaysFromSlice(ways))); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing KML: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Wrote %s\n", *outPath)
}

func parseLatLon(s string) (lat, lon float64, err error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected 'lat,lon', got %q", s)
	}
	lat, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid latitude: %w", err)
	}
	lon, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid longitude: %w", err)
	}
	return lat, lon, nil
}
