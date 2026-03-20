package quality

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

// ---------------------------------------------------------------------------
// Test fixtures — synthetic ScoredWay data representing realistic road scenarios
// All coordinates use the Vermont area (~44°N, ~-72°W).
// ---------------------------------------------------------------------------

// makeScoredSegment creates a ScoredSegment with explicit coordinates and scoring.
func makeScoredSegment(wayID int64, tier int, startLat, startLon, endLat, endLon, length float64) ScoredSegment {
	_, weight := AssignTier(float64(radiusForTier(tier)))
	score := length * weight
	return ScoredSegment{
		WayID:  wayID,
		Tier:   tier,
		Weight: weight,
		Length: length,
		Score:  score,
		Start:  geo.Coord{Lat: startLat, Lon: startLon},
		End:    geo.Coord{Lat: endLat, Lon: endLon},
	}
}

// radiusForTier returns a representative radius for a given tier (used for weight lookup).
func radiusForTier(tier int) float64 {
	switch tier {
	case 4:
		return 20.0
	case 3:
		return 45.0
	case 2:
		return 80.0
	case 1:
		return 130.0
	default:
		return 500.0 // tier 0 — straight
	}
}

// fixtureConnectedWays builds a chain of ScoredWays that share endpoints, starting at
// (startLat, startLon) and advancing north by stepLat each way.
// Each way gets one segment of the specified tier with the given length.
func fixtureConnectedWays(startID int64, name, highway string, startLat, startLon, stepLat float64, count int, tier int, segLength float64) []ScoredWay {
	ways := make([]ScoredWay, count)
	lat := startLat
	for i := range count {
		endLat := lat + stepLat
		seg := makeScoredSegment(startID+int64(i), tier, lat, startLon, endLat, startLon, segLength)
		ways[i] = ScoredWay{
			WayID: startID + int64(i),
			Tags:  map[string]string{"name": name, "highway": highway},
			Segments: []ScoredSegment{seg},
		}
		lat = endLat
	}
	return ways
}

// fixtureTwistyMountainPass builds "Mountain Pass Rd" — 18 connected ways with
// mixed tier 2-4 segments, highway type "secondary".
func fixtureTwistyMountainPass() []ScoredWay {
	const (
		name    = "Mountain Pass Rd"
		highway = "secondary"
		startLat = 44.10
		startLon = -72.50
		stepLat  = 0.0005 // ~55 m
	)
	var ways []ScoredWay
	tiers := []int{2, 3, 4, 2, 3, 4, 2, 3, 2, 3, 4, 3, 2, 4, 3, 2, 3, 4}
	lat := startLat
	for i, tier := range tiers {
		endLat := lat + stepLat
		seg := makeScoredSegment(int64(100+i), tier, lat, startLon, endLat, startLon, 80.0)
		ways = append(ways, ScoredWay{
			WayID: int64(100 + i),
			Tags:  map[string]string{"name": name, "highway": highway},
			Segments: []ScoredSegment{seg},
		})
		lat = endLat
	}
	return ways
}

// fixtureInterstate builds "Interstate 90" — 10 connected ways, mostly tier 0
// with a few tier 2-3 segments representing on-ramps, highway type "motorway".
func fixtureInterstate() []ScoredWay {
	const (
		name    = "Interstate 90"
		highway = "motorway"
		startLat = 44.20
		startLon = -72.40
		stepLat  = 0.001 // ~111 m
	)
	tiers := []int{0, 0, 2, 3, 0, 0, 0, 2, 0, 0}
	var ways []ScoredWay
	lat := startLat
	for i, tier := range tiers {
		endLat := lat + stepLat
		seg := makeScoredSegment(int64(200+i), tier, lat, startLon, endLat, startLon, 150.0)
		ways = append(ways, ScoredWay{
			WayID: int64(200 + i),
			Tags:  map[string]string{"name": name, "highway": highway},
			Segments: []ScoredSegment{seg},
		})
		lat = endLat
	}
	return ways
}

