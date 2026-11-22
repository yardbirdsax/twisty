package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// overpassEnv is an updaterEnv with a live Overpass install: an old database on
// disk and the old container running. The fake convert step stands in for the
// PBF -> merged.osm.bz2 conversion.
type overpassEnv struct {
	*updaterEnv
	db        string
	merged    string
	converted [][]string
	convErr   error
}

func newOverpassEnv(t *testing.T, regions ...string) *overpassEnv {
	t.Helper()
	e := &overpassEnv{updaterEnv: newUpdaterEnv(t)}
	e.db = filepath.Join(e.root.OverpassDir, "db")
	e.merged = filepath.Join(e.root.OverpassDir, "merged.osm.bz2")
	for _, r := range regions {
		e.load("overpass", r, t0, t0)
	}
	e.writeSized(filepath.Join(e.db, "nodes.dat"), 1000)
	e.writeSized(e.merged, 100)
	e.runtime.seedRunning(overpassContainerSpec(overpassContainerName, defaultOverpassPort, e.db, e.merged))
	return e
}

func (e *overpassEnv) makeChanged(region string) {
	e.upstream.info[region] = UpstreamInfo{LastModified: t1, ContentLength: 100}
}

func (e *overpassEnv) update(t *testing.T, opts UpdateOptions) UpdateReport {
	t.Helper()
	opts.Only = "overpass"
	deps := e.deps()
	deps.ConvertOverpass = func(_ context.Context, regions []string) error {
		e.converted = append(e.converted, regions)
		if e.convErr != nil {
			return e.convErr
		}
		return os.WriteFile(e.merged, []byte("new merged"), 0o644)
	}
	rep, err := Update(context.Background(), deps, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestOverpassInPlace_StopsWipesAndReimports(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)

	var dbEmptyAtRun, mergedNewAtRun bool
	e.runtime.OnRun = func(ContainerSpec) error {
		entries, _ := os.ReadDir(e.db)
		dbEmptyAtRun = len(entries) == 0
		b, _ := os.ReadFile(e.merged)
		mergedNewAtRun = string(b) == "new merged"
		return nil
	}

	rep := e.update(t, UpdateOptions{InPlace: true})

	op := findService(t, rep, "overpass")
	if op.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated", op.Outcome, op.Detail)
	}
	wantCalls := []string{
		"stop " + overpassContainerName,
		"rm " + overpassContainerName,
		"run " + overpassContainerName,
		"ready " + overpassReadyURL(defaultOverpassPort),
	}
	if !reflect.DeepEqual(e.runtime.calls, wantCalls) {
		t.Errorf("calls = %v\nwant    %v", e.runtime.calls, wantCalls)
	}
	if !dbEmptyAtRun {
		t.Error("database must be wiped before the container re-imports")
	}
	if !mergedNewAtRun {
		t.Error("container must be started on the refreshed merged archive")
	}
	if !reflect.DeepEqual(e.converted, [][]string{{vermont}}) {
		t.Errorf("converted = %v, want all loaded regions once", e.converted)
	}
	if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
		t.Errorf("downloads = %v", e.upstream.downloads)
	}
	if got, ok := readUpstreamStamp(e.root.PBFDir, vermont); !ok || !got.Equal(t1) {
		t.Errorf("stamp = %v (ok=%v), want %v", got, ok, t1)
	}
	if !e.runtime.IsRunning(overpassContainerName) {
		t.Error("container should be running again")
	}
	spec, _ := e.runtime.spec(overpassContainerName)
	if host, _ := mountHostDir(spec, "/db"); host != e.db {
		t.Errorf("container mounts db %q, want %q", host, e.db)
	}
}

func TestOverpassInPlace_ConvertsAllLoadedRegionsNotOnlyChanged(t *testing.T) {
	e := newOverpassEnv(t, vermont, maine)
	e.makeChanged(vermont)

	rep := e.update(t, UpdateOptions{InPlace: true})

	if op := findService(t, rep, "overpass"); op.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s)", op.Outcome, op.Detail)
	}
	if !reflect.DeepEqual(e.converted, [][]string{{vermont, maine}}) {
		t.Errorf("converted = %v, want both regions", e.converted)
	}
	if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
		t.Errorf("only the changed region should download, got %v", e.upstream.downloads)
	}
}

