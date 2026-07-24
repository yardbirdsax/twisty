package main

import (
	"fmt"

	"github.com/yardbirdsax/twisty/quality"
)

type geoJSONSegmentProps struct {
	RoadName string  `json:"road_name"`
	Tier     int     `json:"tier"`
	Score    float64 `json:"score"`
	WayID    int64   `json:"way_id"`
	Highway  *string `json:"highway,omitempty"`
	Maxspeed *string `json:"maxspeed,omitempty"`
}

type geoJSONGeometry struct {
	Type        string       `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

type geoJSONFeature struct {
	Type       string              `json:"type"`
	Geometry   geoJSONGeometry     `json:"geometry"`
	Properties geoJSONSegmentProps `json:"properties"`
}

type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

func collectionsToGeoJSON(collections []quality.RoadCollection, scoredWays quality.ScoredWays) geoJSONFeatureCollection {
	tagsByWayID := make(map[int64]map[string]string, len(scoredWays))
	for _, sw := range scoredWays {
		tagsByWayID[sw.WayID] = sw.Tags
	}

	total := 0
	for _, c := range collections {
		total += len(c.Segments)
	}
	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]geoJSONFeature, 0, total),
	}
	for _, c := range collections {
		for _, seg := range c.Segments {
			props := geoJSONSegmentProps{
				RoadName: c.DisplayName(),
				Tier:     seg.Tier,
				Score:    seg.Score,
				WayID:    seg.WayID,
			}
			if tags, ok := tagsByWayID[seg.WayID]; ok {
				if hw := tags["highway"]; hw != "" {
					props.Highway = &hw
				}
				if ms := tags["maxspeed"]; ms != "" {
					props.Maxspeed = &ms
				}
			}
			fc.Features = append(fc.Features, geoJSONFeature{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type: "LineString",
					Coordinates: [][2]float64{
						{seg.Start.Lon, seg.Start.Lat},
						{seg.End.Lon, seg.End.Lat},
					},
				},
				Properties: props,
			})
		}
	}
	return fc
}

func filterFeaturesByBBox(fc geoJSONFeatureCollection, west, south, east, north float64) geoJSONFeatureCollection {
	out := geoJSONFeatureCollection{Type: "FeatureCollection", Features: []geoJSONFeature{}}
	for _, f := range fc.Features {
		if len(f.Geometry.Coordinates) == 0 {
			continue
		}
		for _, coord := range f.Geometry.Coordinates {
			lon, lat := coord[0], coord[1]
			if lon >= west && lon <= east && lat >= south && lat <= north {
				out.Features = append(out.Features, f)
				break
			}
		}
	}
	return out
}

func parseBBox(s string) (west, south, east, north float64, err error) {
	if s == "" {
		return -180, -90, 180, 90, nil
	}
	_, err = fmt.Sscanf(s, "%f,%f,%f,%f", &west, &south, &east, &north)
	return
}

type geoJSONRoadProps struct {
	RoadName    string   `json:"road_name"`
	Score       float64  `json:"score"`
	Color       string   `json:"color"`
	MaxSpeedMPH *float64 `json:"max_speed_mph"`
}

type geoJSONMultiLineGeometry struct {
	Type  string         `json:"type"`
	Lines [][][2]float64 `json:"coordinates"`
}

type geoJSONRoadFeature struct {
	Type       string                   `json:"type"`
	Geometry   geoJSONMultiLineGeometry `json:"geometry"`
	Properties geoJSONRoadProps         `json:"properties"`
}

type geoJSONRoadFeatureCollection struct {
	Type     string               `json:"type"`
	Features []geoJSONRoadFeature `json:"features"`
}

func filterRoadFeaturesByBBox(fc geoJSONRoadFeatureCollection, west, south, east, north float64) geoJSONRoadFeatureCollection {
	out := geoJSONRoadFeatureCollection{Type: "FeatureCollection", Features: []geoJSONRoadFeature{}}
outer:
	for _, f := range fc.Features {
		for _, line := range f.Geometry.Lines {
			for _, coord := range line {
				if coord[0] >= west && coord[0] <= east && coord[1] >= south && coord[1] <= north {
					out.Features = append(out.Features, f)
					continue outer
				}
			}
		}
	}
	return out
}

func collectionsToRoadGeoJSON(collections []quality.RoadCollection) geoJSONRoadFeatureCollection {
	fc := geoJSONRoadFeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]geoJSONRoadFeature, 0, len(collections)),
	}
	for _, c := range collections {
		if len(c.Segments) == 0 {
			continue
		}
		level := quality.CurvatureColorLevel(c.TotalScore, quality.DefaultMinCurvature, quality.DefaultMaxCurvature)
		color := quality.GradientColorCSS(level)

		pts := make([][2]float64, 0, len(c.Segments)+1)
		for i, seg := range c.Segments {
			if i == 0 {
				pts = append(pts, [2]float64{seg.Start.Lon, seg.Start.Lat})
			}
			pts = append(pts, [2]float64{seg.End.Lon, seg.End.Lat})
		}
		lines := [][][2]float64{pts}

		var maxSpeedMPH *float64
		if mph, ok := quality.WeightedAverageSpeedMPH(c.WaySpeeds); ok {
			maxSpeedMPH = &mph
		}

		fc.Features = append(fc.Features, geoJSONRoadFeature{
			Type: "Feature",
			Geometry: geoJSONMultiLineGeometry{
				Type:  "MultiLineString",
				Lines: lines,
			},
			Properties: geoJSONRoadProps{
				RoadName:    c.DisplayName(),
				Score:       c.TotalScore,
				Color:       color,
				MaxSpeedMPH: maxSpeedMPH,
			},
		})
	}
	return fc
}