// fixtureRoute100 builds "Route 100" — two clusters of twisty ways separated by
// a long straight gap (> StraightGapSplitM = 2414 m), highway type "tertiary".
// The gap is represented by a single tier-0 segment of 2500 m.
func fixtureRoute100() []ScoredWay {
	const (
		name    = "Route 100"
		highway = "tertiary"
		startLat = 44.30
		startLon = -72.60
		stepLat  = 0.0005 // ~55 m
	)

	var ways []ScoredWay
	lat := startLat

	// First cluster: 5 twisty ways
	for i := range 5 {
		endLat := lat + stepLat
		seg := makeScoredSegment(int64(300+i), 2, lat, startLon, endLat, startLon, 100.0)
		ways = append(ways, ScoredWay{
			WayID: int64(300 + i),
			Tags:  map[string]string{"name": name, "highway": highway},
			Segments: []ScoredSegment{seg},
		})
		lat = endLat
	}

	// Gap: one way with a single tier-0 segment of 2600 m (> StraightGapSplitM)
	gapEndLat := lat + 0.025 // large jump north
	gapSeg := ScoredSegment{
		WayID:  305,
		Tier:   0,
		Weight: TierWeight0,
		Length: 2600.0,
		Score:  0,
		Start:  geo.Coord{Lat: lat, Lon: startLon},
		End:    geo.Coord{Lat: gapEndLat, Lon: startLon},
	}
	ways = append(ways, ScoredWay{
		WayID:    305,
		Tags:     map[string]string{"name": name, "highway": highway},
		Segments: []ScoredSegment{gapSeg},
	})
	lat = gapEndLat

	// Second cluster: 5 more twisty ways
	for i := range 5 {
		endLat := lat + stepLat
		seg := makeScoredSegment(int64(306+i), 3, lat, startLon, endLat, startLon, 100.0)
		ways = append(ways, ScoredWay{
			WayID: int64(306 + i),
			Tags:  map[string]string{"name": name, "highway": highway},
			Segments: []ScoredSegment{seg},
		})
		lat = endLat
	}

	return ways
}

// fixtureMainStreet builds two geographically disconnected clusters both named
// "Main Street". Cluster A is in Vermont, cluster B is in North Carolina.
func fixtureMainStreet() []ScoredWay {
	const name = "Main Street"

	// Cluster A: 4 ways in Vermont area
	clusterA := fixtureConnectedWays(400, name, "residential", 44.40, -72.70, 0.0005, 4, 2, 120.0)

	// Cluster B: 4 ways in North Carolina — far enough that they form a separate component
	clusterB := fixtureConnectedWays(410, name, "residential", 36.00, -80.00, 0.0005, 4, 2, 120.0)

	return append(clusterA, clusterB...)
}

// fixtureUnnamedRoad builds ways with no "name" tag.
func fixtureUnnamedRoad() []ScoredWay {
	return []ScoredWay{
		{
			WayID: 500,
			Tags:  map[string]string{"highway": "tertiary"}, // no "name"
			Segments: []ScoredSegment{
				makeScoredSegment(500, 2, 44.50, -72.80, 44.501, -72.80, 200.0),
			},
		},
	}
}

// fixtureSpecialCharsRoad builds a road with special characters in its name.
func fixtureSpecialCharsRoad() []ScoredWay {
	return []ScoredWay{
		{
			WayID: 600,
			Tags:  map[string]string{"name": "Route 9 & 20", "highway": "secondary"},
			Segments: []ScoredSegment{
				makeScoredSegment(600, 2, 44.55, -72.85, 44.551, -72.85, 300.0),
				makeScoredSegment(600, 3, 44.551, -72.85, 44.552, -72.85, 200.0),
			},
		},
	}
}

// fixtureAllTierZeroRoad builds a road whose all segments are tier 0.
func fixtureAllTierZeroRoad() []ScoredWay {
	return []ScoredWay{
		{
			WayID: 700,
			Tags:  map[string]string{"name": "Flat Street", "highway": "residential"},
			Segments: []ScoredSegment{
				{WayID: 700, Tier: 0, Weight: 0, Length: 500, Score: 0,
					Start: geo.Coord{Lat: 44.60, Lon: -72.90},
					End:   geo.Coord{Lat: 44.605, Lon: -72.90}},
			},
		},
	}
}

// allFixtures returns all fixture ways combined.
func allFixtures() []ScoredWay {
	var all []ScoredWay
	all = append(all, fixtureTwistyMountainPass()...)
	all = append(all, fixtureInterstate()...)
	all = append(all, fixtureRoute100()...)
	all = append(all, fixtureMainStreet()...)
	all = append(all, fixtureUnnamedRoad()...)
	all = append(all, fixtureSpecialCharsRoad()...)
	all = append(all, fixtureAllTierZeroRoad()...)
	return all
}

