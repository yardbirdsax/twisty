package gpx

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

func TestWriteGPX(t *testing.T) {
	points := []geo.Coord{
		{Lat: 37.7749, Lon: -122.4194},
		{Lat: 37.8044, Lon: -122.2712},
		{Lat: 37.8716, Lon: -122.2727},
	}
	trackName := "Twist Route (factor=0.8, score=123)"

	dir := t.TempDir()
	path := filepath.Join(dir, "test.gpx")

	if err := WriteGPX(path, points, trackName); err != nil {
		t.Fatalf("WriteGPX returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	content := string(data)

	// File must start with XML declaration.
	if !strings.HasPrefix(content, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("file does not start with XML declaration; got prefix: %q", content[:min(len(content), 60)])
	}

	// Parse the XML.
	var g GPX
	if err := xml.Unmarshal(data, &g); err != nil {
		t.Fatalf("failed to parse written GPX: %v", err)
	}

	// Root element version.
	if g.Version != "1.1" {
		t.Errorf("expected version 1.1, got %q", g.Version)
	}

	// Creator.
	if g.Creator != "twistrouter" {
		t.Errorf("expected creator twistrouter, got %q", g.Creator)
	}

	// xmlns.
	if g.Xmlns != "http://www.topografix.com/GPX/1/1" {
		t.Errorf("expected xmlns http://www.topografix.com/GPX/1/1, got %q", g.Xmlns)
	}

	// Track name.
	if g.Trk.Name != trackName {
		t.Errorf("expected track name %q, got %q", trackName, g.Trk.Name)
	}

	// Track points.
	if len(g.Trk.TrkSeg.Points) != 3 {
		t.Fatalf("expected 3 track points, got %d", len(g.Trk.TrkSeg.Points))
	}

	for i, want := range points {
		got := g.Trk.TrkSeg.Points[i]
		if got.Lat != want.Lat {
			t.Errorf("point %d: expected lat %v, got %v", i, want.Lat, got.Lat)
		}
		if got.Lon != want.Lon {
			t.Errorf("point %d: expected lon %v, got %v", i, want.Lon, got.Lon)
		}
	}
}

func TestWriteGPX_FileError(t *testing.T) {
	// Attempt to write to an invalid path to verify errors are returned.
	err := WriteGPX("/nonexistent-dir/test.gpx", []geo.Coord{{Lat: 1, Lon: 2}}, "test")
	if err == nil {
		t.Error("expected error writing to invalid path, got nil")
	}
}

func TestWriteGPXWithWaypoints(t *testing.T) {
	points := []geo.Coord{
		{Lat: 37.7749, Lon: -122.4194},
		{Lat: 37.8044, Lon: -122.2712},
	}
	waypoints := []Waypoint{
		{Lat: 37.7749, Lon: -122.4194, Name: "Turn right", Desc: "Turn right onto Main St — 1m30s, 0.50km"},
		{Lat: 37.8044, Lon: -122.2712, Name: "Turn left", Desc: "Turn left onto Oak Ave — 2m00s, 1.20km"},
	}
	trackName := "twisty random"

	dir := t.TempDir()
	path := filepath.Join(dir, "test_wpts.gpx")

	if err := WriteGPXWithWaypoints(path, points, waypoints, trackName); err != nil {
		t.Fatalf("WriteGPXWithWaypoints returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	content := string(data)

	// File must start with XML declaration.
	if !strings.HasPrefix(content, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("file does not start with XML declaration; got prefix: %q", content[:min(len(content), 60)])
	}

	// Must contain <wpt> elements.
	if !strings.Contains(content, "<wpt") {
		t.Error("output does not contain <wpt> elements")
	}

	// Parse the XML back using the GPX struct.
	var g GPX
	if err := xml.Unmarshal(data, &g); err != nil {
		t.Fatalf("failed to parse written GPX: %v", err)
	}

	// Verify waypoint count.
	if len(g.Wpts) != 2 {
		t.Fatalf("expected 2 waypoints, got %d", len(g.Wpts))
	}

	// Verify first waypoint fields.
	wpt := g.Wpts[0]
	if wpt.Lat != 37.7749 {
		t.Errorf("wpt[0].Lat = %v, want 37.7749", wpt.Lat)
	}
	if wpt.Lon != -122.4194 {
		t.Errorf("wpt[0].Lon = %v, want -122.4194", wpt.Lon)
	}
	if wpt.Name != "Turn right" {
		t.Errorf("wpt[0].Name = %q, want %q", wpt.Name, "Turn right")
	}
	if !strings.Contains(wpt.Desc, "Main St") {
		t.Errorf("wpt[0].Desc = %q, want it to contain %q", wpt.Desc, "Main St")
	}

	// Verify track points are still present.
	if len(g.Trk.TrkSeg.Points) != 2 {
		t.Errorf("expected 2 track points, got %d", len(g.Trk.TrkSeg.Points))
	}

	// Verify track name.
	if g.Trk.Name != trackName {
		t.Errorf("track name = %q, want %q", g.Trk.Name, trackName)
	}
}
