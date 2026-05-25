package quality

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// KML colors in AABBGGRR format.
var TierColors = map[int]string{
	0: "F000E010", // Green
	1: "F000FFFF", // Yellow
	2: "F000AAFF", // Orange
	3: "F00055FF", // Dark Orange
	4: "F00000FF", // Red
}

// KMLLineWidth is the line width used for all KML placemarks.
const KMLLineWidth = 4

// KML XML structs

type KMLDocument struct {
	XMLName xml.Name    `xml:"kml"`
	XMLNS   string      `xml:"xmlns,attr"`
	Doc     KMLDocInner `xml:"Document"`
}

type KMLDocInner struct {
	Name    string      `xml:"name"`
	Styles  []KMLStyle  `xml:"Style"`
	Folders []KMLFolder `xml:"Folder"`
}

type KMLStyle struct {
	ID        string       `xml:"id,attr"`
	LineStyle KMLLineStyle `xml:"LineStyle"`
}

type KMLLineStyle struct {
	Color string `xml:"color"`
	Width int    `xml:"width"`
}

type KMLFolder struct {
	Name        string          `xml:"name"`
	Description string          `xml:"description"`
	Style       *KMLFolderStyle `xml:"Style,omitempty"`
	Placemarks  []KMLPlacemark  `xml:"Placemark"`
}

type KMLFolderStyle struct {
	ListStyle KMLListStyle `xml:"ListStyle"`
}

type KMLListStyle struct {
	ListItemType string `xml:"listItemType"`
}

type KMLPlacemark struct {
	StyleURL   string        `xml:"styleUrl"`
	LineString KMLLineString `xml:"LineString"`
}

// KMLInlinePlacemark is a placemark that carries its own inline Style element
// instead of referencing a shared style by URL. Used by WriteKMLSingleColor.
type KMLInlinePlacemark struct {
	Style      KMLStyle      `xml:"Style"`
	LineString KMLLineString `xml:"LineString"`
}

// KMLSingleColorFolder is a KML Folder element that uses inline-styled placemarks.
type KMLSingleColorFolder struct {
	Name        string               `xml:"name"`
	Description string               `xml:"description"`
	Style       *KMLFolderStyle      `xml:"Style,omitempty"`
	Placemarks  []KMLInlinePlacemark `xml:"Placemark"`
}

// KMLSingleColorDocInner is the Document element for single-color KML output.
// It has no shared Styles — each placemark carries its own inline style.
type KMLSingleColorDocInner struct {
	Name    string                 `xml:"name"`
	Folders []KMLSingleColorFolder `xml:"Folder"`
}

// KMLSingleColorDocument is the root kml element for single-color output.
type KMLSingleColorDocument struct {
	XMLName xml.Name               `xml:"kml"`
	XMLNS   string                 `xml:"xmlns,attr"`
	Doc     KMLSingleColorDocInner `xml:"Document"`
}

type KMLLineString struct {
	Coordinates string `xml:"coordinates"`
}

// tierRun represents a contiguous sequence of segments with the same curvature tier.
type tierRun struct {
	Tier     int
	Segments []ScoredSegment
}

// MergeTierRuns merges contiguous segments of the same tier.
func MergeTierRuns(segments []ScoredSegment) []tierRun {
	if len(segments) == 0 {
		return nil
	}
	var runs []tierRun
	current := tierRun{Tier: segments[0].Tier, Segments: []ScoredSegment{segments[0]}}
	for _, seg := range segments[1:] {
		if seg.Tier == current.Tier {
			current.Segments = append(current.Segments, seg)
		} else {
			runs = append(runs, current)
			current = tierRun{Tier: seg.Tier, Segments: []ScoredSegment{seg}}
		}
	}
	runs = append(runs, current)
	return runs
}

// filterAndSortCollections filters collections by minScore and MinRoadLengthM,
// then sorts them by penalized score descending.
func filterAndSortCollections(cols []RoadCollection, minScore, minLength, minSpeedMPH float64) []RoadCollection {
	var filtered []RoadCollection
	for _, c := range cols {
		if c.PenalizedScore < minScore || c.TotalLength < minLength {
			continue
		}
		if minSpeedMPH > 0 && SpeedPassingFraction(c.WaySpeeds, c.TotalLength, minSpeedMPH) < 0.5 {
			continue
		}
		filtered = append(filtered, c)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].PenalizedScore > filtered[j].PenalizedScore
	})
	return filtered
}

// formatCoordinates formats segment coordinates for KML as "lon,lat,0" pairs
// separated by spaces. Includes the start of the first segment, then the end
// of each segment to avoid duplicating shared endpoints.
func formatCoordinates(segments []ScoredSegment) string {
	if len(segments) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%f,%f,0", segments[0].Start.Lon, segments[0].Start.Lat)
	for _, seg := range segments {
		fmt.Fprintf(&sb, " %f,%f,0", seg.End.Lon, seg.End.Lat)
	}
	return sb.String()
}

