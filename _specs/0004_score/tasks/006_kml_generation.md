# Task 006: KML Generation

## Summary

Implement stage 7: render road collections as a multi-color KML file where each segment is colored by curvature tier. This produces the visual output users load into Google Earth.

## Dependencies

Task 002, Task 005 — requires `RoadCollection` with penalized scores.

## Detailed Directions

### 1. Define KML Color Constants

- In `quality/scoring_params.go` (or in the new `quality/kml.go`), define tier colors:

```go
// KML colors in AABBGGRR format
var TierColors = map[int]string{
	0: "F000E010", // Green
	1: "F000FFFF", // Yellow
	2: "F000AAFF", // Orange
	3: "F00055FF", // Dark Orange
	4: "F00000FF", // Red
}

const KMLLineWidth = 4
```

### 2. Create `quality/kml.go` with XML Structs

- Define Go structs that marshal to KML XML using `encoding/xml`. Key structs:

```go
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
	Name        string         `xml:"name"`
	Description string         `xml:"description"`
	Style       KMLFolderStyle `xml:"Style"`
	Placemarks  []KMLPlacemark `xml:"Placemark"`
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
```

Adjust struct definitions as needed to produce valid KML — test the output against the KML schema.

### 3. Implement Tier Run Merging

- Add a function to merge contiguous segments of the same tier into runs:

```go
// tierRun represents a contiguous sequence of segments with the same curvature tier.
type tierRun struct {
	Tier     int
	Segments []ScoredSegment
}

// MergeTierRuns merges contiguous segments of the same tier.
func MergeTierRuns(segments []ScoredSegment) []tierRun
```

- Walk segments in order. When the tier changes, start a new run.

### 4. Implement Coordinate Formatting

- Add a helper to format segment coordinates for KML:

```go
func formatCoordinates(segments []ScoredSegment) string
```

- KML format: `lon,lat,0` separated by spaces. Include the start of the first segment, then the end of each segment. Avoid duplicating shared endpoints between consecutive segments.

### 5. Implement KML Writer

- Add the main KML generation function:

```go
// WriteKML writes road collections as a KML file.
// Collections are sorted by penalized score descending.
// Collections with penalized score below minScore are excluded.
func WriteKML(w io.Writer, collections []RoadCollection, minScore float64) error
```

- Steps:
  1. Filter collections by `minScore`
  2. Sort remaining by `PenalizedScore` descending
  3. Build tier style definitions (tier0 through tier4)
  4. For each collection, build a `KMLFolder`:
     - Name: `collection.DisplayName()`
     - Description: formatted string with penalized score, score per km, total length in km, highway types, way IDs
     - ListStyle: `checkHideChildren`
     - Placemarks: one per tier run, with `#tierN` style URL and formatted coordinates
  5. Marshal to XML and write

### 6. Write Unit Tests

- Create `quality/kml_test.go` with tests:
  - `TestMergeTierRuns`: segments with alternating tiers produce correct runs; all same tier produces one run
  - `TestFormatCoordinates`: correct `lon,lat,0` format, no duplicate endpoints
  - `TestWriteKML_BasicOutput`: synthetic collections produce valid XML with expected structure
  - `TestWriteKML_MinScoreFilter`: collections below threshold are excluded
  - `TestWriteKML_SortOrder`: highest-scoring collection appears first
  - `TestWriteKML_EmptyInput`: empty collection list produces valid empty KML document
  - `TestWriteKML_StyleDefinitions`: all five tier styles present with correct colors

## Acceptance Criteria

- [ ] KML output is valid XML that follows the KML structure from the PRD
- [ ] Five tier styles defined with correct AABBGGRR colors and 4px line width
- [ ] Each road collection renders as a folder with name, description, and hidden children
- [ ] Contiguous same-tier segments are merged into single placemarks
- [ ] Coordinates use `lon,lat,0` format (longitude first)
- [ ] Collections sorted by penalized score descending
- [ ] `-min-score` filtering excludes low-scoring roads
- [ ] Empty input produces valid empty KML document with document name "Twisty Roads"
- [ ] All unit tests pass

## Notes

- Use `encoding/xml` for all XML generation — no string concatenation. This handles escaping of road names that may contain special characters (`&`, `<`, etc.).
- The `xmlns` attribute for KML is `http://www.opengis.net/kml/2.2`.
- Description format example: `Score: 1219 | Per km: 99 | Length: 12.3 km | Types: secondary | Ways: 12345, 12346`
- Road lengths in the description should be in km (divide meters by 1000), rounded to one decimal.