// ---------------------------------------------------------------------------
// Integration Tests
// ---------------------------------------------------------------------------

// TestPipelineIntegration_FullPipelineProducesValidKML exercises the full
// Aggregate → ApplyPenalties → WriteKML pipeline and verifies the output.
func TestPipelineIntegration_FullPipelineProducesValidKML(t *testing.T) {
	ways := allFixtures()

	collections := Aggregate(ways)
	ApplyPenalties(collections)

	var buf bytes.Buffer
	if err := WriteKML(&buf, collections, 0); err != nil {
		t.Fatalf("WriteKML returned error: %v", err)
	}

	out := buf.String()

	// Must begin with XML header
	if !strings.HasPrefix(out, xml.Header) {
		t.Errorf("output does not start with XML header")
	}

	// Must be parseable as valid XML
	var kml KMLDocument
	xmlBody := out[len(xml.Header):]
	if err := xml.Unmarshal([]byte(xmlBody), &kml); err != nil {
		t.Fatalf("output is not valid XML: %v\n---\n%s", err, out)
	}

	// Document name must be "Twisty Roads"
	if kml.Doc.Name != "Twisty Roads" {
		t.Errorf("document name: got %q, want %q", kml.Doc.Name, "Twisty Roads")
	}

	// Five tier styles must be present
	if len(kml.Doc.Styles) != 5 {
		t.Errorf("styles: got %d, want 5", len(kml.Doc.Styles))
	}

	// Verify exact folder count and key road names.
	// Expected: Mountain Pass Rd (1) + Interstate 90 (1) + Route 100 (2) +
	//           Main Street (2) + Route 9 & 20 (1) + Flat Street (1) = 8.
	// Unnamed road is excluded; all collections pass when minScore=0.
	const wantFolders = 8
	if len(kml.Doc.Folders) != wantFolders {
		t.Errorf("folder count: got %d, want %d", len(kml.Doc.Folders), wantFolders)
	}

	// Key road names must appear as folder names.
	folderNames := make(map[string]bool, len(kml.Doc.Folders))
	for _, f := range kml.Doc.Folders {
		folderNames[f.Name] = true
	}
	for _, wantName := range []string{"Mountain Pass Rd", "Interstate 90", "Route 9 & 20"} {
		if !folderNames[wantName] {
			t.Errorf("expected folder named %q in KML output", wantName)
		}
	}

	// Verify all placemark coordinates are in lon,lat,0 format
	for _, folder := range kml.Doc.Folders {
		for _, pm := range folder.Placemarks {
			coords := strings.TrimSpace(pm.LineString.Coordinates)
			if coords == "" {
				continue
			}
			for pair := range strings.FieldsSeq(coords) {
				parts := strings.Split(pair, ",")
				if len(parts) != 3 {
					t.Errorf("folder %q: coordinate %q not in lon,lat,0 format", folder.Name, pair)
					break
				}
				if parts[2] != "0" {
					t.Errorf("folder %q: altitude field is %q, want 0", folder.Name, parts[2])
				}
			}
		}
	}
}

