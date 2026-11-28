package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

// pipelineEnv is a staged Overpass install with vermont, maine and
// new-hampshire all changed upstream, and recorders for the per-region
// conversion.
type pipelineEnv struct {
	*stagedOverpassEnv
	mu        sync.Mutex
	converted []string // regions whose conversion finished
	atMerge   []string // regions converted by the time the merge began
	merged    bool
}

func newPipelineEnv(t *testing.T) *pipelineEnv {
	t.Helper()
	e := &pipelineEnv{stagedOverpassEnv: newStagedOverpassEnv(t, vermont, maine, newHampshire)}
	for _, r := range []string{vermont, maine, newHampshire} {
		e.makeChanged(r)
	}
	return e
}

func (e *pipelineEnv) update(t *testing.T, convertRegion func(ctx context.Context, region string) error) UpdateReport {
	t.Helper()
	d := e.stagedOverpassEnv.deps()
	d.ConvertOverpassRegion = func(ctx context.Context, region string) error {
		if convertRegion != nil {
			if err := convertRegion(ctx, region); err != nil {
				return err
			}
		}
		e.mu.Lock()
		e.converted = append(e.converted, region)
		e.mu.Unlock()
		return nil
	}
	merge := d.ConvertOverpass
	d.ConvertOverpass = func(ctx context.Context, regions []string) error {
		e.mu.Lock()
		e.merged = true
		e.atMerge = append([]string(nil), e.converted...)
		e.mu.Unlock()
		return merge(ctx, regions)
	}
	rep, err := Update(context.Background(), d, UpdateOptions{Only: "overpass"})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestPipeline_RegionConvertsBeforeTheLastDownloadFinishes(t *testing.T) {
	e := newPipelineEnv(t)
	converting := make(chan struct{})
	var once sync.Once
	// New Hampshire's download cannot finish until some other region has begun
	// converting, so a convert-after-all-downloads run times out here.
	e.upstream.onDownload = func(ctx context.Context, region, _ string) error {
		if region != newHampshire {
			return nil
		}
		select {
		case <-converting:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return errors.New("no region began converting while the last download was running")
		}
	}

	rep := e.update(t, func(_ context.Context, region string) error {
		if region != newHampshire {
			once.Do(func() { close(converting) })
		}
		return nil
	})

	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Errorf("overpass outcome = %v (%s), want updated", o.Outcome, o.Detail)
	}
}

func TestPipeline_MergeStartsOnlyAfterEveryRegionIsConverted(t *testing.T) {
	e := newPipelineEnv(t)

	rep := e.update(t, func(_ context.Context, region string) error {
		if region == newHampshire {
			time.Sleep(50 * time.Millisecond) // the slowest region
		}
		return nil
	})

	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Fatalf("overpass outcome = %v (%s), want updated", o.Outcome, o.Detail)
	}
	if len(e.atMerge) != 3 {
		t.Errorf("regions converted when the merge began = %v, want all 3", e.atMerge)
	}
}

func TestPipeline_ConvertFailureStopsDownloadsAndNamesStageAndRegion(t *testing.T) {
	e := newPipelineEnv(t)
	e.upstream.onDownload = func(ctx context.Context, region, _ string) error {
		if region != newHampshire {
			return nil
		}
		select { // would hang past the deadline unless cancelled
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return errors.New("download was not cancelled")
		}
	}

	rep := e.update(t, func(_ context.Context, region string) error {
		if region == vermont {
			return errors.New("bad pbf")
		}
		return nil
	})

	o := findService(t, rep, "overpass")
	if o.Outcome != OutcomeFailed {
		t.Fatalf("overpass outcome = %v, want failed", o.Outcome)
	}
	for _, want := range []string{"stage convert", vermont, "bad pbf"} {
		if !strings.Contains(o.Detail, want) {
			t.Errorf("detail %q should contain %q", o.Detail, want)
		}
	}
	e.assertNotMergedOrSwapped(t)
}

func TestPipeline_DownloadFailureStopsConversionAndNamesStageAndRegion(t *testing.T) {
	e := newPipelineEnv(t)
	e.upstream.downloadErr[newHampshire] = errors.New("connection reset")

	rep := e.update(t, func(ctx context.Context, region string) error {
		if region == newHampshire {
			return nil
		}
		select { // a conversion running at the time sees the pipeline stop
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return errors.New("conversion was not cancelled")
		}
	})

	o := findService(t, rep, "overpass")
	if o.Outcome != OutcomeFailed {
		t.Fatalf("overpass outcome = %v, want failed", o.Outcome)
	}
	for _, want := range []string{"stage download", newHampshire, "connection reset"} {
		if !strings.Contains(o.Detail, want) {
			t.Errorf("detail %q should contain %q", o.Detail, want)
		}
	}
	e.assertNotMergedOrSwapped(t)
}

// assertNotMergedOrSwapped checks that a failed pipeline never merged and left
// the live merged archive as it was.
func (e *pipelineEnv) assertNotMergedOrSwapped(t *testing.T) {
	t.Helper()
	if e.merged {
		t.Error("merge ran after a failure")
	}
	if b, err := os.ReadFile(e.stagedOverpassEnv.merged); err != nil || string(b) != "old merged" {
		t.Errorf("merged archive = %q, %v; want it untouched", b, err)
	}
}

func TestPipeline_RegionLoadedByBothServicesIsFetchedAndConvertedOnce(t *testing.T) {
	e := &pipelineEnv{stagedOverpassEnv: newStagedOverpassEnv(t, vermont)}
	e.load("valhalla", vermont, t0, t1)
	e.makeChanged(vermont)
	e.liveTilesFor(t)
	d := e.stagedOverpassEnv.deps()
	d.ConvertOverpassRegion = func(_ context.Context, region string) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.converted = append(e.converted, region)
		return nil
	}

	if _, err := Update(context.Background(), d, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if n := len(e.upstream.downloads); n != 1 {
		t.Errorf("downloads = %v, want vermont exactly once", e.upstream.downloads)
	}
	if len(e.converted) != 1 || e.converted[0] != vermont {
		t.Errorf("converted = %v, want vermont exactly once", e.converted)
	}
}

func TestConvertRegion_RealConversionFillsTheCacheOnceForTheMerge(t *testing.T) {
	const region = "europe/r0"
	dataDir, pbfDir := setupConcurrentFixture(t, []string{region})
	var conversions atomic.Int32
	stubConvertRegion(t, 1, func(pbf, cache, meta string, p osmconv.ConvertProgress) error {
		conversions.Add(1)
		return convertSingleRegionToCache(pbf, cache, meta, p)
	})
	u := &updateRun{deps: UpdateDeps{Root: DataRoot{OverpassDir: dataDir, PBFDir: pbfDir}}}

	for range 2 {
		if err := u.convertRegion(context.Background(), region); err != nil {
			t.Fatal(err)
		}
	}

	pbf, cache, meta := regionCachePaths(dataDir, pbfDir, region)
	if !isCacheValid(pbf, cache, meta) {
		t.Error("cache is not valid after converting the region")
	}
	if n := conversions.Load(); n != 1 {
		t.Errorf("conversions = %d, want 1 (a valid cache is not converted again)", n)
	}
}
