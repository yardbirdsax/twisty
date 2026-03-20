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

// formatCoordinates formats segment coordinates for KML as "lon,lat,0" pairs
// separated by spaces. Includes the start of the first segment, then the end
// of each segment to avoid duplicating shared endpoints.
func formatCoordinates(segments []ScoredSegment) string {
	if len(segments) == 0 {
		return ""
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%f,%f,0", segments[0].Start.Lon, segments[0].Start.Lat))
	for _, seg := range segments {
		parts = append(parts, fmt.Sprintf("%f,%f,0", seg.End.Lon, seg.End.Lat))
	}
	return strings.Join(parts, " ")
}

// WriteKML writes road collections as a KML file.
// Collections are sorted by penalized score descending.
// Collections with penalized score below minScore are excluded.
func WriteKML(w io.Writer, collections []RoadCollection, minScore float64) error {
	// Filter by minScore
	var filtered []RoadCollection
	for _, c := range collections {
		if c.PenalizedScore >= minScore {
			filtered = append(filtered, c)
		}
	}

	// Sort by penalized score descending
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].PenalizedScore > filtered[j].PenalizedScore
	})

	// Build style definitions
	styles := make([]KMLStyle, 0, 5)
	for tier := 0; tier <= 4; tier++ {
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
