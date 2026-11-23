package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestUpdateCmd_DataAndCacheDirFlagsMapToLocations(t *testing.T) {
	var got UpdateLocations
	cmd := newUpdateCmdWith(func(_ context.Context, _ UpdateOptions, loc UpdateLocations, _ io.Writer) error {
		got = loc
		return nil
	})
	cmd.SetArgs([]string{"--data-dir", "/d", "--cache-dir", "/c/overpass"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if want := (UpdateLocations{DataDir: "/d", CacheDir: "/c/overpass"}); got != want {
		t.Errorf("locations = %+v, want %+v", got, want)
	}
}

func TestDataRootFor_CustomDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := dataRootFor(UpdateLocations{DataDir: "/d", CacheDir: "/c/overpass"})

	want := DataRoot{
		OverpassDir:   "/d/overpass",
		ValhallaDir:   "/d/valhalla",
		PBFDir:        filepath.Join(home, ".twisty", "pbf"), // as for `start`, never moved by --data-dir
		TileCacheDir:  "/c/overpass",
		ScoreCacheDir: "/c/scores", // beside the tile cache, as `score` derives it
	}
	if root != want {
		t.Errorf("root = %+v\nwant   %+v", root, want)
	}
}

func TestDataRootFor_DefaultsMatchHomeLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := dataRootFor(UpdateLocations{})

	tw := filepath.Join(home, ".twisty")
	want := DataRoot{
		OverpassDir:   filepath.Join(tw, "overpass"),
		ValhallaDir:   filepath.Join(tw, "valhalla"),
		PBFDir:        filepath.Join(tw, "pbf"),
		TileCacheDir:  filepath.Join(tw, "cache", "overpass"),
		ScoreCacheDir: filepath.Join(tw, "cache", "scores"),
	}
	if root != want {
		t.Errorf("root = %+v\nwant   %+v", root, want)
	}
}

func TestBackfillCmd_HonoursDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	data := t.TempDir()
	writeLoadedRegions(t, filepath.Join(data, "valhalla"), "europe/malta")
	pbfDir := resolvePBFDir("")
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pbfDir, pbfFilename("europe/malta")), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newUpdateCmd()
	cmd.SetArgs([]string{"backfill-stamps", "--data-dir", data})
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if _, ok := readServiceStamp(pbfDir, ServiceValhalla, "europe/malta"); !ok {
		t.Error("backfill did not read the region list under --data-dir")
	}
}

// The full-run wiring: the production deps are built from the locations.
func TestProductionUpdateDepsAt_UsesLocations(t *testing.T) {
	deps := productionUpdateDepsAt(UpdateLocations{DataDir: "/d", CacheDir: "/c/overpass"})
	if !reflect.DeepEqual(deps.Root, dataRootFor(UpdateLocations{DataDir: "/d", CacheDir: "/c/overpass"})) {
		t.Errorf("root = %+v", deps.Root)
	}
	if deps.Clock == nil || deps.Clock().Sub(time.Now()) > time.Minute {
		t.Error("clock not wired")
	}
}
