package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestContainerSpec_DockerRunArgs(t *testing.T) {
	spec := ContainerSpec{
		Name:    "c1",
		Image:   "img",
		Ports:   []PortMapping{{Host: 8080, Container: 80}},
		Mounts:  []Mount{{Host: "/h", Container: "/c"}},
		Restart: "unless-stopped",
		Command: []string{"serve", "--x"},
	}

	got := spec.dockerRunArgs(true)
	want := []string{"run", "-d", "--name", "c1", "--restart", "unless-stopped",
		"-p", "8080:80", "-v", "/h:/c", "img", "serve", "--x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("detached = %v\nwant %v", got, want)
	}

	oneShot := ContainerSpec{Image: "img", Mounts: []Mount{{Host: "/h", Container: "/c"}}, Command: []string{"go"}}
	got = oneShot.dockerRunArgs(false)
	want = []string{"run", "--rm", "-v", "/h:/c", "img", "go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("one-shot = %v\nwant %v", got, want)
	}
}

func TestIsNoSuchContainer(t *testing.T) {
	if !isNoSuchContainer("Error response from daemon: No such container: twisty-valhalla") {
		t.Error("should recognise docker's missing-container message")
	}
	if isNoSuchContainer("permission denied") {
		t.Error("unrelated error misclassified")
	}
}

func TestGeofabrikPBFURL(t *testing.T) {
	// Verified against Geofabrik's index-v1.json: each region lives in a
	// directory of its own name, and the file is named for the last segment.
	cases := map[string]string{
		"north-america/us/vermont": "https://download.geofabrik.de/north-america/us/vermont/vermont-latest.osm.pbf",
		"europe/monaco":            "https://download.geofabrik.de/europe/monaco/monaco-latest.osm.pbf",
		"north-america/us":         "https://download.geofabrik.de/north-america/us/us-latest.osm.pbf",
	}
	for region, want := range cases {
		if got := geofabrikPBFURL(geofabrikBaseURL, region); got != want {
			t.Errorf("%s: %s, want %s", region, got, want)
		}
	}
}

// fakeGeofabrik serves HEAD/GET at the real URL layout, redirecting -latest to
// a dated file like the real site does.
func fakeGeofabrik(t *testing.T, lastModified time.Time) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/north-america/us/vermont/vermont-latest.osm.pbf", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/north-america/us/vermont/vermont-261121.osm.pbf", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/north-america/us/vermont/vermont-261121.osm.pbf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
		http.ServeContent(w, r, "", lastModified, strings.NewReader("PBFBYTES"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGeofabrikUpstream_HeadAndDownload(t *testing.T) {
	lm := time.Date(2026, 11, 21, 11, 26, 47, 0, time.UTC)
	srv := fakeGeofabrik(t, lm)
	up := geofabrikUpstream{client: srv.Client(), baseURL: srv.URL}

	info, err := up.Head(context.Background(), "north-america/us/vermont")
	if err != nil {
		t.Fatal(err)
	}
	if !info.LastModified.Equal(lm) || info.ContentLength != int64(len("PBFBYTES")) {
		t.Errorf("head = %+v", info)
	}

	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	got, err := up.Download(context.Background(), "north-america/us/vermont", dest)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "PBFBYTES" {
		t.Errorf("downloaded %q", b)
	}
	if !got.LastModified.Equal(lm) {
		t.Errorf("Download returned Last-Modified %v, want %v", got.LastModified, lm)
	}
	// Download leaves stamping to the updater, which only stamps after the
	// refresh has succeeded.
	if _, err := os.Stat(dest + ".upstream"); err == nil {
		t.Error("Download must not write the upstream stamp")
	}
}

func TestGeofabrikUpstream_DownloadErrorLeavesNoFile(t *testing.T) {
	srv := fakeGeofabrik(t, time.Now())
	up := geofabrikUpstream{client: srv.Client(), baseURL: srv.URL}
	dir := t.TempDir()
	dest := filepath.Join(dir, "x.osm.pbf")

	if _, err := up.Download(context.Background(), "europe/nowhere", dest); err == nil {
		t.Fatal("expected an error for a 404")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}
