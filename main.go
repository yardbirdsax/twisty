package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	origin := flag.String("origin", "", "Origin address or lat,lon")
	dest := flag.String("dest", "", "Destination address or lat,lon")
	twist := flag.Float64("twist", 0.5, "0.0 = fastest, 1.0 = twistiest")
	out := flag.String("out", "route.gpx", "Output file path")
	showAll := flag.Bool("show-all", false, "Print candidate comparison table")

	flag.Parse()

	if *origin == "" || *dest == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "Error: -origin and -dest are required")
		os.Exit(1)
	}

	if *twist < 0.0 || *twist > 1.0 {
		fmt.Fprintln(os.Stderr, "Error: -twist must be between 0.0 and 1.0")
		os.Exit(1)
	}

	_ = out
	_ = showAll

	// Stage 1: Resolve origin
	// Stage 2: Resolve destination
	// Stage 3: Fetch routes from OSRM
	// Stage 4: Decode geometries and score curvature
	// Stage 5: Check road quality (soft failure)
	// Stage 6: Select route by twist factor
	// Stage 7: Write GPX output
	// Stage 8: Print summary
}
