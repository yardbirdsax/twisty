package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

func concurrentRegions(n int) []string {
	regions := make([]string, n)
	for i := range regions {
		regions[i] = fmt.Sprintf("europe/r%d", i)
	}
	return regions
}

func setupConcurrentFixture(t *testing.T, regions []string) (dataDir, pbfDir string) {
	t.Helper()
	dataDir = t.TempDir()
	pbfDir = filepath.Join(dataDir, "pbf")
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, r := range regions {
		setupPBFRegion(t, pbfDir, r)
	}
	return dataDir, pbfDir
}

func stubConvertRegion(t *testing.T, workers int, fn func(pbf, cache, meta string, p osmconv.ConvertProgress) error) {
	t.Helper()
	oldFn, oldWorkers := convertRegionToCache, convertWorkers
	convertRegionToCache, convertWorkers = fn, workers
	t.Cleanup(func() { convertRegionToCache, convertWorkers = oldFn, oldWorkers })
}

func TestConvertRegionsIncremental_ConvertsStaleRegionsConcurrentlyBoundedByPool(t *testing.T) {
	const workers = 3
	regions := concurrentRegions(8)
	dataDir, pbfDir := setupConcurrentFixture(t, regions)

	var inFlight, maxInFlight atomic.Int32
	reached := make(chan struct{})
	var once sync.Once
	stubConvertRegion(t, workers, func(pbf, cache, meta string, p osmconv.ConvertProgress) error {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		if n == workers {
			once.Do(func() { close(reached) })
		}
		// Hold every worker until the pool is full: this only completes if the
		// regions really run at the same time.
		select {
		case <-reached:
		case <-time.After(5 * time.Second):
			return fmt.Errorf("pool never reached %d concurrent conversions", workers)
		}
		time.Sleep(10 * time.Millisecond)
		return convertSingleRegionToCache(pbf, cache, meta, p)
	})

	if err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, io.Discard); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	if got := maxInFlight.Load(); got != workers {
		t.Errorf("max concurrent conversions = %d, want %d", got, workers)
	}
}

func TestConvertRegionsIncremental_ConcurrentOutputMatchesSequential(t *testing.T) {
	regions := concurrentRegions(6)

	run := func(workers int) (caches map[string][]byte, merged []byte) {
		dataDir, pbfDir := setupConcurrentFixture(t, regions)
		old := convertWorkers
		convertWorkers = workers
		t.Cleanup(func() { convertWorkers = old })
		if err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, io.Discard); err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		caches = map[string][]byte{}
		for _, r := range regions {
			b, err := os.ReadFile(filepath.Join(dataDir, "cache", cacheFilename(r)))
			if err != nil {
				t.Fatal(err)
			}
			caches[r] = b
		}
		return caches, decompressBZ2(t, filepath.Join(dataDir, "merged.osm.bz2"))
	}

	seqCaches, seqMerged := run(1)
	conCaches, conMerged := run(4)
	for _, r := range regions {
		if !bytes.Equal(seqCaches[r], conCaches[r]) {
			t.Errorf("region %s cache differs between sequential and concurrent", r)
		}
	}
	if !bytes.Equal(seqMerged, conMerged) {
		t.Error("merged archive differs between sequential and concurrent")
	}
}

func TestConvertRegionsIncremental_FailureNamesRegionAndLeavesNoPartialCache(t *testing.T) {
	regions := concurrentRegions(6)
	dataDir, pbfDir := setupConcurrentFixture(t, regions)
	bad := regions[2]
	if err := os.WriteFile(filepath.Join(pbfDir, pbfFilename(bad)), []byte("not a pbf"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, io.Discard)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("error %q does not name region %s", err, bad)
	}

	cacheDir := filepath.Join(dataDir, "cache")
	if _, statErr := os.Stat(filepath.Join(cacheDir, cacheFilename(bad))); !os.IsNotExist(statErr) {
		t.Errorf("cache for failed region exists (stat err: %v)", statErr)
	}
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".cache-") {
			t.Errorf("partial temp file left behind: %s", e.Name())
		}
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "merged.osm.bz2")); !os.IsNotExist(statErr) {
		t.Error("merged archive must not be produced when a region fails")
	}
}

