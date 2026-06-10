package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/yardbirdsax/twisty/diag"
	"github.com/yardbirdsax/twisty/quality"
)

type geoJSONSegmentProps struct {
	RoadName string  `json:"road_name"`
	Tier     int     `json:"tier"`
	Score    float64 `json:"score"`
	WayID    int64   `json:"way_id"`
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

const diagHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>twisty diag</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"/>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<style>
  html, body, #map { height: 100%; margin: 0; padding: 0; }
  #tooltip {
    position: fixed;
    background: rgba(15,17,23,0.92);
    color: #e2e8f0;
    border: 1px solid #334155;
    border-radius: 6px;
    padding: 8px 12px;
    font-family: system-ui, sans-serif;
    font-size: 13px;
    pointer-events: none;
    display: none;
    z-index: 9999;
    line-height: 1.6;
  }
  #tooltip .road { font-weight: 600; color: #7dd3fc; }
</style>
</head>
<body>
<div id="map"></div>
<div id="tooltip"><div class="road"></div><div class="detail"></div></div>
<div id="loading" style="position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);background:rgba(15,17,23,0.92);color:#e2e8f0;border:1px solid #334155;border-radius:8px;padding:16px 24px;font-family:system-ui,sans-serif;font-size:14px;z-index:9999;">Loading segments…</div>
<script>
const TIER_COLORS = ['#475569','#4ade80','#facc15','#fb923c','#f87171'];
const map = L.map('map').setView([39, -77], 7);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
  attribution: '© OpenStreetMap contributors', maxZoom: 19
}).addTo(map);

const tooltip = document.getElementById('tooltip');
const loading = document.getElementById('loading');

const MIN_ZOOM = 12;
let geoLayer = null;
let byRoad = {};
let activeGroup = null;
let fetchController = null;

function segmentStyle(feature) {
  const tier = feature.properties.tier;
  return { color: TIER_COLORS[Math.max(0, Math.min(tier, 4))], weight: 3, opacity: 0.8 };
}

function loadSegments() {
  if (map.getZoom() < MIN_ZOOM) {
    if (geoLayer) { map.removeLayer(geoLayer); geoLayer = null; byRoad = {}; activeGroup = null; }
    loading.textContent = 'Zoom in (level ' + MIN_ZOOM + '+) to see road segments.';
    loading.style.display = 'block';
    return;
  }

  if (fetchController) fetchController.abort();
  fetchController = new AbortController();

  const b = map.getBounds();
  const bbox = b.getWest().toFixed(6) + ',' + b.getSouth().toFixed(6) + ',' +
               b.getEast().toFixed(6) + ',' + b.getNorth().toFixed(6);
  loading.textContent = 'Loading…';
  loading.style.display = 'block';

  fetch('/api/segments?bbox=' + bbox, { signal: fetchController.signal })
    .then(r => r.json())
    .then(fc => {
      if (geoLayer) { map.removeLayer(geoLayer); byRoad = {}; activeGroup = null; }

      geoLayer = L.geoJSON(fc, {
        style: segmentStyle,
        onEachFeature: function(feature, layer) {
          const name = feature.properties.road_name;
          if (!byRoad[name]) byRoad[name] = [];
          byRoad[name].push(layer);

          layer.on('mouseover', function() {
            const p = feature.properties;
            const group = byRoad[p.road_name] || [];
            if (activeGroup && activeGroup !== group) {
              activeGroup.forEach(l => l.setStyle({ weight: 3, opacity: 0.8 }));
            }
            if (group !== activeGroup) {
              group.forEach(l => l.setStyle({ weight: 5, opacity: 1.0 }));
              activeGroup = group;
            }
            tooltip.querySelector('.road').textContent = p.road_name;
            tooltip.querySelector('.detail').textContent =
              'Tier ' + p.tier + ' · Score ' + p.score.toFixed(1);
            tooltip.style.display = 'block';
          });
          layer.on('mousemove', function(e) {
            tooltip.style.left = (e.originalEvent.clientX + 14) + 'px';
            tooltip.style.top  = (e.originalEvent.clientY - 10) + 'px';
          });
          layer.on('mouseout', function() { tooltip.style.display = 'none'; });
        }
      }).addTo(map);

      loading.textContent = fc.features.length + ' segments loaded.';
      setTimeout(() => { loading.style.display = 'none'; }, 1500);
    })
    .catch(err => { if (err.name !== 'AbortError') console.error('fetch segments:', err); });
}

