package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflight_ValhallaOnlyShortfallDoesNotSuggestInPlace(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", vermont, t0, t1)
	e.writeSized(filepath.Join(e.root.ValhallaDir, "tiles", "0", "000.gph"), 400)
	deps := e.deps()
	deps.FreeSpace = func(string) (uint64, error) { return 10, nil }

	rep, err := Update(context.Background(), deps, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeFailed || !strings.Contains(v.Detail, "short by") {
		t.Fatalf("valhalla = %v %q, want a preflight shortfall failure", v.Outcome, v.Detail)
	}
	if strings.Contains(v.Detail, "--in-place") {
		t.Errorf("--in-place only affects Overpass, so it must not be suggested for Valhalla: %q", v.Detail)
	}
	var buf bytes.Buffer
	renderPreflight(&buf, rep.Preflight)
	if strings.Contains(buf.String(), "--in-place") {
		t.Errorf("rendered preflight suggests --in-place for a Valhalla-only shortfall:\n%s", buf.String())
	}
}

func TestPreflight_OverpassShortfallRenderSuggestsInPlace(t *testing.T) {
	p := newPreflightEnv(t)
	deps := p.deps()
	deps.FreeSpace = func(string) (uint64, error) { return 100, nil }

	rep, err := Update(context.Background(), deps, UpdateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	renderPreflight(&buf, rep.Preflight)
	if !strings.Contains(buf.String(), "--in-place") {
		t.Errorf("rendered preflight should suggest --in-place for an Overpass shortfall:\n%s", buf.String())
	}
}

func TestPreflight_ReclaimedCachesAreNamedInTheReport(t *testing.T) {
	p := newPreflightEnv(t)
	deps := p.deps()
	deps.FreeSpace = p.diskOf(dirSize(filepath.Dir(p.root.OverpassDir)) + preflightNeeded - 340)

	var buf bytes.Buffer
	if err := runUpdateWith(context.Background(), deps, UpdateOptions{}, &buf); err != nil {
		t.Logf("update error (later stages are stubbed): %v", err)
	}

	out := buf.String()
	for _, want := range []string{"reclaimed Overpass tile cache", "reclaimed Overpass score cache"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestOverpassUpdate_CacheClearMessageSaysWhatWasRemoved(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	o := findService(t, rep, "overpass")
	for _, want := range []string{"cleared tile cache", "1 entry", "score cache"} {
		if !strings.Contains(o.Detail, want) {
			t.Errorf("detail %q missing %q", o.Detail, want)
		}
	}
}