func TestConvertRegionsIncremental_SkipsValidCachedRegionsConcurrently(t *testing.T) {
	regions := concurrentRegions(5)
	dataDir, pbfDir := setupConcurrentFixture(t, regions)
	if err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, io.Discard); err != nil {
		t.Fatal(err)
	}

	// Make one region stale again by dropping its sidecar.
	stale := regions[3]
	os.Remove(filepath.Join(dataDir, "cache", cacheFilename(stale)+".meta.json"))

	var mu sync.Mutex
	var converted []string
	stubConvertRegion(t, 3, func(pbf, cache, meta string, p osmconv.ConvertProgress) error {
		mu.Lock()
		converted = append(converted, filepath.Base(pbf))
		mu.Unlock()
		return convertSingleRegionToCache(pbf, cache, meta, p)
	})
	if err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(converted) != 1 || converted[0] != pbfFilename(stale) {
		t.Errorf("converted = %v, want only %s", converted, pbfFilename(stale))
	}
}

func TestConvertRegionsIncremental_GivesEachConvertingRegionItsOwnProgress(t *testing.T) {
	regions := concurrentRegions(3)
	dataDir, pbfDir := setupConcurrentFixture(t, regions)

	var buf bytes.Buffer
	bar := &termConvertBar{w: &buf}
	if err := convertRegionsIncremental(regions, dataDir, pbfDir, bar, io.Discard); err != nil {
		t.Fatal(err)
	}
	bar.mu.Lock()
	defer bar.mu.Unlock()
	if len(bar.regions) != 0 {
		t.Errorf("%d regions still shown as active after the stage", len(bar.regions))
	}
}

func TestTermConvertBar_ShowsSeveralRegionsAtOnce(t *testing.T) {
	var buf bytes.Buffer
	bar := &termConvertBar{w: &buf}
	a := bar.StartRegion("europe/a", 1000)
	b := bar.StartRegion("europe/b", 2000)
	c := bar.StartRegion("europe/c", 1000)
	buf.Reset()

	a.BytesRead(0, 500)
	bar.mu.Lock()
	bar.lastRender = time.Time{}
	bar.mu.Unlock()
	b.BytesRead(0, 500)
	bar.mu.Lock()
	bar.lastRender = time.Time{}
	bar.mu.Unlock()
	c.BytesRead(0, 100)

	out := buf.String()
	last := out[strings.LastIndex(out, "\r"):]
	for _, name := range []string{"europe/a", "europe/b", "europe/c"} {
		if !strings.Contains(last, name) {
			t.Errorf("latest render %q does not show %s", last, name)
		}
	}
	if !strings.Contains(last, "3 regions") {
		t.Errorf("latest render %q should say how many regions are converting", last)
	}

	a.Done()
	bar.mu.Lock()
	n := len(bar.regions)
	bar.mu.Unlock()
	if n != 2 {
		t.Errorf("active regions after one finished = %d, want 2", n)
	}
}

func TestLogConvertProgress_StartRegionIsConcurrencySafe(t *testing.T) {
	var buf bytes.Buffer
	p := &logConvertProgress{w: &buf}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := p.StartRegion("r", 10)
			h.ObjectsWritten(1000)
			h.Done()
		}()
	}
	wg.Wait()
}

var _ regionConvertProgress = &termConvertBar{}
var _ regionConvertProgress = &logConvertProgress{}

func TestConvertRegionsIncremental_FailedRegionIsRemovedFromProgress(t *testing.T) {
	regions := concurrentRegions(3)
	dataDir, pbfDir := setupConcurrentFixture(t, regions)
	if err := os.WriteFile(filepath.Join(pbfDir, pbfFilename(regions[1])), []byte("not a pbf"), 0o644); err != nil {
		t.Fatal(err)
	}

	bar := &termConvertBar{w: io.Discard}
	if err := convertRegionsIncremental(regions, dataDir, pbfDir, bar, io.Discard); err == nil {
		t.Fatal("expected an error")
	}
	bar.mu.Lock()
	defer bar.mu.Unlock()
	if len(bar.regions) != 0 {
		t.Errorf("%d regions still shown after the stage failed", len(bar.regions))
	}
}