// TestPipelineIntegration_AggregationCorrectness verifies per-road behavior.
func TestPipelineIntegration_AggregationCorrectness(t *testing.T) {
	ways := allFixtures()
	collections := Aggregate(ways)
	ApplyPenalties(collections)

	// Helper: find all collections by name prefix.
	findByName := func(name string) []RoadCollection {
		var found []RoadCollection
		for _, c := range collections {
			if c.Name == name {
				found = append(found, c)
			}
		}
		return found
	}

	// "Mountain Pass Rd" — must appear as exactly one collection with a high score.
	t.Run("MountainPassRd_OneCollection", func(t *testing.T) {
		matches := findByName("Mountain Pass Rd")
		if len(matches) != 1 {
			t.Fatalf("expected 1 collection for 'Mountain Pass Rd', got %d", len(matches))
		}
		if matches[0].TotalScore == 0 {
			t.Error("Mountain Pass Rd should have a non-zero score")
		}
	})

	// "Interstate 90" — must have penalty applied (HighwayPenalty["motorway"] = 0.3).
	t.Run("Interstate90_PenaltyApplied", func(t *testing.T) {
		matches := findByName("Interstate 90")
		if len(matches) == 0 {
			t.Fatal("expected at least one collection for 'Interstate 90'")
		}
		verifiedNonZero := 0
		for _, rc := range matches {
			if rc.TotalScore == 0 {
				continue // straight segments only; no penalty to verify
			}
			verifiedNonZero++
			wantFactor := 0.3
			if rc.HighwayPenaltyFactor != wantFactor {
				t.Errorf("Interstate 90: HighwayPenaltyFactor = %v, want %v", rc.HighwayPenaltyFactor, wantFactor)
			}
			if rc.PenalizedScore >= rc.TotalScore {
				t.Errorf("Interstate 90: PenalizedScore (%v) should be less than TotalScore (%v)", rc.PenalizedScore, rc.TotalScore)
			}
		}
		// The fixture contains tier-2 and tier-3 on-ramp segments, so at least one
		// non-zero-score collection must have been verified. If this assertion fails,
		// the fixture has changed or the penalty logic is broken.
		if verifiedNonZero == 0 {
			t.Error("Interstate 90: no non-zero-score collections were verified; penalty assertion never executed")
		}
	})

	// "Route 100" — must be split into exactly two collections (straight gap > 2414 m).
	t.Run("Route100_SplitIntoTwo", func(t *testing.T) {
		matches := findByName("Route 100")
		if len(matches) != 2 {
			t.Errorf("expected 2 collections for 'Route 100' (straight gap split), got %d", len(matches))
		}
	})

	// "Main Street" — two geographically disconnected clusters → two collections.
	t.Run("MainStreet_TwoDisconnected", func(t *testing.T) {
		matches := findByName("Main Street")
		if len(matches) != 2 {
			t.Errorf("expected 2 collections for 'Main Street' (disconnected), got %d", len(matches))
		}
	})

	// Unnamed road — must not appear in any collection.
	t.Run("UnnamedRoad_Excluded", func(t *testing.T) {
		for _, rc := range collections {
			for _, id := range rc.WayIDs {
				if id == 500 {
					t.Errorf("unnamed road (way ID 500) should not appear in any collection")
				}
			}
		}
	})
}

// TestPipelineIntegration_OrderingAndFiltering verifies sort order and minScore filter.
func TestPipelineIntegration_OrderingAndFiltering(t *testing.T) {
	ways := allFixtures()
	collections := Aggregate(ways)
	ApplyPenalties(collections)

	// Find Mountain Pass Rd and Interstate 90 collections.
	var mountainScore, interstateScore float64
	for _, rc := range collections {
		if rc.Name == "Mountain Pass Rd" {
			mountainScore = rc.PenalizedScore
		}
		if rc.Name == "Interstate 90" && rc.PenalizedScore > interstateScore {
			interstateScore = rc.PenalizedScore
		}
	}

	// Mountain Pass Rd (secondary, penalty=0.9) should outrank Interstate 90 (motorway, penalty=0.3).
	t.Run("MountainPassOutranksInterstate", func(t *testing.T) {
		if mountainScore == 0 {
			t.Skip("Mountain Pass Rd has score 0; cannot compare")
		}
		if interstateScore >= mountainScore {
			t.Errorf("expected Mountain Pass Rd score (%.2f) > Interstate 90 score (%.2f)", mountainScore, interstateScore)
		}
	})

	// KML output is sorted descending by penalized score.
	t.Run("KMLSortedDescending", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteKML(&buf, collections, 0); err != nil {
			t.Fatalf("WriteKML: %v", err)
		}
		var kml KMLDocument
		if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
			t.Fatalf("invalid XML: %v", err)
		}
		// Build a score lookup by folder name.
		scoreByName := make(map[string]float64)
		for _, rc := range collections {
			if existing, ok := scoreByName[rc.DisplayName()]; !ok || rc.PenalizedScore > existing {
				scoreByName[rc.DisplayName()] = rc.PenalizedScore
			}
		}
		// Verify each folder appears in non-increasing score order.
		prev := -1.0
		for i, folder := range kml.Doc.Folders {
			score := scoreByName[folder.Name]
			if i > 0 && score > prev {
				t.Errorf("folders not sorted descending at position %d: %q (%.2f) > prev (%.2f)",
					i, folder.Name, score, prev)
			}
			prev = score
		}
	})

	// minScore filter excludes low-scoring collections.
	t.Run("MinScoreFilter", func(t *testing.T) {
		// Find the max penalized score.
		var maxScore float64
		for _, rc := range collections {
			if rc.PenalizedScore > maxScore {
				maxScore = rc.PenalizedScore
			}
		}
		if maxScore == 0 {
			t.Skip("all collections have score 0; cannot test minScore filter")
		}
		// Set minScore to just above 80% of the max — should exclude at least some collections.
		minScore := maxScore * 0.8
		var buf bytes.Buffer
		if err := WriteKML(&buf, collections, minScore); err != nil {
			t.Fatalf("WriteKML: %v", err)
		}
		var kml KMLDocument
		if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
			t.Fatalf("invalid XML: %v", err)
		}
		// The filter must have actually excluded at least one collection.
		// If WriteKML had a bug and returned all collections, this assertion catches it.
		if len(kml.Doc.Folders) >= len(collections) {
			t.Fatalf("MinScoreFilter: folder count %d >= total collections %d; filter did not exclude anything",
				len(kml.Doc.Folders), len(collections))
		}
		for _, folder := range kml.Doc.Folders {
			score := scoreByFolder(collections, folder.Name)
			if score < minScore {
				t.Errorf("folder %q has score %.2f below minScore %.2f", folder.Name, score, minScore)
			}
		}
	})
}

