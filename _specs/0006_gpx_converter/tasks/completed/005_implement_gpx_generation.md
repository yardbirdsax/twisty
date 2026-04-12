# Task 005: Implement GPX File Generation

## Summary

Implement the GPX file generation logic that transforms `RouteData` into a valid GPX 1.1 file. This task extends the existing `gpx/` package by adding a converter that builds on the existing `WriteGPXWithWaypoints` function. The generated GPX files must be importable into offline navigation apps like OSMAnd without errors.

## Dependencies

Task 001, Task 004 - route data types and Google Maps API client must be in place.

## Context: Existing GPX Package

**IMPORTANT:** The `gpx/` package already contains a working GPX implementation in `gpx/gpx.go`:

- `Waypoint` struct — GPX `<wpt>` element with `Lat`, `Lon`, `Name`, `Desc` fields
- `TrackPoint` struct — GPX `<trkpt>` element with `Lat`, `Lon` fields
- `GPX`, `Track`, `TrackSeg` structs — full GPX 1.1 document structure
- `WriteGPXWithWaypoints(path, points []geo.Coord, waypoints []Waypoint, trackName string) error` — writes a GPX file with waypoints and track
- `WriteGPX(path, points []geo.Coord, trackName string) error` — writes a GPX file with track only

This task should reuse these existing types and functions rather than redefine them. The converter's job is to translate `RouteData` (intermediate type from Task 001) into the types the existing functions already accept.

## Detailed Directions

### 1. Implement the Route-to-GPX Converter

Create `gpx/converter.go`:

```go
package gpx

import (
	"fmt"

	"github.com/yardbirdsax/twisty/geo"
)

// ConvertRouteToGPX converts RouteData into a GPX 1.1 file at the given path.
// It reuses the existing WriteGPXWithWaypoints function.
func ConvertRouteToGPX(data *RouteData, outPath string) error {
	if len(data.RouteWaypoints) == 0 && len(data.TrackPoints) == 0 {
		return fmt.Errorf("route must contain either waypoints or track points")
	}

	// Validate all waypoint coordinates
	for i, wp := range data.RouteWaypoints {
		if !isValidLatitude(wp.Latitude) || !isValidLongitude(wp.Longitude) {
			return fmt.Errorf("waypoint %d (%q) has invalid coordinates: lat=%f, lon=%f", i, wp.Name, wp.Latitude, wp.Longitude)
		}
	}

	// Validate all track point coordinates
	for i, tp := range data.TrackPoints {
		if !isValidLatitude(tp.Latitude) || !isValidLongitude(tp.Longitude) {
			return fmt.Errorf("track point %d has invalid coordinates: lat=%f, lon=%f", i, tp.Latitude, tp.Longitude)
		}
	}

	// Convert RouteWaypoints to the existing Waypoint type
	wpts := make([]Waypoint, len(data.RouteWaypoints))
	for i, rw := range data.RouteWaypoints {
		wpts[i] = Waypoint{
			Lat:  rw.Latitude,
			Lon:  rw.Longitude,
			Name: rw.Name,
		}
	}

	// Convert TrackCoords to geo.Coord (used by existing WriteGPXWithWaypoints)
	coords := make([]geo.Coord, len(data.TrackPoints))
	for i, tp := range data.TrackPoints {
		coords[i] = geo.Coord{Lat: tp.Latitude, Lon: tp.Longitude}
	}

	trackName := fmt.Sprintf("%s to %s", data.StartName, data.DestinationName)
	return WriteGPXWithWaypoints(outPath, coords, wpts, trackName)
}

func isValidLatitude(lat float64) bool {
	return lat >= -90 && lat <= 90
}

func isValidLongitude(lon float64) bool {
	return lon >= -180 && lon <= 180
}
```

### 2. Write Unit Tests

Create `gpx/converter_test.go`:

