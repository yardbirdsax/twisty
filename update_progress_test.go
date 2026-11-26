package main

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// startCall is one DownloadProgress.StartFile call.
type startCall struct {
	name       string
	index      int
	total      int
	totalBytes int64
}

// recordingProgress is a DownloadProgress that records what an update tells it.
type recordingProgress struct {
	mu        sync.Mutex
	starts    []startCall
	completed int
	done      int
}

func (p *recordingProgress) StartFile(name string, index, total int, totalBytes int64) FileDownloadProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts = append(p.starts, startCall{name, index, total, totalBytes})
	return &recordingFile{p: p}
}

func (p *recordingProgress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done++
}

type recordingFile struct{ p *recordingProgress }

func (f *recordingFile) BytesDownloaded(int64) {}
func (f *recordingFile) FileComplete() {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	f.p.completed++
}

func TestUpdateProgress_ReportsPositionAmongChangedRegions(t *testing.T) {
	const newHampshire = "north-america/us/new-hampshire"
	e := newValhallaEnv(t, vermont, maine, newHampshire)
	e.makeChanged(vermont)
	e.makeChanged(maine)
	e.makeChanged(newHampshire)
	e.upstream.info[maine] = UpstreamInfo{LastModified: t1, ContentLength: 200}
	rec := &recordingProgress{}
	e.progress = rec

	e.update(t, UpdateOptions{})

	want := []startCall{
		{name: pbfFilename(vermont), index: 0, total: 3, totalBytes: 100},
		{name: pbfFilename(maine), index: 1, total: 3, totalBytes: 200},
		{name: pbfFilename(newHampshire), index: 2, total: 3, totalBytes: 100},
	}
	got := make([]startCall, len(rec.starts))
	for i, s := range rec.starts {
		s.name = filepath.Base(s.name)
		got[i] = s
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StartFile calls = %+v, want %+v", got, want)
	}
	if rec.completed != 3 {
		t.Errorf("completed files = %d, want 3", rec.completed)
	}
	if rec.done != 1 {
		t.Errorf("Done called %d times, want once", rec.done)
	}
}

func TestUpdateProgress_UnchangedRegionsAreNotCounted(t *testing.T) {
	e := newValhallaEnv(t, vermont, maine)
	e.makeChanged(vermont) // maine stays up to date
	rec := &recordingProgress{}
	e.progress = rec

	e.update(t, UpdateOptions{})

	if len(rec.starts) != 1 || rec.starts[0].total != 1 || rec.starts[0].index != 0 {
		t.Errorf("StartFile calls = %+v, want a single 0 of 1", rec.starts)
	}
}

func TestUpdateProgress_RegionSharedByBothServicesIsCountedOnce(t *testing.T) {
	e := newTwoServiceEnv(t) // valhalla: vermont, overpass: maine
	e.load("overpass", vermont, t0, t1)
	e.makeChanged(vermont)
	rec := &recordingProgress{}
	e.progress = rec

	e.update(t, UpdateOptions{})

	// Valhalla runs first and fetches vermont; Overpass then fetches only maine.
	if len(rec.starts) != 2 {
		t.Fatalf("StartFile calls = %+v, want 2 (shared region fetched once)", rec.starts)
	}
	for i, s := range rec.starts {
		if s.index != i || s.total != 2 {
			t.Errorf("call %d = %+v, want index %d of 2", i, s, i)
		}
	}
	if rec.done != 1 {
		t.Errorf("Done called %d times, want once", rec.done)
	}
}

func TestUpdateProgress_NothingFetchedStartsNoDownloads(t *testing.T) {
	e := newValhallaEnv(t, vermont) // up to date
	rec := &recordingProgress{}
	e.progress = rec

	e.update(t, UpdateOptions{})

	if len(rec.starts) != 0 || rec.done != 0 {
		t.Errorf("progress touched with nothing to download: %+v done=%d", rec.starts, rec.done)
	}
}
