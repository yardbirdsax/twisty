package gpx

import (
	"encoding/xml"
	"os"

	"github.com/yardbirdsax/twisty/geo"
)

// GPX represents the root element of a GPX 1.1 document.
type GPX struct {
	XMLName xml.Name `xml:"gpx"`
	Version string   `xml:"version,attr"`
	Creator string   `xml:"creator,attr"`
	Xmlns   string   `xml:"xmlns,attr"`
	Trk     Track    `xml:"trk"`
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

// WriteGPX writes a GPX 1.1 file to the given path containing the route points.
// trackName is used as the trk name value.
func WriteGPX(path string, points []geo.Coord, trackName string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"); err != nil {
		return err
	}

	trkPts := make([]TrackPoint, len(points))
	for i, p := range points {
		trkPts[i] = TrackPoint{Lat: p.Lat, Lon: p.Lon}
	}

	g := GPX{
		Version: "1.1",
		Creator: "twistrouter",
		Xmlns:   "http://www.topografix.com/GPX/1/1",
		Trk: Track{
			Name: trackName,
			TrkSeg: TrackSeg{
				Points: trkPts,
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
