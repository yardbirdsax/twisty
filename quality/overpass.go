package quality

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/route"
)

// Way represents an OSM highway way with tags and geometry.
type Way struct {
	Tags     map[string]string
	Geometry []geo.Coord
}

// BoundingBox returns (south, west, north, east) covering all points in all routes,
// expanded by bufferDeg on each side.
func BoundingBox(routes [][]geo.Coord, bufferDeg float64) (south, west, north, east float64) {
	south = math.MaxFloat64
	west = math.MaxFloat64
	north = -math.MaxFloat64
	east = -math.MaxFloat64

	for _, pts := range routes {
		for _, p := range pts {
			if p.Lat < south {
				south = p.Lat
			}
			if p.Lat > north {
				north = p.Lat
			}
			if p.Lon < west {
				west = p.Lon
			}
			if p.Lon > east {
				east = p.Lon
			}
		}
	}

	south -= bufferDeg
	west -= bufferDeg
	north += bufferDeg
	east += bufferDeg
	return
}

// overpassResponse is the top-level JSON returned by Overpass API.
type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

// overpassElement represents a single way element from Overpass.
type overpassElement struct {
	Tags     map[string]string   `json:"tags"`
	Geometry []overpassGeomPoint `json:"geometry"`
}

// overpassGeomPoint is a lat/lon node in the Overpass geometry output.
type overpassGeomPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

var overpassHTTPClient = &http.Client{Timeout: 10 * time.Second}

const overpassBaseURL = "https://overpass-api.de/api/interpreter"

// fetchWaysFromURL is the internal implementation, accepting a base URL so tests
// can substitute a local server.
func fetchWaysFromURL(endpoint string, south, west, north, east float64) ([]Way, error) {
	query := fmt.Sprintf(
		`[out:json][timeout:10];way["highway"](%.6f,%.6f,%.6f,%.6f);out geom;`,
		south, west, north, east,
	)

	body := url.Values{}
	body.Set("data", query)

	resp, err := overpassHTTPClient.Post(
		endpoint,
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("overpass request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("overpass unexpected HTTP status: %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading overpass response: %w", err)
	}

	var oResp overpassResponse
	if err := json.Unmarshal(raw, &oResp); err != nil {
		return nil, fmt.Errorf("parsing overpass response: %w", err)
	}

	ways := make([]Way, 0, len(oResp.Elements))
	for _, el := range oResp.Elements {
		geom := make([]geo.Coord, 0, len(el.Geometry))
		for _, pt := range el.Geometry {
			geom = append(geom, geo.Coord{Lat: pt.Lat, Lon: pt.Lon})
		}
		ways = append(ways, Way{
			Tags:     el.Tags,
			Geometry: geom,
		})
	}
	return ways, nil
}

// FetchWays queries Overpass for all highway ways within the bounding box.
// Returns (ways, nil) on success, or (nil, error) on any failure.
func FetchWays(south, west, north, east float64) ([]Way, error) {
	return fetchWaysFromURL(overpassBaseURL, south, west, north, east)
}

// IsDisqualifying returns true if the way's tags indicate the route segment
// should be heavily penalized (private, restricted, or non-motor-vehicle road).
func IsDisqualifying(tags map[string]string) bool {
	switch tags["access"] {
	case "private", "no":
		return true
	}
	switch tags["highway"] {
	case "track", "path", "footway", "cycleway":
		return true
	}
	switch tags["motor_vehicle"] {
	case "no", "private":
		return true
	}
	return false
}

// PenaltyFactor returns a soft penalty factor in [0, 1] for unpaved or
// low-quality surfaces. 0 = no penalty, 1 = full soft penalty.
func PenaltyFactor(tags map[string]string) float64 {
	surface := tags["surface"]
	switch surface {
	case "unpaved", "gravel", "dirt", "mud", "sand":
		return 1.0
	case "compacted", "fine_gravel":
		return 0.5
	}

	highway := tags["highway"]
	if highway == "unclassified" && surface == "" {
		return 0.25
	}
	if highway == "service" {
		return 0.25
	}
	return 0.0
}

// NearestWay returns the Way whose geometry has a node within maxDist meters
// of point p, or nil if none is found.
func NearestWay(p geo.Coord, ways []Way, maxDist float64) *Way {
	var best *Way
	bestDist := math.MaxFloat64

	for i := range ways {
		for _, node := range ways[i].Geometry {
			d := geo.Haversine(p, node)
			if d < bestDist {
				bestDist = d
				best = &ways[i]
			}
		}
	}

	if bestDist <= maxDist {
		return best
	}
	return nil
}

// ApplyQuality checks each route against the fetched ways and updates
// route.Stats.AdjustedScore. It also prints warnings for heavily disqualified routes.
// If ways is nil (Overpass failed), it prints the fallback warning and returns immediately.
func ApplyQuality(routes []route.Route, ways []Way) {
	if ways == nil {
		return
	}

	const maxDist = 30.0

	for i := range routes {
		pts := routes[i].Points
		if len(pts) < 2 {
			continue
		}

		totalDist := 0.0
		disqualifiedDist := 0.0
		penaltyWeightedDist := 0.0

		for j := 0; j < len(pts)-1; j++ {
			segLen := geo.Haversine(pts[j], pts[j+1])
			midpoint := geo.Coord{
				Lat: (pts[j].Lat + pts[j+1].Lat) / 2,
				Lon: (pts[j].Lon + pts[j+1].Lon) / 2,
			}
			totalDist += segLen

			w := NearestWay(midpoint, ways, maxDist)
			if w == nil {
				continue
			}
			if IsDisqualifying(w.Tags) {
				disqualifiedDist += segLen
			}
			penaltyWeightedDist += PenaltyFactor(w.Tags) * segLen
		}

		if totalDist == 0 {
			continue
		}

		disqualifiedFraction := disqualifiedDist / totalDist
		penaltyFraction := penaltyWeightedDist / totalDist

		adjusted := routes[i].Stats.AdjustedScore
		adjusted *= (1 - disqualifiedFraction*0.9)
		adjusted *= (1 - penaltyFraction*0.3)
		routes[i].Stats.AdjustedScore = adjusted

		if disqualifiedFraction > 0.10 {
			fmt.Printf("WARNING: route %d has %.1f%% disqualifying segments\n", i, disqualifiedFraction*100)
		}
	}
}
