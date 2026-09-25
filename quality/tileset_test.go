package quality

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestTileSet(t *testing.T, overpassURL string, fetchDelay time.Duration) *TileSet {
	t.Helper()
	return NewTileSet(TileSetConfig{
		CacheDir:    t.TempDir(),
		TileSize:    0.1,
		OverpassURL: overpassURL,
		FetchDelay:  fetchDelay,
		TileReady:   make(chan Tile, 64),
	})
}

func TestTileSet_EnsureReportsCachedTile(t *testing.T) {
	ts := newTestTileSet(t, "http://127.0.0.1:1", time.Millisecond)
	tile := Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	if err := ts.Cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := ts.Cache.Write(tile, []byte(`{"elements":[]}`)); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	status := ts.Ensure([]Tile{tile}, false)

	if len(status.Cached) != 1 || status.Cached[0] != tile {
		t.Fatalf("expected tile to be reported cached, got %+v", status)
	}
	if len(status.Pending) != 0 {
		t.Fatalf("expected no pending tiles, got %+v", status.Pending)
	}
}

func TestTileSet_EnsureReportsPendingAndTriggersFetch(t *testing.T) {
	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()

	ts := newTestTileSet(t, overpassSrv.URL, time.Millisecond)
	tile := Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}

	status := ts.Ensure([]Tile{tile}, false)

	if len(status.Pending) != 1 || status.Pending[0] != tile {
		t.Fatalf("expected tile to be reported pending, got %+v", status)
	}
	if len(status.Cached) != 0 {
		t.Fatalf("expected no cached tiles, got %+v", status.Cached)
	}

	// Wait for the async fetch Ensure kicked off to land the tile in cache
	// (not just for the mock server to respond) so its goroutine has fully
	// finished writing before t.TempDir() cleans up.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ts.Cache.Has(tile) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected Ensure to trigger an async fetch that lands the tile in cache")
}

func TestTileSet_EnsureReportsFailedTiles(t *testing.T) {
	ts := newTestTileSet(t, "http://127.0.0.1:1", time.Millisecond)
	tile := Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	ts.failedTiles.Store(tile, struct{}{})

	status := ts.Ensure([]Tile{tile}, false)

	if len(status.Failed) != 1 || status.Failed[0] != tile {
		t.Fatalf("expected tile to be reported failed, got %+v", status)
	}
}

func TestTileSet_FetchMissingClearsFailedOnSuccess(t *testing.T) {
	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()

	ts := newTestTileSet(t, overpassSrv.URL, time.Millisecond)
	if err := ts.Cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	tile := Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	ts.failedTiles.Store(tile, struct{}{})

	ts.FetchMissing([]Tile{tile}, false)

	if !ts.Cache.Has(tile) {
		t.Fatal("expected tile to be in cache after successful fetch")
	}
	if _, still := ts.failedTiles.Load(tile); still {
		t.Error("expected failedTiles entry to be cleared after successful fetch")
	}
}

func TestTileSet_FetchMissingDeduplicatesInFlightTiles(t *testing.T) {
	firstRequestStarted := make(chan struct{})
	firstRequestUnblock := make(chan struct{})
	fetchCount := 0

	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		close(firstRequestStarted)
		<-firstRequestUnblock
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()

	ts := newTestTileSet(t, overpassSrv.URL, time.Millisecond)
	tile := Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}

	go ts.FetchMissing([]Tile{tile}, false)

	select {
	case <-firstRequestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first goroutine never reached the Overpass mock")
	}

	// Second call with the same tile — should be a no-op due to in-flight tracking.
	ts.FetchMissing([]Tile{tile}, false)

	close(firstRequestUnblock)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ts.Cache.Has(tile) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if fetchCount != 1 {
		t.Errorf("expected exactly 1 Overpass fetch, got %d", fetchCount)
	}
}

func TestTileSet_FetchMissing_cancelStale(t *testing.T) {
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()
	defer releaseOnce()

	ts := newTestTileSet(t, overpassSrv.URL, time.Millisecond)
	tileA := Tile{South: 40.0, West: -75.0, North: 40.1, East: -74.9}
	tileB := Tile{South: 40.1, West: -75.0, North: 40.2, East: -74.9}
	tileC := Tile{South: 40.2, West: -75.0, North: 40.3, East: -74.9}

	var wg sync.WaitGroup
	wg.Add(2)

	// First call: claim tileA and tileB; both will block at the Overpass server.
	go func() {
		defer wg.Done()
		ts.FetchMissing([]Tile{tileA, tileB}, true)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, aOk := ts.inFlight.Load(tileA)
		_, bOk := ts.inFlight.Load(tileB)
		if aOk && bOk {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second call: tileA and tileC. tileB is stale — the stale sweep must cancel
	// and remove it.
	secondCallStarted := make(chan struct{})
	go func() {
		defer wg.Done()
		close(secondCallStarted)
		ts.FetchMissing([]Tile{tileA, tileC}, true)
	}()
	<-secondCallStarted

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, bOk := ts.inFlight.Load(tileB)
		_, cOk := ts.inFlight.Load(tileC)
		if !bOk && cOk {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, tileBInFlight := ts.inFlight.Load(tileB); tileBInFlight {
		t.Error("expected tileB to have been cancelled and removed from inFlight")
	}
	if _, tileCInFlight := ts.inFlight.Load(tileC); !tileCInFlight {
		t.Error("expected tileC to be in-flight after second call")
	}

	// Release the blocked Overpass handler so both goroutines can finish
	// writing (or abandoning) their cache files before t.TempDir() cleans up.
	releaseOnce()
	wg.Wait()
}

func TestTileSet_FetchMissing_overlapTileContinuesOnStaleCancel(t *testing.T) {
	tileA := Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	tileB := Tile{South: 41.0, West: -75.8, North: 41.1, East: -75.7}

	unblockA := make(chan struct{})

	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err == nil {
			if strings.Contains(r.FormValue("data"), "40.000000") {
				<-unblockA
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()

	ts := newTestTileSet(t, overpassSrv.URL, time.Millisecond)

	go ts.FetchMissing([]Tile{tileA, tileB}, false)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for both tiles to appear in inFlight")
		}
		_, aOk := ts.inFlight.Load(tileA)
		_, bOk := ts.inFlight.Load(tileB)
		if aOk && bOk {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second call: only tileA in viewport, cancelStale=true — should cancel tileB.
	ts.FetchMissing([]Tile{tileA}, true)

	close(unblockA)

	deadline = time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("tileA did not land in cache — its context was likely cancelled")
		}
		if ts.Cache.Has(tileA) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ts.Cache.Has(tileB) {
		t.Error("tileB should not be in cache — it was supposed to be cancelled")
	}
}

func TestTileSet_ClearFailedAndCache(t *testing.T) {
	ts := newTestTileSet(t, "http://127.0.0.1:1", time.Millisecond)
	if err := ts.Cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	tile := Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	if err := ts.Cache.Write(tile, []byte(`{"elements":[]}`)); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}
	ts.failedTiles.Store(tile, struct{}{})

	cleared := ts.ClearCacheAndFailed(tile)
	if !cleared {
		t.Fatal("expected ClearCacheAndFailed to report a cleared tile")
	}
	if ts.Cache.Has(tile) {
		t.Error("expected cache file to be removed")
	}
	if _, still := ts.failedTiles.Load(tile); still {
		t.Error("expected failedTiles entry to be cleared")
	}

	// Clearing again should report false — nothing left to clear.
	if cleared := ts.ClearCacheAndFailed(tile); cleared {
		t.Error("expected second clear to report false")
	}
}
