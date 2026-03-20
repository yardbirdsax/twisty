package quality

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

// helper to build a simple ScoredSegment
func seg(tier int, startLat, startLon, endLat, endLon float64) ScoredSegment {
	return ScoredSegment{
		Tier:  tier,
		Start: geo.Coord{Lat: startLat, Lon: startLon},
		End:   geo.Coord{Lat: endLat, Lon: endLon},
	}
}

func TestMergeTierRuns_AllSameTier(t *testing.T) {
	segments := []ScoredSegment{
		seg(1, 0, 0, 1, 1),
		seg(1, 1, 1, 2, 2),
		seg(1, 2, 2, 3, 3),
	}
	runs := MergeTierRuns(segments)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Tier != 1 {
		t.Errorf("expected tier 1, got %d", runs[0].Tier)
	}
	if len(runs[0].Segments) != 3 {
		t.Errorf("expected 3 segments in run, got %d", len(runs[0].Segments))
	}
}

func TestMergeTierRuns_AlternatingTiers(t *testing.T) {
	segments := []ScoredSegment{
		seg(1, 0, 0, 1, 1),
		seg(2, 1, 1, 2, 2),
		seg(2, 2, 2, 3, 3),
		seg(3, 3, 3, 4, 4),
		seg(1, 4, 4, 5, 5),
	}
	runs := MergeTierRuns(segments)
	if len(runs) != 4 {
		t.Fatalf("expected 4 runs, got %d", len(runs))
	}
	want := []struct {
		tier int
		n    int
	}{
		{1, 1},
		{2, 2},
		{3, 1},
		{1, 1},
	}
	for i, w := range want {
		if runs[i].Tier != w.tier {
			t.Errorf("run %d: expected tier %d, got %d", i, w.tier, runs[i].Tier)
		}
		if len(runs[i].Segments) != w.n {
			t.Errorf("run %d: expected %d segments, got %d", i, w.n, len(runs[i].Segments))
		}
	}
}

func TestMergeTierRuns_Empty(t *testing.T) {
	runs := MergeTierRuns(nil)
	if len(runs) != 0 {
		t.Errorf("expected 0 runs for nil input, got %d", len(runs))
	}
}

func TestFormatCoordinates_Basic(t *testing.T) {
	segments := []ScoredSegment{
		seg(1, 10.0, -70.0, 11.0, -71.0),
		seg(1, 11.0, -71.0, 12.0, -72.0),
	}
	out := formatCoordinates(segments)
	// Should have 3 coordinate pairs: start of first + end of each
	parts := strings.Fields(out)
	if len(parts) != 3 {
		t.Fatalf("expected 3 coordinate pairs, got %d: %q", len(parts), out)
	}
	// First point: lon,lat,0 of first segment start
	if parts[0] != "-70.000000,10.000000,0" {
		t.Errorf("unexpected first coordinate: %q", parts[0])
	}
	if parts[1] != "-71.000000,11.000000,0" {
		t.Errorf("unexpected second coordinate: %q", parts[1])
	}
	if parts[2] != "-72.000000,12.000000,0" {
		t.Errorf("unexpected third coordinate: %q", parts[2])
	}
}

func TestFormatCoordinates_NoDuplicateEndpoints(t *testing.T) {
	// Three consecutive segments sharing endpoints
	segments := []ScoredSegment{
		seg(2, 1.0, 10.0, 2.0, 11.0),
		seg(2, 2.0, 11.0, 3.0, 12.0),
		seg(2, 3.0, 12.0, 4.0, 13.0),
	}
	out := formatCoordinates(segments)
	parts := strings.Fields(out)
	// Should be 4 points: start + 3 ends
	if len(parts) != 4 {
		t.Fatalf("expected 4 coordinate pairs, got %d", len(parts))
	}
}

func TestFormatCoordinates_Empty(t *testing.T) {
	out := formatCoordinates(nil)
	if out != "" {
		t.Errorf("expected empty string for nil input, got %q", out)
	}
}

// makeCollection creates a minimal RoadCollection for testing.
func makeCollection(name string, penalizedScore float64, totalLength float64, penalizedPerKm float64, highways []string, wayIDs []int64, segments []ScoredSegment) RoadCollection {
	return RoadCollection{
		Name:           name,
		PenalizedScore: penalizedScore,
		TotalLength:    totalLength,
		PenalizedPerKm: penalizedPerKm,
		HighwayTypes:   highways,
		WayIDs:         wayIDs,
		Segments:       segments,
	}
}

