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

func TestUpdate_UpToDateRegionsExitEarlyTouchingNothing(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)
	e.load("overpass", "north-america/us/vermont", t1, t1)

	rep, err := Update(context.Background(), e.deps(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, svc := range allServices {
		s := findService(t, rep, svc)
		if s.Outcome != OutcomeUnchanged {
			t.Errorf("%s outcome = %v, want unchanged", svc, s.Outcome)
		}
		if got := findRegion(t, s, "north-america/us/vermont").Reason; got != "up to date" {
			t.Errorf("%s reason = %q", svc, got)
		}
	}
	if len(e.runtime.calls) != 0 {
		t.Errorf("container runtime was touched: %v", e.runtime.calls)
	}
	if rep.Failed() {
		t.Error("report should not be failed")
	}
}

func TestUpdate_MissingStampCountsAsChanged(t *testing.T) {
	e := newUpdaterEnv(t)
	// Upstream looks old, but with no stamp we cannot tell: refresh.
	e.load("overpass", "north-america/us/vermont", time.Time{}, t0)

	rep, _ := Update(context.Background(), e.deps(), UpdateOptions{})

	r := findRegion(t, findService(t, rep, "overpass"), "north-america/us/vermont")
	if !r.Changed || r.Reason != "no stamp" {
		t.Errorf("got changed=%v reason=%q, want changed with reason \"no stamp\"", r.Changed, r.Reason)
	}
}

func TestUpdate_ForceBypassesUnchangedCheck(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)

	rep, _ := Update(context.Background(), e.deps(), UpdateOptions{Force: true, DryRun: true})

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeChanged {
		t.Errorf("outcome = %v, want changed under --force", v.Outcome)
	}
	if r := findRegion(t, v, "north-america/us/vermont"); !r.Changed || r.Reason != "forced" {
		t.Errorf("got changed=%v reason=%q", r.Changed, r.Reason)
	}
}

func TestUpdate_OnlyLimitsServicesConsidered(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t0, t1)
	e.load("overpass", "north-america/us/maine", t0, t1)

	rep, _ := Update(context.Background(), e.deps(), UpdateOptions{Only: "overpass"})

	if len(rep.Services) != 1 || rep.Services[0].Service != "overpass" {
		t.Fatalf("services = %+v, want only overpass", rep.Services)
	}
	for _, h := range e.upstream.heads {
		if h == "north-america/us/vermont" {
			t.Error("valhalla-only region was checked despite --only overpass")
		}
	}
}

func TestUpdate_OnlyRejectsUnknownService(t *testing.T) {
	e := newUpdaterEnv(t)
	if _, err := Update(context.Background(), e.deps(), UpdateOptions{Only: "postgres"}); err == nil {
		t.Error("expected error for unknown --only value")
	}
}

func TestUpdate_NeverConsidersRegionsOutsideRegionsFiles(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)
	// Upstream knows about another region, and a stray PBF exists on disk, but
	// it is in no .regions file: it must never be looked at.
	e.upstream.info["north-america/us/maine"] = UpstreamInfo{LastModified: t1}
	_ = os.WriteFile(filepath.Join(e.root.PBFDir, pbfFilename("north-america/us/maine")), []byte("x"), 0o644)

	rep, _ := Update(context.Background(), e.deps(), UpdateOptions{Force: true})

	for _, s := range rep.Services {
		for _, r := range s.Regions {
			if r.Region == "north-america/us/maine" {
				t.Errorf("%s reported unloaded region maine", s.Service)
			}
		}
	}
	for _, h := range e.upstream.heads {
		if h == "north-america/us/maine" {
			t.Error("unloaded region was HEADed")
		}
	}
}

func TestUpdate_DryRunReportsDataAgeAndModifiesNothing(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t0, t1)
	e.headers["north-america/us/vermont"] = e.now.Add(-36 * time.Hour)
	before := snapshotTree(t, filepath.Dir(e.root.PBFDir))

	rep, err := Update(context.Background(), e.deps(), UpdateOptions{DryRun: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}

	r := findRegion(t, findService(t, rep, "valhalla"), "north-america/us/vermont")
	if !r.HasDataAge || r.DataAge != 36*time.Hour {
		t.Errorf("data age = %v (has=%v), want 36h", r.DataAge, r.HasDataAge)
	}
	if after := snapshotTree(t, filepath.Dir(e.root.PBFDir)); after != before {
		t.Errorf("dry-run modified the data tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if len(e.runtime.calls) != 0 {
		t.Errorf("runtime touched: %v", e.runtime.calls)
	}
}

func TestUpdate_UpstreamErrorFailsThatServiceOnly(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)
	e.load("overpass", "north-america/us/maine", t1, t1)
	e.upstream.err["north-america/us/vermont"] = fmt.Errorf("HEAD: 503")

	rep, _ := Update(context.Background(), e.deps(), UpdateOptions{})

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeFailed || !strings.Contains(v.Detail, "vermont") {
		t.Errorf("valhalla = %v %q, want failed mentioning the region", v.Outcome, v.Detail)
	}
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUnchanged {
		t.Errorf("overpass = %v, want unchanged (failure isolation)", o.Outcome)
	}
	if !rep.Failed() {
		t.Error("report.Failed() should be true")
	}
}

func TestUpdate_ServicesReportedValhallaFirst(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("overpass", "north-america/us/maine", t1, t1)
	e.load("valhalla", "north-america/us/vermont", t1, t1)

	rep, _ := Update(context.Background(), e.deps(), UpdateOptions{})

	if len(rep.Services) != 2 || rep.Services[0].Service != "valhalla" || rep.Services[1].Service != "overpass" {
		t.Errorf("order = %+v, want valhalla then overpass", rep.Services)
	}
}

func TestUpdate_ServiceWithNoRegionsIsUnchanged(t *testing.T) {
	e := newUpdaterEnv(t)
	rep, _ := Update(context.Background(), e.deps(), UpdateOptions{Force: true})
	for _, s := range rep.Services {
		if s.Outcome != OutcomeUnchanged || !strings.Contains(s.Detail, "no regions") {
			t.Errorf("%s = %v %q, want unchanged / no regions loaded", s.Service, s.Outcome, s.Detail)
		}
	}
}

// snapshotTree returns a stable listing of every file under dir with size and mtime.
func snapshotTree(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		fmt.Fprintf(&sb, "%s %d %d\n", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// Installs that predate per-service stamps hold only the download stamp; both
// services honour it until they get a stamp of their own.
func TestUpdate_LegacyDownloadStampCountsForEveryService(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", time.Time{}, t1)
	e.load("overpass", "north-america/us/vermont", time.Time{}, t1)
	writeDownloadStamp(t, e.root.PBFDir, "north-america/us/vermont", t1)

	rep, err := Update(context.Background(), e.deps(), UpdateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range allServices {
		if s := findService(t, rep, svc); s.Outcome != OutcomeUnchanged {
			t.Errorf("%s = %v, want unchanged via the legacy stamp", svc, s.Outcome)
		}
	}
}
