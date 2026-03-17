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

