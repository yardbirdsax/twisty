package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── fakes ───────────────────────────────────────────────────────────────────

type fakeUpstream struct {
	info  map[string]UpstreamInfo
	err   map[string]error
	heads []string
}

func (f *fakeUpstream) Head(_ context.Context, region string) (UpstreamInfo, error) {
	f.heads = append(f.heads, region)
	if err := f.err[region]; err != nil {
		return UpstreamInfo{}, err
	}
	info, ok := f.info[region]
	if !ok {
		return UpstreamInfo{}, fmt.Errorf("fake upstream: unknown region %q", region)
	}
	return info, nil
}

type fakeRuntime struct {
	calls []string
}

func (f *fakeRuntime) IsRunning(name string) bool {
	f.calls = append(f.calls, "IsRunning "+name)
	return true
}

// updaterEnv is a temp-dir data root plus fakes, wired into UpdateDeps.
type updaterEnv struct {
	t        *testing.T
	root     DataRoot
	upstream *fakeUpstream
	runtime  *fakeRuntime
	now      time.Time
	headers  map[string]time.Time // region -> PBF header data timestamp
}

func newUpdaterEnv(t *testing.T) *updaterEnv {
	t.Helper()
	base := t.TempDir()
	e := &updaterEnv{
		t: t,
		root: DataRoot{
			OverpassDir: filepath.Join(base, "overpass"),
			ValhallaDir: filepath.Join(base, "valhalla"),
			PBFDir:      filepath.Join(base, "pbf"),
		},
		upstream: &fakeUpstream{info: map[string]UpstreamInfo{}, err: map[string]error{}},
		runtime:  &fakeRuntime{},
		now:      time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
		headers:  map[string]time.Time{},
	}
	for _, d := range []string{e.root.OverpassDir, e.root.ValhallaDir, e.root.PBFDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *updaterEnv) deps() UpdateDeps {
	return UpdateDeps{
		Upstream: e.upstream,
		Runtime:  e.runtime,
		Root:     e.root,
		Clock:    func() time.Time { return e.now },
		FreeSpace: func(string) (uint64, error) {
			return 1 << 40, nil
		},
		PBFDataTimestamp: func(path string) (time.Time, bool, error) {
			for region, ts := range e.headers {
				if filepath.Base(path) == pbfFilename(region) {
					return ts, true, nil
				}
			}
			return time.Time{}, false, nil
		},
	}
}

// load records region as loaded for service ("overpass" or "valhalla"), with an
// upstream stamp held locally (zero stamp means no stamp file) and an upstream
// Last-Modified.
func (e *updaterEnv) load(service, region string, held, upstream time.Time) {
	e.t.Helper()
	dir := e.root.OverpassDir
	if service == "valhalla" {
		dir = e.root.ValhallaDir
	}
	regionsFile := filepath.Join(dir, ".regions")
	var regions []string
	if b, err := os.ReadFile(regionsFile); err == nil {
		regions = strings.Split(string(b), ",")
	}
	regions = append(regions, region)
	if err := os.WriteFile(regionsFile, []byte(strings.Join(regions, ",")), 0o644); err != nil {
		e.t.Fatal(err)
	}
	pbf := filepath.Join(e.root.PBFDir, pbfFilename(region))
	if err := os.WriteFile(pbf, []byte("pbf"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	if !held.IsZero() {
		if err := writeUpstreamStamp(e.root.PBFDir, region, held); err != nil {
			e.t.Fatal(err)
		}
	}
	e.upstream.info[region] = UpstreamInfo{LastModified: upstream, ContentLength: 100}
}

func findService(t *testing.T, rep UpdateReport, service string) ServiceReport {
	t.Helper()
	for _, s := range rep.Services {
		if s.Service == service {
			return s
		}
	}
	t.Fatalf("no report for service %q in %+v", service, rep.Services)
	return ServiceReport{}
}

func findRegion(t *testing.T, s ServiceReport, region string) RegionStatus {
	t.Helper()
	for _, r := range s.Regions {
		if r.Region == region {
			return r
		}
	}
	t.Fatalf("no status for region %q in %+v", region, s.Regions)
	return RegionStatus{}
}

var (
	t0 = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	t1 = time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
)

// ─── tests ───────────────────────────────────────────────────────────────────

func TestUpdate_RegionNewerUpstreamIsChanged(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t0, t1)

	rep, err := Update(context.Background(), e.deps(), UpdateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeChanged {
		t.Errorf("outcome = %v, want changed", v.Outcome)
	}
	if !findRegion(t, v, "north-america/us/vermont").Changed {
		t.Error("vermont should be marked changed")
	}
}
