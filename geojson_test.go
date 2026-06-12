package main

import (
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

func TestFilterFeaturesByBBox_segmentEndInsideBBox(t *testing.T) {
	// A segment whose start is outside the bbox but end is inside should be included.
	fc := geoJSONFeatureCollection{
		Type: "FeatureCollection",
		Features: []geoJSONFeature{
			{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type: "LineString",
					Coordinates: [][2]float64{
						{-77.0, 38.9}, // start: outside bbox (west of it)
						{-76.9, 39.0}, // end: inside bbox
					},
				},
				Properties: geoJSONSegmentProps{WayID: 1, Tier: 2},
			},
		},
	}

	result := filterFeaturesByBBox(fc, -76.95, 38.95, -76.85, 39.05)

	if len(result.Features) != 1 {
		t.Errorf("expected 1 feature (segment end inside bbox), got %d", len(result.Features))
	}
}

func TestFilterFeaturesByBBox_bothEndpointsOutside(t *testing.T) {
	// A segment with both endpoints outside the bbox should be excluded.
	fc := geoJSONFeatureCollection{
		Type: "FeatureCollection",
		Features: []geoJSONFeature{
			{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type: "LineString",
					Coordinates: [][2]float64{
						{-78.0, 38.0},
						{-78.1, 38.1},
					},
				},
				Properties: geoJSONSegmentProps{WayID: 2, Tier: 1},
			},
		},
	}

	result := filterFeaturesByBBox(fc, -76.95, 38.95, -76.85, 39.05)

	if len(result.Features) != 0 {
		t.Errorf("expected 0 features (both endpoints outside bbox), got %d", len(result.Features))
	}
}

func TestFilterFeaturesByBBox_startInsideBBox(t *testing.T) {
	// A segment whose start is inside the bbox should always be included (existing behavior).
	fc := geoJSONFeatureCollection{
		Type: "FeatureCollection",
		Features: []geoJSONFeature{
			{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type: "LineString",
					Coordinates: [][2]float64{
						{-76.9, 39.0}, // start: inside bbox
						{-77.0, 38.9}, // end: outside bbox
					},
				},
				Properties: geoJSONSegmentProps{WayID: 3, Tier: 3},
			},
		},
	}

	result := filterFeaturesByBBox(fc, -76.95, 38.95, -76.85, 39.05)

	if len(result.Features) != 1 {
		t.Errorf("expected 1 feature (segment start inside bbox), got %d", len(result.Features))
	}
}

func TestCollectionsToRoadGeoJSON_emptyInput(t *testing.T) {
	fc := collectionsToRoadGeoJSON(nil)
	if fc.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", fc.Type)
	}
	if len(fc.Features) != 0 {
		t.Fatalf("expected 0 features, got %d", len(fc.Features))
	}
}

func TestCollectionsToRoadGeoJSON_colorAndCoordinates(t *testing.T) {
	col := quality.RoadCollection{
		Name:       "PA 100",
		TotalScore: 300,
		Segments: []quality.ScoredSegment{
			{
				Start: geo.Coord{Lat: 40.1, Lon: -75.1},
				End:   geo.Coord{Lat: 40.2, Lon: -75.2},
			},
			{
				Start: geo.Coord{Lat: 40.2, Lon: -75.2},
				End:   geo.Coord{Lat: 40.3, Lon: -75.3},
			},
		},
	}
	fc := collectionsToRoadGeoJSON([]quality.RoadCollection{col})
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}
	f := fc.Features[0]
	if f.Properties.RoadName != "PA 100" {
		t.Errorf("road_name = %q, want %q", f.Properties.RoadName, "PA 100")
	}
	if f.Properties.Score != 300 {
		t.Errorf("score = %v, want 300", f.Properties.Score)
	}
	if f.Geometry.Type != "MultiLineString" {
		t.Errorf("geometry type = %q, want MultiLineString", f.Geometry.Type)
	}
	if len(f.Geometry.Lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(f.Geometry.Lines))
	}
	line0 := f.Geometry.Lines[0]
	if len(line0) != 3 {
		t.Fatalf("line 0: expected 3 points, got %d", len(line0))
	}
	if line0[0] != [2]float64{-75.1, 40.1} {
		t.Errorf("line 0 point 0: got %v, want [-75.1 40.1] (lon,lat order)", line0[0])
	}
	if line0[2] != [2]float64{-75.3, 40.3} {
		t.Errorf("line 0 point 2: got %v, want [-75.3 40.3] (lon,lat order)", line0[2])
	}
	wantColor := "#ffdf00" // TotalScore=300: CurvatureColorLevel=33, GradientColorCSS level 33
	if f.Properties.Color != wantColor {
		t.Errorf("color = %q, want %q", f.Properties.Color, wantColor)
	}
}

func TestFilterRoadFeaturesByBBox(t *testing.T) {
	bbox := [4]float64{-76.0, 40.0, -75.0, 41.0} // west, south, east, north

	makeFeature := func(name string, lines [][][2]float64) geoJSONRoadFeature {
		return geoJSONRoadFeature{
			Type:       "Feature",
			Geometry:   geoJSONMultiLineGeometry{Type: "MultiLineString", Lines: lines},
			Properties: geoJSONRoadProps{RoadName: name},
		}
	}

	tests := []struct {
		name      string
		features  []geoJSONRoadFeature
		wantNames []string
	}{
		{
			name: "single coord inside bbox is kept",
			features: []geoJSONRoadFeature{
				makeFeature("Inside", [][][2]float64{{{-75.5, 40.5}}}),
			},
			wantNames: []string{"Inside"},
		},
		{
			name: "single coord outside bbox is dropped",
			features: []geoJSONRoadFeature{
				makeFeature("Outside", [][][2]float64{{{-80.0, 45.0}}}),
			},
			wantNames: []string{},
		},
		{
			name: "second line string touches bbox — feature is kept",
			features: []geoJSONRoadFeature{
				makeFeature("MultiLine", [][][2]float64{
					{{-80.0, 45.0}, {-79.0, 44.0}}, // entirely outside
					{{-75.5, 40.5}},                  // inside
				}),
			},
			wantNames: []string{"MultiLine"},
		},
		{
			name: "multiple coords per line, only last coord inside — feature is kept",
			features: []geoJSONRoadFeature{
				makeFeature("MultiCoord", [][][2]float64{
					{{-80.0, 45.0}, {-75.5, 40.5}},
				}),
			},
			wantNames: []string{"MultiCoord"},
		},
		{
			name: "mix of inside and outside features",
			features: []geoJSONRoadFeature{
				makeFeature("Inside", [][][2]float64{{{-75.5, 40.5}}}),
				makeFeature("Outside", [][][2]float64{{{-80.0, 45.0}}}),
			},
			wantNames: []string{"Inside"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := geoJSONRoadFeatureCollection{Type: "FeatureCollection", Features: tc.features}
			result := filterRoadFeaturesByBBox(fc, bbox[0], bbox[1], bbox[2], bbox[3])
			if len(result.Features) != len(tc.wantNames) {
				t.Fatalf("got %d features, want %d", len(result.Features), len(tc.wantNames))
			}
			for i, want := range tc.wantNames {
				if result.Features[i].Properties.RoadName != want {
					t.Errorf("feature[%d] name = %q, want %q", i, result.Features[i].Properties.RoadName, want)
				}
			}
		})
	}
}
