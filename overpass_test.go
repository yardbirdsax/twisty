package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestDockerCmdStreaming_Success(t *testing.T) {
	err := dockerCmdStreaming("echo", "hello")
	if err != nil {
		t.Fatalf("dockerCmdStreaming returned error for valid command: %v", err)
	}
}

func TestDockerCmdStreaming_Failure(t *testing.T) {
	err := dockerCmdStreaming("false")
	if err == nil {
		t.Fatal("dockerCmdStreaming should return error for failing command")
	}
}

func TestOverpassDockerRunArgs(t *testing.T) {
	args := overpassDockerRunArgs(8080, "/abs/data/db", "/abs/data/merged.osm.bz2")

	// Verify the BZ2 file is mounted at /data/planet.osm.bz2 (not /db/, which is a separate mount).
	if !slices.Contains(args, "/abs/data/merged.osm.bz2:/data/planet.osm.bz2:ro") {
		t.Errorf("args missing BZ2 volume mount\ngot: %v", args)
	}

	// Verify essential env vars are present.
	for _, want := range []string{
		"OVERPASS_MODE=init",
		"OVERPASS_USE_AREAS=false",
		"OVERPASS_COMPRESSION=no",
		"OVERPASS_PLANET_URL=file:///data/planet.osm.bz2",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("args missing %q\ngot: %v", want, args)
		}
	}

	// Verify port mapping.
	if !slices.Contains(args, "8080:80") {
		t.Errorf("args missing port mapping 8080:80\ngot: %v", args)
	}

	// Verify the container image is the overpass image (not a separate osmium image).
	lastArg := args[len(args)-1]
	if lastArg != overpassImage {
		t.Errorf("expected image %q, got %q", overpassImage, lastArg)
	}
}

func TestMergeRegions(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		add      []string
		want     []string
	}{
		{
			name:     "disjoint sets are unioned",
			existing: []string{"north-america/us/pennsylvania"},
			add:      []string{"north-america/us/georgia"},
			want:     []string{"north-america/us/georgia", "north-america/us/pennsylvania"},
		},
		{
			name:     "duplicate regions are deduplicated",
			existing: []string{"north-america/us/pennsylvania"},
			add:      []string{"north-america/us/pennsylvania"},
			want:     []string{"north-america/us/pennsylvania"},
		},
		{
			name:     "empty existing set",
			existing: nil,
			add:      []string{"north-america/us/pennsylvania", "north-america/us/georgia"},
			want:     []string{"north-america/us/georgia", "north-america/us/pennsylvania"},
		},
		{
			name:     "result is always sorted",
			existing: []string{"north-america/us/pennsylvania"},
			add:      []string{"north-america/us/alabama", "north-america/us/georgia"},
			want:     []string{"north-america/us/alabama", "north-america/us/georgia", "north-america/us/pennsylvania"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeRegions(tt.existing, tt.add)
			if !slices.Equal(got, tt.want) {
				t.Errorf("mergeRegions(%v, %v) = %v, want %v", tt.existing, tt.add, got, tt.want)
			}
		})
	}
}

func TestProbeOverpass_ReturnsTrue_WhenAPIReturnsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`))
	}))
	defer srv.Close()

	if !probeOverpass(srv.URL) {
		t.Error("probeOverpass returned false for healthy server")
	}
}

func TestProbeOverpass_ReturnsFalse_WhenAPIReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if probeOverpass(srv.URL) {
		t.Error("probeOverpass returned true for unhealthy server")
	}
}

func TestProbeOverpass_ReturnsFalse_WhenServerUnreachable(t *testing.T) {
	if probeOverpass("http://127.0.0.1:1") {
		t.Error("probeOverpass returned true for unreachable server")
	}
}

func TestWaitForOverpass_ReturnsNil_WhenAPIBecomesReady(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"elements":[]}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	// Simulate the API becoming ready after a short delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		ready.Store(true)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := waitForOverpass(ctx, srv.URL)
	if err != nil {
		t.Fatalf("waitForOverpass returned error: %v", err)
	}
}

func TestWaitForOverpass_ReturnsError_WhenContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := waitForOverpass(ctx, srv.URL)
	if err == nil {
		t.Fatal("waitForOverpass should return error when context times out")
	}
}