func TestOverpassUpdate_WithoutInPlaceStagesInsteadOfWipingFirst(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	// The staged path imports into a staging container before the live one is
	// stopped; the in-place path never starts a staging container.
	e.runtime.OnStop = func(name string) error {
		if name == overpassContainerName && len(e.runtime.calls) < 3 {
			t.Errorf("live container stopped before the staged import finished: %v", e.runtime.calls)
		}
		return nil
	}

	rep := e.update(t, UpdateOptions{})

	if op := findService(t, rep, "overpass"); op.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated by the staged path", op.Outcome, op.Detail)
	}
	staged := false
	for _, c := range e.runtime.calls {
		if c == "run "+overpassContainerName+"-staging" {
			staged = true
		}
	}
	if !staged {
		t.Errorf("no staging container started: %v", e.runtime.calls)
	}
}

func TestOverpassInPlace_ShortfallOnlyWarns(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	deps := e.deps()
	deps.FreeSpace = func(string) (uint64, error) { return 10, nil }
	deps.ConvertOverpass = func(context.Context, []string) error {
		return os.WriteFile(e.merged, []byte("new merged"), 0o644)
	}

	rep, err := Update(context.Background(), deps, UpdateOptions{Only: "overpass", InPlace: true})
	if err != nil {
		t.Fatal(err)
	}

	if rep.Preflight == nil || rep.Preflight.Shortfall == 0 || rep.Preflight.Fits {
		t.Fatalf("preflight should still report the shortfall, got %+v", rep.Preflight)
	}
	if !rep.Preflight.Warn {
		t.Error("preflight should be flagged as a warning, not an abort")
	}
	if op := findService(t, rep, "overpass"); op.Outcome != OutcomeUpdated {
		t.Errorf("outcome = %v (%s), want updated despite shortfall", op.Outcome, op.Detail)
	}
}

func TestOverpassWithoutInPlace_ShortfallStillAborts(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	deps := e.deps()
	deps.FreeSpace = func(string) (uint64, error) { return 10, nil }

	rep, err := Update(context.Background(), deps, UpdateOptions{Only: "overpass"})
	if err != nil {
		t.Fatal(err)
	}
	if op := findService(t, rep, "overpass"); op.Outcome != OutcomeFailed {
		t.Errorf("outcome = %v, want failed", op.Outcome)
	}
	if len(e.runtime.calls) != 0 {
		t.Errorf("no fallback to in-place: calls = %v", e.runtime.calls)
	}
}

func TestOverpassInPlace_DownloadFailureLeavesOverpassUp(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	e.upstream.downloadErr[vermont] = errors.New("boom")

	rep := e.update(t, UpdateOptions{InPlace: true})

	op := findService(t, rep, "overpass")
	if op.Outcome != OutcomeFailed || !strings.Contains(op.Detail, "download") {
		t.Errorf("outcome = %v detail %q, want failed at download", op.Outcome, op.Detail)
	}
	if len(e.runtime.calls) != 0 || !e.runtime.IsRunning(overpassContainerName) {
		t.Errorf("container must be left running; calls = %v", e.runtime.calls)
	}
}

func TestOverpassInPlace_ConvertFailureKeepsDatabase(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	e.convErr = errors.New("bad pbf")

	rep := e.update(t, UpdateOptions{InPlace: true})

	op := findService(t, rep, "overpass")
	if op.Outcome != OutcomeFailed || !strings.Contains(op.Detail, "convert") {
		t.Errorf("outcome = %v detail %q, want failed at convert", op.Outcome, op.Detail)
	}
	if len(e.runtime.calls) != 0 {
		t.Errorf("nothing should be stopped before conversion succeeds: %v", e.runtime.calls)
	}
	if _, err := os.Stat(filepath.Join(e.db, "nodes.dat")); err != nil {
		t.Errorf("old database must survive a failed conversion: %v", err)
	}
}

func TestOverpassInPlace_ImportFailureIsReportedAndNotStamped(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	e.runtime.OnWaitReady = func(string) error { return errors.New("never ready") }

	rep := e.update(t, UpdateOptions{InPlace: true})

	op := findService(t, rep, "overpass")
	if op.Outcome != OutcomeFailed || !strings.Contains(op.Detail, "import") {
		t.Errorf("outcome = %v detail %q, want failed at import", op.Outcome, op.Detail)
	}
	if got, _ := readUpstreamStamp(e.root.PBFDir, vermont); !got.Equal(t0) {
		t.Errorf("stamp = %v, want old stamp kept so the next run retries", got)
	}
}