```go
package gpx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertRouteToGPX(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	data := &RouteData{
		StartName:       "Home",
		DestinationName: "Work",
		RouteWaypoints: []RouteWaypoint{
			{Name: "Home", Latitude: 40.7128, Longitude: -74.0060},
			{Name: "Work", Latitude: 40.7580, Longitude: -73.9855},
		},
		TrackPoints: []TrackCoord{
			{Latitude: 40.7128, Longitude: -74.0060},
			{Latitude: 40.7200, Longitude: -74.0050},
			{Latitude: 40.7580, Longitude: -73.9855},
		},
	}

	if err := ConvertRouteToGPX(data, outPath); err != nil {
		t.Fatalf("ConvertRouteToGPX failed: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	xml := string(content)
	if !strings.Contains(xml, `<?xml version="1.0"`) {
		t.Error("XML declaration missing")
	}
	if !strings.Contains(xml, `version="1.1"`) {
		t.Error("GPX version attribute missing")
	}
	if !strings.Contains(xml, "<wpt") {
		t.Error("waypoint elements missing")
	}
	if !strings.Contains(xml, "<trk>") {
		t.Error("track element missing")
	}
	if !strings.Contains(xml, "<trkpt") {
		t.Error("track point elements missing")
	}
	if !strings.Contains(xml, "Home") {
		t.Error("waypoint name 'Home' not found in XML")
	}
	if !strings.Contains(xml, "Work") {
		t.Error("waypoint name 'Work' not found in XML")
	}
}

func TestConvertRouteToGPX_InvalidCoordinates(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	tests := []struct {
		name    string
		data    *RouteData
		wantErr bool
	}{
		{
			name: "invalid latitude",
			data: &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: 91, Longitude: 0},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid longitude",
			data: &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: 0, Longitude: 181},
				},
			},
			wantErr: true,
		},
		{
			name: "valid boundary coordinates",
			data: &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: 90, Longitude: 180},
					{Name: "B", Latitude: -90, Longitude: -180},
				},
				TrackPoints: []TrackCoord{
					{Latitude: 90, Longitude: 180},
					{Latitude: -90, Longitude: -180},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ConvertRouteToGPX(tt.data, outPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRouteToGPX: expected error=%v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestConvertRouteToGPX_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	data := &RouteData{
		StartName:       "A",
		DestinationName: "B",
		RouteWaypoints:  []RouteWaypoint{},
		TrackPoints:     []TrackCoord{},
	}

	if err := ConvertRouteToGPX(data, outPath); err == nil {
		t.Error("ConvertRouteToGPX should fail for empty route")
	}
}

func TestConvertRouteToGPX_LargeRoute(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	trackPoints := make([]TrackCoord, 10000)
	for i := 0; i < 10000; i++ {
		trackPoints[i] = TrackCoord{
			Latitude:  40.0 + float64(i)*0.00001,
			Longitude: -74.0 + float64(i)*0.00001,
		}
	}

	data := &RouteData{
		StartName:       "Start",
		DestinationName: "End",
		RouteWaypoints: []RouteWaypoint{
			{Name: "Start", Latitude: 40.0, Longitude: -74.0},
			{Name: "End", Latitude: 40.1, Longitude: -73.9},
		},
		TrackPoints: trackPoints,
	}

	if err := ConvertRouteToGPX(data, outPath); err != nil {
		t.Fatalf("ConvertRouteToGPX failed for large route: %v", err)
	}
}
```

### 3. Verify Build

```bash
go build ./...
go test ./gpx/... -v
```

All tests should pass.

## Acceptance Criteria

- [ ] `ConvertRouteToGPX` converts `*RouteData` to a GPX 1.1 file at the given path
- [ ] Reuses existing `WriteGPXWithWaypoints` rather than reimplementing XML generation
- [ ] Converts `RouteWaypoint` → existing `Waypoint` type for GPX output
- [ ] Converts `TrackCoord` → `geo.Coord` for GPX output
- [ ] Coordinates are validated (latitude ±90, longitude ±180) before writing
- [ ] Invalid coordinates cause a clear error; no partial file is written
- [ ] Empty routes (no waypoints and no track points) cause an error
- [ ] Track name is set to `"<StartName> to <DestinationName>"`
- [ ] Generated GPX file can be parsed by standard XML parser
- [ ] Unit tests pass for valid routes, coordinate validation, empty routes, and large routes
- [ ] `go build ./...` compiles without errors

## Notes

- **Do not redefine `Waypoint`, `Track`, `TrackSeg`, `TrackPoint`, or `GPX`** — these already exist in `gpx/gpx.go`.
- The converter's role is translation: `RouteData` → existing GPX types → file output.
- The existing `WriteGPXWithWaypoints` already handles XML marshaling and file I/O.
- `geo.Coord` is the existing coordinate type used by the GPX package (`Lat`, `Lon` fields).

---

# Task 005 Review: Implement GPX File Generation

**Reviewer:** Principal Engineer
**Date:** 2026-04-11
**Verdict:** APPROVED

---

## Summary

Implements `ConvertRouteToGPX` in `gpx/converter.go` which translates `RouteData` into a GPX 1.1 file by reusing the existing `WriteGPXWithWaypoints` function. Includes coordinate validation and full unit test coverage.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/gpx/converter.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/gpx/converter_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `ConvertRouteToGPX` converts `*RouteData` to GPX 1.1 file at given path | PASS |
| Reuses existing `WriteGPXWithWaypoints` | PASS |
| Converts `RouteWaypoint` → `Waypoint` | PASS |
| Converts `TrackCoord` → `geo.Coord` | PASS |
| Coordinates validated (latitude ±90, longitude ±180) before writing | PASS |
| Invalid coordinates cause clear error; no partial file written | PASS |
| Empty routes cause an error | PASS |
| Track name set to `"<StartName> to <DestinationName>"` | PASS |
| Generated GPX file can be parsed by standard XML parser | PASS |
| Unit tests pass for valid routes, coordinate validation, empty routes, large routes | PASS |
| `go build ./...` compiles without errors | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go build ./...          # clean
go vet ./gpx/...        # clean
go test ./gpx/... -v -run TestConvert  # all 4 tests PASS
make test               # all packages PASS
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met, all tests pass, build and vet are clean.