func simpleSegments() []ScoredSegment {
	return []ScoredSegment{
		seg(1, 35.0, -80.0, 35.1, -80.1),
		seg(2, 35.1, -80.1, 35.2, -80.2),
	}
}

func TestWriteKML_BasicOutput(t *testing.T) {
	col := makeCollection("Test Road", 500.0, 5000.0, 100.0, []string{"secondary"}, []int64{12345}, simpleSegments())
	var buf bytes.Buffer
	err := WriteKML(&buf, []RoadCollection{col}, 0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	out := buf.String()

	// Must be valid XML
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(out[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("output is not valid XML: %v\n%s", err, out)
	}

	if kml.Doc.Name != "Twisty Roads" {
		t.Errorf("expected document name 'Twisty Roads', got %q", kml.Doc.Name)
	}
	if len(kml.Doc.Folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(kml.Doc.Folders))
	}
	folder := kml.Doc.Folders[0]
	if folder.Name != "Test Road" {
		t.Errorf("expected folder name 'Test Road', got %q", folder.Name)
	}
	if !strings.Contains(folder.Description, "Score: 500") {
		t.Errorf("description missing score: %q", folder.Description)
	}
	if !strings.Contains(folder.Description, "12345") {
		t.Errorf("description missing way ID: %q", folder.Description)
	}
	if folder.Style == nil {
		t.Fatalf("folder.Style is nil")
	}
	if folder.Style.ListStyle.ListItemType != "checkHideChildren" {
		t.Errorf("expected checkHideChildren, got %q", folder.Style.ListStyle.ListItemType)
	}
}

func TestWriteKML_MinScoreFilter(t *testing.T) {
	col1 := makeCollection("High Road", 1000.0, 10000.0, 100.0, []string{"tertiary"}, []int64{1}, simpleSegments())
	col2 := makeCollection("Low Road", 50.0, 5000.0, 10.0, []string{"tertiary"}, []int64{2}, simpleSegments())
	var buf bytes.Buffer
	err := WriteKML(&buf, []RoadCollection{col1, col2}, 100.0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if len(kml.Doc.Folders) != 1 {
		t.Fatalf("expected 1 folder after filter, got %d", len(kml.Doc.Folders))
	}
	if kml.Doc.Folders[0].Name != "High Road" {
		t.Errorf("expected 'High Road', got %q", kml.Doc.Folders[0].Name)
	}
}

func TestWriteKML_SortOrder(t *testing.T) {
	col1 := makeCollection("Low", 200.0, 5000.0, 100.0, []string{"tertiary"}, []int64{1}, simpleSegments())
	col2 := makeCollection("High", 800.0, 8000.0, 100.0, []string{"tertiary"}, []int64{2}, simpleSegments())
	col3 := makeCollection("Mid", 500.0, 5000.0, 100.0, []string{"tertiary"}, []int64{3}, simpleSegments())
	var buf bytes.Buffer
	err := WriteKML(&buf, []RoadCollection{col1, col2, col3}, 0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if len(kml.Doc.Folders) != 3 {
		t.Fatalf("expected 3 folders, got %d", len(kml.Doc.Folders))
	}
	order := []string{"High", "Mid", "Low"}
	for i, name := range order {
		if kml.Doc.Folders[i].Name != name {
			t.Errorf("position %d: expected %q, got %q", i, name, kml.Doc.Folders[i].Name)
		}
	}
}

func TestWriteKML_EmptyInput(t *testing.T) {
	var buf bytes.Buffer
	err := WriteKML(&buf, nil, 0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("invalid XML for empty input: %v\n%s", err, buf.String())
	}
	if kml.Doc.Name != "Twisty Roads" {
		t.Errorf("expected 'Twisty Roads', got %q", kml.Doc.Name)
	}
	if len(kml.Doc.Folders) != 0 {
		t.Errorf("expected 0 folders, got %d", len(kml.Doc.Folders))
	}
}

func TestWriteKML_StyleDefinitions(t *testing.T) {
	var buf bytes.Buffer
	err := WriteKML(&buf, nil, 0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if len(kml.Doc.Styles) != 5 {
		t.Fatalf("expected 5 styles, got %d", len(kml.Doc.Styles))
	}
	// Build a map of id -> style for easy lookup
	styleMap := make(map[string]KMLStyle)
	for _, s := range kml.Doc.Styles {
		styleMap[s.ID] = s
	}
	for tier := 0; tier <= 4; tier++ {
		id := fmt.Sprintf("tier%d", tier)
		s, ok := styleMap[id]
		if !ok {
			t.Errorf("missing style %q", id)
			continue
		}
		wantColor := TierColors[tier]
		if s.LineStyle.Color != wantColor {
			t.Errorf("tier %d: expected color %q, got %q", tier, wantColor, s.LineStyle.Color)
		}
		if s.LineStyle.Width != KMLLineWidth {
			t.Errorf("tier %d: expected width %d, got %d", tier, KMLLineWidth, s.LineStyle.Width)
		}
	}
}

// TestWriteKML_MinLengthFilter verifies that collections with TotalLength < MinRoadLengthM
// are excluded from output, and that both filters are independent.
func TestWriteKML_MinLengthFilter(t *testing.T) {
	// Collections with varying lengths (all have high score so only length filter matters).
	short1km := makeCollection("Short 1km", 1000.0, 1000.0, 200.0, []string{"tertiary"}, []int64{1}, simpleSegments())
	short3km := makeCollection("Short 3km", 1000.0, 3000.0, 200.0, []string{"tertiary"}, []int64{2}, simpleSegments())
	above5km := makeCollection("Above 5km", 1000.0, 5000.0, 200.0, []string{"tertiary"}, []int64{3}, simpleSegments())
	above10km := makeCollection("Above 10km", 1000.0, 10000.0, 200.0, []string{"tertiary"}, []int64{4}, simpleSegments())

	var buf bytes.Buffer
	err := WriteKML(&buf, []RoadCollection{short1km, short3km, above5km, above10km}, 0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	// Only collections >= MinRoadLengthM (4,828 m) should appear.
	if len(kml.Doc.Folders) != 2 {
		t.Fatalf("expected 2 folders (collections >= MinRoadLengthM), got %d", len(kml.Doc.Folders))
	}
	names := make(map[string]bool)
	for _, f := range kml.Doc.Folders {
		names[f.Name] = true
	}
	if !names["Above 5km"] {
		t.Error("expected 'Above 5km' in output")
	}
	if !names["Above 10km"] {
		t.Error("expected 'Above 10km' in output")
	}
	if names["Short 1km"] {
		t.Error("'Short 1km' should be excluded (length < MinRoadLengthM)")
	}
	if names["Short 3km"] {
		t.Error("'Short 3km' should be excluded (length < MinRoadLengthM)")
	}
}

// TestWriteKML_HighScoreShortLengthExcluded verifies a high-score but short collection
// is excluded by the length filter.
func TestWriteKML_HighScoreShortLengthExcluded(t *testing.T) {
	highScoreShort := makeCollection("High Score Short", 9999.0, 1000.0, 9999.0, []string{"tertiary"}, []int64{1}, simpleSegments())
	longRoad := makeCollection("Long Road", 100.0, 10000.0, 10.0, []string{"tertiary"}, []int64{2}, simpleSegments())

	var buf bytes.Buffer
	err := WriteKML(&buf, []RoadCollection{highScoreShort, longRoad}, 0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	if len(kml.Doc.Folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(kml.Doc.Folders))
	}
	if kml.Doc.Folders[0].Name != "Long Road" {
		t.Errorf("expected 'Long Road' in output, got %q", kml.Doc.Folders[0].Name)
	}
}

// TestWriteKML_LowScoreLongLengthExcludedByMinScore verifies that a long road
// with score below minScore is still excluded — both filters are independent.
func TestWriteKML_LowScoreLongLengthExcludedByMinScore(t *testing.T) {
	lowScoreLong := makeCollection("Low Score Long", 50.0, 10000.0, 5.0, []string{"tertiary"}, []int64{1}, simpleSegments())
	highScoreLong := makeCollection("High Score Long", 500.0, 10000.0, 50.0, []string{"tertiary"}, []int64{2}, simpleSegments())

	var buf bytes.Buffer
	// minScore = 100 excludes low-score-long but not high-score-long.
	err := WriteKML(&buf, []RoadCollection{lowScoreLong, highScoreLong}, 100.0)
	if err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	if len(kml.Doc.Folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(kml.Doc.Folders))
	}
	if kml.Doc.Folders[0].Name != "High Score Long" {
		t.Errorf("expected 'High Score Long', got %q", kml.Doc.Folders[0].Name)
	}
}