// scoreByFolder looks up the penalized score for the given folder name from collections.
func scoreByFolder(collections []RoadCollection, name string) float64 {
	for _, rc := range collections {
		if rc.DisplayName() == name {
			return rc.PenalizedScore
		}
	}
	return 0
}

// TestPipelineIntegration_SpecialCharactersInRoadName verifies XML is well-formed
// when road names contain characters like '&'.
func TestPipelineIntegration_SpecialCharactersInRoadName(t *testing.T) {
	ways := fixtureSpecialCharsRoad()
	collections := Aggregate(ways)
	ApplyPenalties(collections)

	var buf bytes.Buffer
	if err := WriteKML(&buf, collections, 0); err != nil {
		t.Fatalf("WriteKML: %v", err)
	}

	out := buf.String()

	// Must be valid XML (xml.Unmarshal will fail if & is unescaped).
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(out[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("XML with special chars is invalid: %v\n%s", err, out)
	}

	// The road name must appear in a folder.
	found := false
	for _, folder := range kml.Doc.Folders {
		if folder.Name == "Route 9 & 20" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected folder named 'Route 9 & 20' in KML output")
	}
}

// TestPipelineIntegration_EmptyInput verifies that empty input produces valid empty KML.
func TestPipelineIntegration_EmptyInput(t *testing.T) {
	collections := Aggregate(nil)
	ApplyPenalties(collections)

	var buf bytes.Buffer
	if err := WriteKML(&buf, collections, 0); err != nil {
		t.Fatalf("WriteKML: %v", err)
	}

	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("empty input produced invalid XML: %v", err)
	}
	if kml.Doc.Name != "Twisty Roads" {
		t.Errorf("document name: got %q, want %q", kml.Doc.Name, "Twisty Roads")
	}
	if len(kml.Doc.Folders) != 0 {
		t.Errorf("expected 0 folders for empty input, got %d", len(kml.Doc.Folders))
	}
}

// TestPipelineIntegration_AllBelowMinScore verifies valid empty KML when all are filtered.
func TestPipelineIntegration_AllBelowMinScore(t *testing.T) {
	ways := fixtureTwistyMountainPass()
	collections := Aggregate(ways)
	ApplyPenalties(collections)

	var buf bytes.Buffer
	// Use an extremely high minScore to exclude everything.
	if err := WriteKML(&buf, collections, 1e18); err != nil {
		t.Fatalf("WriteKML: %v", err)
	}

	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("all-below-min produced invalid XML: %v", err)
	}
	if len(kml.Doc.Folders) != 0 {
		t.Errorf("expected 0 folders when all below minScore, got %d", len(kml.Doc.Folders))
	}
}

