package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const newHampshire = "north-america/us/new-hampshire"

// holdUntilAll makes each download wait until n downloads are in flight at
// once, so a sequential fetch cannot get past the first region.
func holdUntilAll(n int) func(ctx context.Context, region, destPath string) error {
	var (
		mu      sync.Mutex
		started int
		all     = make(chan struct{})
	)
	return func(ctx context.Context, region, _ string) error {
		mu.Lock()
		started++
		if started == n {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			mu.Lock()
			defer mu.Unlock()
			return fmt.Errorf("only %d of %d downloads in flight together", started, n)
		}
	}
}

func TestConcurrentDownload_ChangedRegionsDownloadAtTheSameTime(t *testing.T) {
	e := newValhallaEnv(t, vermont, maine, newHampshire)
	for _, r := range []string{vermont, maine, newHampshire} {
		e.makeChanged(r)
	}
	e.upstream.onDownload = holdUntilAll(3)

	rep := e.update(t, UpdateOptions{})

	if got := findService(t, rep, "valhalla").Outcome; got != OutcomeUpdated {
		t.Errorf("valhalla outcome = %v (%s), want updated", got, findService(t, rep, "valhalla").Detail)
	}
}

func TestConcurrentDownload_FailureCancelsTheRestAndNamesTheRegion(t *testing.T) {
	e := newValhallaEnv(t, vermont, maine, newHampshire)
	for _, r := range []string{vermont, maine, newHampshire} {
		e.makeChanged(r)
	}
	inFlight := holdUntilAll(3)
	e.upstream.onDownload = func(ctx context.Context, region, destPath string) error {
		// Everything is in flight before maine breaks; the others would hang
		// forever were they not cancelled.
		if err := inFlight(ctx, region, destPath); err != nil {
			return err
		}
		if region == maine {
			return errors.New("connection reset")
		}
		<-ctx.Done()
		return ctx.Err()
	}

	rep := e.update(t, UpdateOptions{})

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeFailed {
		t.Fatalf("valhalla outcome = %v, want failed", v.Outcome)
	}
	if !strings.Contains(v.Detail, maine) || !strings.Contains(v.Detail, "connection reset") {
		t.Errorf("detail %q should name %s and its error", v.Detail, maine)
	}
	e.assertNothingLeftBehind(t, vermont, maine, newHampshire)
}

func TestConcurrentDownload_ContextCancellationStopsAllInFlight(t *testing.T) {
	e := newValhallaEnv(t, vermont, maine, newHampshire)
	for _, r := range []string{vermont, maine, newHampshire} {
		e.makeChanged(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inFlight := holdUntilAll(3)
	e.upstream.onDownload = func(ctx context.Context, region, destPath string) error {
		if err := inFlight(ctx, region, destPath); err != nil {
			return err
		}
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	deps := e.deps()
	deps.Root = e.root

	done := make(chan UpdateReport, 1)
	go func() {
		rep, _ := Update(ctx, deps, UpdateOptions{Only: "valhalla"})
		done <- rep
	}()
	select {
	case rep := <-done:
		if got := findService(t, rep, "valhalla").Outcome; got != OutcomeFailed {
			t.Errorf("valhalla outcome = %v, want failed", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("update still running after the context was cancelled")
	}
	e.assertNothingLeftBehind(t, vermont, maine, newHampshire)
}

func TestConcurrentDownload_RegionLoadedByBothServicesDownloadsOnce(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.load("valhalla", vermont, t0, t1)
	e.makeChanged(vermont)
	e.liveTilesFor(t)

	if _, err := Update(context.Background(), e.deps(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if n := len(e.upstream.downloads); n != 1 {
		t.Errorf("downloads = %v, want vermont exactly once", e.upstream.downloads)
	}
}

func TestConcurrentDownload_StampsOnlyAfterOwningServiceRefreshes(t *testing.T) {
	// Valhalla's refresh succeeds; Overpass's conversion fails. Only Valhalla's
	// stamp may advance.
	e := newStagedOverpassEnv(t, vermont)
	e.load("valhalla", vermont, t0, t1)
	e.makeChanged(vermont)
	e.liveTilesFor(t)
	e.convErr = errors.New("bad pbf")

	rep, err := Update(context.Background(), e.deps(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := findService(t, rep, "overpass").Outcome; got != OutcomeFailed {
		t.Fatalf("overpass outcome = %v, want failed", got)
	}
	if got, _ := readServiceStamp(e.root.PBFDir, ServiceValhalla, vermont); !got.Equal(t1) {
		t.Errorf("valhalla stamp = %v, want %v", got, t1)
	}
	if got, _ := readServiceStamp(e.root.PBFDir, ServiceOverpass, vermont); !got.Equal(t0) {
		t.Errorf("overpass stamp = %v, want it left at %v", got, t0)
	}
}

// assertNothingLeftBehind checks that a failed or cancelled download left each
// region's PBF as it was, with no fetched marker and no advanced stamp.
func (e *valhallaEnv) assertNothingLeftBehind(t *testing.T, regions ...string) {
	t.Helper()
	for _, r := range regions {
		b, err := os.ReadFile(pbfPathFor(e.updaterEnv, r))
		if err != nil || string(b) != "pbf" {
			t.Errorf("%s: PBF = %q, %v; want the original left untouched", r, b, err)
		}
		if pbfAlreadyFetched(e.root.PBFDir, r, t1) {
			t.Errorf("%s: fetched marker left behind", r)
		}
		if got, _ := readServiceStamp(e.root.PBFDir, ServiceValhalla, r); !got.Equal(t0) {
			t.Errorf("%s: stamp advanced to %v", r, got)
		}
	}
}
