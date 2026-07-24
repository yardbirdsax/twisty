package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

func TestCollectionsToGeoJSON(t *testing.T) {
	collections := []quality.RoadCollection{
		{
			Name: "PA 120",
			Segments: []quality.ScoredSegment{
				{
					WayID: 111,
					Start: geo.Coord{Lat: 41.1, Lon: -78.1},
					End:   geo.Coord{Lat: 41.2, Lon: -78.2},
					Tier:  3,
					Score: 142.5,
				},
			},
		},
	}

	fc := collectionsToGeoJSON(collections, quality.ScoredWays{})

	if fc.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", fc.Type)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}
	f := fc.Features[0]
	if f.Geometry.Type != "LineString" {
		t.Fatalf("expected LineString, got %q", f.Geometry.Type)
	}
	if len(f.Geometry.Coordinates) != 2 {
		t.Fatalf("expected 2 coordinates, got %d", len(f.Geometry.Coordinates))
	}
	// GeoJSON coordinate order is [lon, lat]
	if f.Geometry.Coordinates[0][0] != -78.1 || f.Geometry.Coordinates[0][1] != 41.1 {
		t.Fatalf("unexpected start coord: %v", f.Geometry.Coordinates[0])
	}
	if f.Properties.RoadName != "PA 120" {
		t.Fatalf("expected road_name PA 120, got %q", f.Properties.RoadName)
	}
	if f.Properties.Tier != 3 {
		t.Fatalf("expected tier 3, got %d", f.Properties.Tier)
	}
	if f.Properties.Score != 142.5 {
		t.Fatalf("expected score 142.5, got %f", f.Properties.Score)
	}
	if f.Properties.WayID != 111 {
		t.Fatalf("expected way_id 111, got %d", f.Properties.WayID)
	}

	// Verify it round-trips through JSON cleanly
	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var roundtrip geoJSONFeatureCollection
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if roundtrip.Features[0].Properties.RoadName != "PA 120" {
		t.Fatalf("round-trip road_name mismatch")
	}
}

func TestFilterFeaturesByBBox(t *testing.T) {
	fc := geoJSONFeatureCollection{
		Type: "FeatureCollection",
		Features: []geoJSONFeature{
			{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type:        "LineString",
					Coordinates: [][2]float64{{-78.1, 41.1}, {-78.2, 41.2}},
				},
				Properties: geoJSONSegmentProps{RoadName: "Inside"},
			},
			{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type:        "LineString",
					Coordinates: [][2]float64{{-80.0, 45.0}, {-80.1, 45.1}},
				},
				Properties: geoJSONSegmentProps{RoadName: "Outside"},
			},
			{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type:        "LineString",
					Coordinates: [][2]float64{{-78.5, 41.5}, {-80.0, 45.0}},
				},
				Properties: geoJSONSegmentProps{RoadName: "Straddles"},
			},
		},
	}

	t.Run("returns only features whose start falls within bbox", func(t *testing.T) {
		got := filterFeaturesByBBox(fc, -79.0, 41.0, -78.0, 42.0)
		if len(got.Features) != 2 {
			t.Fatalf("expected 2 features, got %d", len(got.Features))
		}
		names := map[string]bool{}
		for _, f := range got.Features {
			names[f.Properties.RoadName] = true
		}
		if !names["Inside"] {
			t.Errorf("expected Inside to be included")
		}
		if !names["Straddles"] {
			t.Errorf("expected Straddles to be included (start point inside bbox)")
		}
		if names["Outside"] {
			t.Errorf("expected Outside to be excluded")
		}
	})

	t.Run("returns all features when bbox is empty string values", func(t *testing.T) {
		got := filterFeaturesByBBox(fc, -180, -90, 180, 90)
		if len(got.Features) != 3 {
			t.Fatalf("expected 3 features, got %d", len(got.Features))
		}
	})
}

func TestDiagServeHandlers(t *testing.T) {
	collections := []quality.RoadCollection{
		{
			Name: "Test Road",
			Segments: []quality.ScoredSegment{
				{
					WayID: 1,
					Start: geo.Coord{Lat: 40.0, Lon: -75.0},
					End:   geo.Coord{Lat: 40.1, Lon: -75.1},
					Tier:  2,
					Score: 50.0,
				},
			},
		},
	}

	fc := collectionsToGeoJSON(collections, quality.ScoredWays{})
	fcJSON, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	mux, err := buildDiagMux(fcJSON)
	if err != nil {
		t.Fatalf("buildDiagMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("GET / returns HTML", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html content-type, got %q", ct)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "leaflet@1.9.4") {
			t.Fatalf("HTML body missing Leaflet script tag")
		}
	})

	t.Run("GET /api/segments returns GeoJSON", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/segments")
		if err != nil {
			t.Fatalf("GET /api/segments: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected application/json, got %q", ct)
		}
		var fc geoJSONFeatureCollection
		if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
			t.Fatalf("decode GeoJSON: %v", err)
		}
		if fc.Type != "FeatureCollection" {
			t.Fatalf("expected FeatureCollection, got %q", fc.Type)
		}
		if len(fc.Features) != 1 {
			t.Fatalf("expected 1 feature, got %d", len(fc.Features))
		}
		if fc.Features[0].Properties.RoadName != "Test Road" {
			t.Fatalf("expected Test Road, got %q", fc.Features[0].Properties.RoadName)
		}
	})

	t.Run("GET /unknown returns 404", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/does-not-exist")
		if err != nil {
			t.Fatalf("GET /does-not-exist: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})
}