map.on('zoomend moveend', loadSegments);
loadSegments();
</script>
</body>
</html>`

func diagServe(cacheDir string, port int) error {
	cfg := diag.DefaultCacheConfig()
	if cacheDir != "" {
		cfg.OverpassDir = cacheDir
	}

	if _, err := os.Stat(cfg.OverpassDir); os.IsNotExist(err) {
		return fmt.Errorf("cache directory does not exist: %s", cfg.OverpassDir)
	}

	tiles, err := diag.AllCachedTiles(cfg)
	if err != nil {
		return fmt.Errorf("reading cache: %w", err)
	}
	if len(tiles) == 0 {
		return fmt.Errorf("no tiles found in cache directory: %s\nRun 'twisty score' first to populate the cache", cfg.OverpassDir)
	}

	fmt.Fprintf(os.Stderr, "Running scoring pipeline over %d tiles...\n", len(tiles))
	t0 := time.Now()
	result, err := diag.SimulatePipelineFull(tiles, cfg, diag.SimulatePipelineOptions{})
	if err != nil {
		return fmt.Errorf("running pipeline: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Pipeline done in %s: %d collections\n", time.Since(t0).Round(time.Millisecond), len(result.Collections))

	fmt.Fprintf(os.Stderr, "Serializing segments to GeoJSON...\n")
	t1 := time.Now()
	fc := collectionsToGeoJSON(result.Collections)
	fcJSON, err := json.Marshal(fc)
	if err != nil {
		return fmt.Errorf("serializing GeoJSON: %w", err)
	}
	fmt.Fprintf(os.Stderr, "GeoJSON serialization done in %s: %d segments, %.1f KB\n",
		time.Since(t1).Round(time.Millisecond), len(fc.Features), float64(len(fcJSON))/1024)
	mux, err := buildDiagMux(fcJSON)
	if err != nil {
		return fmt.Errorf("building mux: %w", err)
	}

	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", port, err)
	}

	fmt.Fprintf(os.Stderr, "Listening on http://localhost:%d\n", port)
	return http.Serve(ln, mux)
}

func buildDiagMux(fcJSON []byte) (*http.ServeMux, error) {
	var fc geoJSONFeatureCollection
	if err := json.Unmarshal(fcJSON, &fc); err != nil {
		return nil, fmt.Errorf("unmarshal GeoJSON: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte(diagHTML)); err != nil {
			log.Printf("diag: write /: %v", err)
		}
	})
	mux.HandleFunc("/api/segments", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		west, south, east, north, err := parseBBox(q.Get("bbox"))
		if err != nil {
			http.Error(w, "invalid bbox", http.StatusBadRequest)
			return
		}
		filtered := filterFeaturesByBBox(fc, west, south, east, north)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(filtered); err != nil {
			log.Printf("diag: write /api/segments: %v", err)
		}
	})
	return mux, nil
}

func parseBBox(s string) (west, south, east, north float64, err error) {
	if s == "" {
		return -180, -90, 180, 90, nil
	}
	_, err = fmt.Sscanf(s, "%f,%f,%f,%f", &west, &south, &east, &north)
	return
}

func filterFeaturesByBBox(fc geoJSONFeatureCollection, west, south, east, north float64) geoJSONFeatureCollection {
	out := geoJSONFeatureCollection{Type: "FeatureCollection", Features: []geoJSONFeature{}}
	for _, f := range fc.Features {
		if len(f.Geometry.Coordinates) == 0 {
			continue
		}
		lon, lat := f.Geometry.Coordinates[0][0], f.Geometry.Coordinates[0][1]
		if lon >= west && lon <= east && lat >= south && lat <= north {
			out.Features = append(out.Features, f)
		}
	}
	return out
}

func collectionsToGeoJSON(collections []quality.RoadCollection) geoJSONFeatureCollection {
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
			fc.Features = append(fc.Features, geoJSONFeature{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type: "LineString",
					Coordinates: [][2]float64{
						{seg.Start.Lon, seg.Start.Lat},
						{seg.End.Lon, seg.End.Lat},
					},
				},
				Properties: geoJSONSegmentProps{
					RoadName: c.DisplayName(),
					Tier:     seg.Tier,
					Score:    seg.Score,
					WayID:    seg.WayID,
				},
			})
		}
	}
	return fc
}