// TestPipelineIntegration_SingleWayRoad verifies a single-way road produces one collection.
func TestPipelineIntegration_SingleWayRoad(t *testing.T) {
	ways := []ScoredWay{
		{
			WayID: 800,
			Tags:  map[string]string{"name": "Lone Way", "highway": "tertiary"},
			Segments: []ScoredSegment{
				makeScoredSegment(800, 3, 44.70, -73.00, 44.701, -73.00, 200.0),
			},
		},
	}

	collections := Aggregate(ways)
	ApplyPenalties(collections)

	if len(collections) != 1 {
		t.Fatalf("expected 1 collection for single-way road, got %d", len(collections))
	}
	if collections[0].Name != "Lone Way" {
		t.Errorf("collection name: got %q, want %q", collections[0].Name, "Lone Way")
	}

	var buf bytes.Buffer
	if err := WriteKML(&buf, collections, 0); err != nil {
		t.Fatalf("WriteKML: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("single-way KML is invalid XML: %v", err)
	}
	if len(kml.Doc.Folders) != 1 {
		t.Errorf("expected 1 folder, got %d", len(kml.Doc.Folders))
	}
}

// TestPipelineIntegration_AllTierZeroSegments verifies a road with all tier-0 segments
// produces a collection with score 0, which can be filtered out or included.
func TestPipelineIntegration_AllTierZeroSegments(t *testing.T) {
	ways := fixtureAllTierZeroRoad()
	collections := Aggregate(ways)
	ApplyPenalties(collections)

	if len(collections) != 1 {
		t.Fatalf("expected 1 collection for all-tier-0 road, got %d", len(collections))
	}
	rc := collections[0]
	if rc.TotalScore != 0 {
		t.Errorf("expected TotalScore = 0 for all-tier-0 road, got %v", rc.TotalScore)
	}
	if rc.PenalizedScore != 0 {
		t.Errorf("expected PenalizedScore = 0 for all-tier-0 road, got %v", rc.PenalizedScore)
	}

	// KML with minScore=0 should include it; with minScore>0 should exclude it.
	var buf bytes.Buffer
	if err := WriteKML(&buf, collections, 0); err != nil {
		t.Fatalf("WriteKML: %v", err)
	}
	var kml KMLDocument
	if err := xml.Unmarshal([]byte(buf.String()[len(xml.Header):]), &kml); err != nil {
		t.Fatalf("all-tier-0 KML is invalid XML: %v", err)
	}
	if len(kml.Doc.Folders) != 1 {
		t.Errorf("expected 1 folder for all-tier-0 road with minScore=0, got %d", len(kml.Doc.Folders))
	}

	// Now filter with minScore=1: should produce 0 folders.
	var buf2 bytes.Buffer
	if err := WriteKML(&buf2, collections, 1.0); err != nil {
		t.Fatalf("WriteKML(minScore=1): %v", err)
	}
	var kml2 KMLDocument
	if err := xml.Unmarshal([]byte(buf2.String()[len(xml.Header):]), &kml2); err != nil {
		t.Fatalf("filtered KML is invalid XML: %v", err)
	}
	if len(kml2.Doc.Folders) != 0 {
		t.Errorf("expected 0 folders for all-tier-0 road with minScore=1, got %d", len(kml2.Doc.Folders))
	}
}

// TestPipelineIntegration_SubIndexAssignment verifies DisplayName uses sub-indices correctly.
func TestPipelineIntegration_SubIndexAssignment(t *testing.T) {
	// Use Route 100 which splits into two collections.
	ways := fixtureRoute100()
	collections := Aggregate(ways)
	ApplyPenalties(collections)

	route100 := make([]RoadCollection, 0)
	for _, rc := range collections {
		if rc.Name == "Route 100" {
			route100 = append(route100, rc)
		}
	}
	if len(route100) != 2 {
		t.Fatalf("expected 2 Route 100 collections, got %d", len(route100))
	}

	// One should have sub-index 0 (bare name), one should have sub-index 1 (with suffix).
	subIndices := map[int]bool{}
	for _, rc := range route100 {
		subIndices[rc.SubIndex] = true
	}
	if !subIndices[0] || !subIndices[1] {
		t.Errorf("expected sub-indices 0 and 1, got %v", subIndices)
	}

	displayNames := map[string]bool{}
	for _, rc := range route100 {
		displayNames[rc.DisplayName()] = true
	}
	if !displayNames["Route 100"] {
		t.Error("expected display name 'Route 100' for sub-index 0")
	}
	if !displayNames["Route 100 (2)"] {
		t.Error("expected display name 'Route 100 (2)' for sub-index 1")
	}
}
