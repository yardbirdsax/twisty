package gpx

import (
	"encoding/xml"
	"os"

	"github.com/yardbirdsax/twisty/geo"
)

const (
	gpxVersion = "1.1"
	gpxCreator = "twistrouter"
	gpxXmlns   = "http://www.topografix.com/GPX/1/1"
)

// Waypoint represents a GPX waypoint element.
type Waypoint struct {
	Lat  float64 `xml:"lat,attr"`
	Lon  float64 `xml:"lon,attr"`
	Name string  `xml:"name,omitempty"`
	Desc string  `xml:"desc,omitempty"`
}

// GPX represents the root element of a GPX 1.1 document.
type GPX struct {
	XMLName xml.Name   `xml:"gpx"`
	Version string     `xml:"version,attr"`
	Creator string     `xml:"creator,attr"`
	Xmlns   string     `xml:"xmlns,attr"`
	Wpts    []Waypoint `xml:"wpt"`
	Trk     Track      `xml:"trk"`
}

// Track represents a GPX track element.
type Track struct {
	Name   string   `xml:"name"`
	TrkSeg TrackSeg `xml:"trkseg"`
}

// TrackSeg represents a GPX track segment element.
type TrackSeg struct {
	Points []TrackPoint `xml:"trkpt"`
}

// TrackPoint represents a GPX track point element.
type TrackPoint struct {
	Lat float64 `xml:"lat,attr"`
	Lon float64 `xml:"lon,attr"`
}

// toTrackPoints converts a slice of geo.Coord into a slice of TrackPoint.
func toTrackPoints(coords []geo.Coord) []TrackPoint {
	pts := make([]TrackPoint, len(coords))
	for i, c := range coords {
		pts[i] = TrackPoint{Lat: c.Lat, Lon: c.Lon}
	}
	return pts
}

// writeGPXTo writes a GPX document to an already-open file.
func writeGPXTo(f *os.File, points []geo.Coord, waypoints []Waypoint, trackName string) error {
	if _, err := f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"); err != nil {
		return err
	}

	g := GPX{
		Version: gpxVersion,
		Creator: gpxCreator,
		Xmlns:   gpxXmlns,
		Wpts:    waypoints,
		Trk: Track{
			Name: trackName,
			TrkSeg: TrackSeg{
				Points: toTrackPoints(points),
			},
		},
	}

	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")
	if err := enc.Encode(g); err != nil {
		return err
	}
	return enc.Flush()
}

// WriteGPX writes a GPX 1.1 file to the given path containing the route points.
// trackName is used as the trk name value.
func WriteGPX(path string, points []geo.Coord, trackName string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeGPXTo(f, points, nil, trackName)
}

// WriteGPXWithWaypoints writes a GPX 1.1 file to the given path containing
// route points and labeled waypoints. trackName is used as the trk name value.
// Waypoints appear before the track in the file (required by the GPX schema).
func WriteGPXWithWaypoints(path string, points []geo.Coord, waypoints []Waypoint, trackName string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeGPXTo(f, points, waypoints, trackName)
}