// WriteKML writes road collections as a KML file.
// Collections are sorted by penalized score descending.
// Collections with penalized score below minScore or total length below
// MinRoadLengthM are excluded.
func WriteKML(w io.Writer, collections []RoadCollection, minScore float64, minSpeedMPH ...float64) error {
	speed := 0.0
	if len(minSpeedMPH) > 0 {
		speed = minSpeedMPH[0]
	}
	filtered := filterAndSortCollections(collections, minScore, MinRoadLengthM, speed)

	// Build style definitions
	styles := make([]KMLStyle, 0, len(TierColors))
	for tier := 0; tier < len(TierColors); tier++ {
		styles = append(styles, KMLStyle{
			ID: fmt.Sprintf("tier%d", tier),
			LineStyle: KMLLineStyle{
				Color: TierColors[tier],
				Width: KMLLineWidth,
			},
		})
	}

	// Build folders
	folders := make([]KMLFolder, 0, len(filtered))
	for _, col := range filtered {
		desc := buildDescription(col)

		runs := MergeTierRuns(col.Segments)
		placemarks := make([]KMLPlacemark, 0, len(runs))
		for _, run := range runs {
			placemarks = append(placemarks, KMLPlacemark{
				StyleURL:   fmt.Sprintf("#tier%d", run.Tier),
				LineString: KMLLineString{Coordinates: formatCoordinates(run.Segments)},
			})
		}

		folders = append(folders, KMLFolder{
			Name:        col.DisplayName(),
			Description: desc,
			Style:       &KMLFolderStyle{ListStyle: KMLListStyle{ListItemType: "checkHideChildren"}},
			Placemarks:  placemarks,
		})
	}

	doc := KMLDocument{
		XMLNS: "http://www.opengis.net/kml/2.2",
		Doc: KMLDocInner{
			Name:    "Twisty Roads",
			Styles:  styles,
			Folders: folders,
		},
	}

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "%s%s", xml.Header, string(out))
	return err
}

// CurvatureColorLevel maps a total curvature score to a color level (0-511)
// using a logarithmic scale for better visual differentiation at lower scores.
// The algorithm matches the Curvature project's SingleColorKmlOutput.level_for_curvature.
// Level 0 means "at or below minCurvature" (renders as green). Levels 1-511
// represent the yellow→red→magenta gradient.
func CurvatureColorLevel(score, minCurvature, maxCurvature float64) int {
	if score < minCurvature {
		return 0
	}
	pct := (score - minCurvature) / (maxCurvature - minCurvature)
	if pct > 1 {
		pct = 1
	}
	// Logarithmic scale: y = 1 - 1/(10^(x*2))
	colorPct := 1 - 1/math.Pow(10, pct*0.75)
	return int(math.Round(510*colorPct)) + 1
}

// GradientColor returns a KML AABBGGRR color string for the given level (0-511).
// Level 0 returns green (same as tier 0). Levels 1-256 are yellow→red.
// Levels 257-511 are red→magenta.
func GradientColor(level int) string {
	if level <= 0 {
		return TierColors[0] // green
	}
	if level <= 256 {
		// Yellow (FF,FF,00) → Red (FF,00,00)
		// KML AABBGGRR: alpha=FF, blue=00, green=variable, red=FF
		green := 255 - (level-1)*255/255
		return fmt.Sprintf("FF00%02XFF", green)
	}
	// Red (FF,00,00) → Magenta (FF,00,FF)
	// KML AABBGGRR: alpha=FF, blue=variable, green=00, red=FF
	blue := (level - 257) * 255 / 254
	return fmt.Sprintf("FF%02X00FF", blue)
}

// WriteKMLSingleColor writes road collections as a KML file where each road is
// rendered as a single-color polyline. The color is determined by the road's
// TotalScore using a logarithmic gradient from yellow (low score) to red to
// magenta (high score). Roads below minScore or below MinRoadLengthM are excluded.
// Collections are sorted by penalized score descending.
//
// TotalScore (not PenalizedScore) is used for the color because the visual should
// reflect actual road geometry, while PenalizedScore is used for filtering and sort order.
func WriteKMLSingleColor(w io.Writer, collections []RoadCollection, minScore float64, minSpeedMPH ...float64) error {
	speed := 0.0
	if len(minSpeedMPH) > 0 {
		speed = minSpeedMPH[0]
	}
	filtered := filterAndSortCollections(collections, minScore, MinRoadLengthM, speed)

	// Build folders — one per collection, one placemark per road
	folders := make([]KMLSingleColorFolder, 0, len(filtered))
	for _, col := range filtered {
		desc := buildDescription(col)

		level := CurvatureColorLevel(col.TotalScore, DefaultMinCurvature, DefaultMaxCurvature)
		color := GradientColor(level)

		placemark := KMLInlinePlacemark{
			Style: KMLStyle{
				LineStyle: KMLLineStyle{
					Color: color,
					Width: KMLLineWidth,
				},
			},
			LineString: KMLLineString{Coordinates: formatCoordinates(col.Segments)},
		}

		folders = append(folders, KMLSingleColorFolder{
			Name:        col.DisplayName(),
			Description: desc,
			Style:       &KMLFolderStyle{ListStyle: KMLListStyle{ListItemType: "checkHideChildren"}},
			Placemarks:  []KMLInlinePlacemark{placemark},
		})
	}

	doc := KMLSingleColorDocument{
		XMLNS: "http://www.opengis.net/kml/2.2",
		Doc: KMLSingleColorDocInner{
			Name:    "Twisty Roads",
			Folders: folders,
		},
	}

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "%s%s", xml.Header, string(out))
	return err
}

// buildDescription formats the description string for a road collection folder.
func buildDescription(col RoadCollection) string {
	wayStrs := make([]string, len(col.WayIDs))
	for i, id := range col.WayIDs {
		wayStrs[i] = fmt.Sprintf("%d", id)
	}
	return fmt.Sprintf(
		"Score: %d | Per km: %d | Length: %.1f km | Types: %s | Ways: %s",
		int(math.Round(col.PenalizedScore)),
		int(math.Round(col.PenalizedPerKm)),
		col.TotalLength/1000.0,
		strings.Join(col.HighwayTypes, ", "),
		strings.Join(wayStrs, ", "),
	)
}
