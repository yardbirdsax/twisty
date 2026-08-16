# Parallel PBF Downloads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Download multiple PBF files concurrently in `twisty overpass start` instead of sequentially.

**Architecture:** Introduce a per-file `FileDownloadProgress` interface that a single goroutine per download calls independently. The existing `DownloadProgress` implementations are rebuilt around a thread-safe aggregator that merges per-file byte counts for the summary line. The parallel loop in `runOverpassStart` fans out goroutines using `sync.WaitGroup` and collects errors via a slice.

**Tech Stack:** Go standard library (`sync`, `sync/atomic`)

## Global Constraints

- No new external dependencies — stdlib only.
- All existing tests must continue to pass.
- Use TDD: write the failing test before each piece of implementation.

---

## File Map

| File | Change |
|---|---|
| `download_progress.go` | Add `FileDownloadProgress` interface; update `DownloadProgress.StartFile` to return `FileDownloadProgress`; update `NoopDownloadProgress` |
| `main.go` | Refactor `termDownloadBar` and `logDownloadProgress` to the new interface with thread-safe per-file inner types |
| `overpass.go` | Change `downloadPBF` signature; add `downloadPBFsParallel`; replace sequential loop with parallel fan-out |
| `download_progress_test.go` | Update tests to match new interface; add concurrent-safety test |
| `overpass_test.go` | Update `recordingDownloadProgress`; update `downloadPBF` call sites; add parallelism test |
| `overpass_e2e_validation_test.go` | Update direct `downloadPBF` call to new signature |

---

### Task 1: Introduce `FileDownloadProgress` and update `DownloadProgress`

**Files:**
- Modify: `download_progress.go`
- Modify: `download_progress_test.go`

**Interfaces:**
- Produces:
  - `FileDownloadProgress` interface: `BytesDownloaded(n int64)`, `FileComplete()`
  - Updated `DownloadProgress` interface: `StartFile(name string, fileIndex int, totalFiles int, totalBytes int64) FileDownloadProgress`, `Done()`
  - `NoopFileDownloadProgress` struct
  - `NoopDownloadProgress.StartFile` returns `NoopFileDownloadProgress{}`

- [ ] **Step 1: Write the failing test**

Add to `download_progress_test.go` (inside `package main`):

```go
func TestNoopFileDownloadProgress_ImplementsInterface(t *testing.T) {
	var p DownloadProgress = NoopDownloadProgress{}
	fp := p.StartFile("test.osm.pbf", 0, 1, 100)
	fp.BytesDownloaded(50)
	fp.FileComplete()
	p.Done()
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test -run TestNoopFileDownloadProgress_ImplementsInterface ./...
```

Expected: compile error — `DownloadProgress` has no method `StartFile` returning `FileDownloadProgress`.

- [ ] **Step 3: Update `download_progress.go`**

Replace the entire file with:

```go
package main

// FileDownloadProgress reports progress for a single in-flight PBF download.
// Each call to DownloadProgress.StartFile returns one of these.
type FileDownloadProgress interface {
	BytesDownloaded(n int64)
	FileComplete()
}

// DownloadProgress manages progress across all PBF file downloads.
type DownloadProgress interface {
	// StartFile begins tracking a new file download and returns a
	// FileDownloadProgress the caller uses to report bytes and completion.
	// totalBytes is Content-Length (-1 if unknown). fileIndex is 0-based.
	StartFile(name string, fileIndex int, totalFiles int, totalBytes int64) FileDownloadProgress

	// Done signals that all downloads are complete.
	Done()
}

// NoopFileDownloadProgress is a FileDownloadProgress that does nothing.
type NoopFileDownloadProgress struct{}

func (NoopFileDownloadProgress) BytesDownloaded(int64) {}
func (NoopFileDownloadProgress) FileComplete()         {}

var _ FileDownloadProgress = NoopFileDownloadProgress{}

// NoopDownloadProgress is a DownloadProgress that does nothing.
type NoopDownloadProgress struct{}

func (NoopDownloadProgress) StartFile(string, int, int, int64) FileDownloadProgress {
	return NoopFileDownloadProgress{}
}
func (NoopDownloadProgress) Done() {}

var _ DownloadProgress = NoopDownloadProgress{}
```

- [ ] **Step 4: Run test to verify it passes**

```
go test -run TestNoopFileDownloadProgress_ImplementsInterface ./...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add download_progress.go download_progress_test.go
git commit -m "refactor(download): split DownloadProgress into per-file FileDownloadProgress"
```

---

### Task 2: Update `termDownloadBar` and `logDownloadProgress` to the new interface

**Files:**
- Modify: `main.go`
- Modify: `download_progress_test.go`

**Interfaces:**
- Consumes: `FileDownloadProgress` from Task 1
- Produces:
  - `termFileBar` struct implementing `FileDownloadProgress` (inner type of `termDownloadBar`)
  - `logFileBar` struct implementing `FileDownloadProgress` (inner type of `logDownloadProgress`)
  - Both `termDownloadBar` and `logDownloadProgress` implement updated `DownloadProgress`
  - All methods are safe for concurrent calls from multiple goroutines

- [ ] **Step 1: Write failing tests**

Replace the full contents of `download_progress_test.go` with:

```go
package main

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestNoopFileDownloadProgress_ImplementsInterface(t *testing.T) {
	var p DownloadProgress = NoopDownloadProgress{}
	fp := p.StartFile("test.osm.pbf", 0, 1, 100)
	fp.BytesDownloaded(50)
	fp.FileComplete()
	p.Done()
}

func TestTermDownloadBar_RenderWithContentLength(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	fp := bar.StartFile("us-northeast", 1, 3, 200*1024*1024)
	fp.BytesDownloaded(100 * 1024 * 1024)

	out := buf.String()
	if !strings.Contains(out, "MB") {
		t.Errorf("expected MB in output, got: %q", out)
	}
	if !strings.HasPrefix(out, "\r") {
		t.Errorf("expected output to start with carriage return, got: %q", out)
	}
}

func TestTermDownloadBar_RenderNoContentLength(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	fp := bar.StartFile("us-west", 0, 2, -1)
	fp.BytesDownloaded(50 * 1024 * 1024)

	out := buf.String()
	if !strings.Contains(out, "50.0 MB") {
		t.Errorf("expected downloaded bytes in output, got: %q", out)
	}
}

func TestTermDownloadBar_Done(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	fp := bar.StartFile("file1", 0, 3, 100*1024*1024)
	fp.BytesDownloaded(50 * 1024 * 1024)
	buf.Reset()

	bar.Done()

	out := buf.String()
	if !strings.Contains(out, "Downloads complete") {
		t.Errorf("expected 'Downloads complete' in output, got: %q", out)
	}
	if !strings.Contains(out, "3 files") {
		t.Errorf("expected '3 files' in output, got: %q", out)
	}
	if !strings.Contains(out, "50.0") {
		t.Errorf("expected 50.0 MB in Done output, got: %q", out)
	}
}

func TestTermDownloadBar_ConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fp := bar.StartFile("file", idx, 4, 10*1024*1024)
			for j := 0; j < 10; j++ {
				fp.BytesDownloaded(1024 * 1024)
			}
			fp.FileComplete()
		}(i)
	}
	wg.Wait()
	bar.Done()
}

func TestLogDownloadProgress_PrintsEvery10MB(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("us-midwest", 0, 1, 100*1024*1024)

	fp.BytesDownloaded(5 * 1024 * 1024)
	if buf.Len() == 0 {
		// StartFile prints a "Downloading" line; reset before testing threshold
	}
	buf.Reset()

	fp.BytesDownloaded(6 * 1024 * 1024)
	out := buf.String()
	if !strings.Contains(out, "downloaded") {
		t.Errorf("expected progress line after 10 MB, got: %q", out)
	}
	if !strings.Contains(out, "%") {
		t.Errorf("expected percentage in log line, got: %q", out)
	}
}

func TestLogDownloadProgress_NoContentLength(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("us-west", 0, 1, -1)
	buf.Reset()

	fp.BytesDownloaded(11 * 1024 * 1024)
	out := buf.String()
	if !strings.Contains(out, "downloaded") {
		t.Errorf("expected progress line after 10 MB, got: %q", out)
	}
	if strings.Contains(out, "%") {
		t.Errorf("expected no percentage when Content-Length unknown, got: %q", out)
	}
}

func TestLogDownloadProgress_FileComplete(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("us-northeast", 0, 2, 100*1024*1024)
	fp.BytesDownloaded(100 * 1024 * 1024)
	buf.Reset()
	fp.FileComplete()

	out := buf.String()
	if !strings.Contains(out, "done") {
		t.Errorf("expected 'done' in FileComplete output, got: %q", out)
	}
	if !strings.Contains(out, "MB") {
		t.Errorf("expected MB in FileComplete output, got: %q", out)
	}
}

func TestLogDownloadProgress_Done(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("file1", 0, 2, 50*1024*1024)
	fp.BytesDownloaded(30 * 1024 * 1024)
	buf.Reset()

	p.Done()

	out := buf.String()
	if !strings.Contains(out, "Downloads complete") {
		t.Errorf("expected 'Downloads complete' in Done output, got: %q", out)
	}
	if !strings.Contains(out, "2 files") {
		t.Errorf("expected '2 files' in Done output, got: %q", out)
	}
	if !strings.Contains(out, "30.0") {
		t.Errorf("expected 30.0 MB in Done output, got: %q", out)
	}
}

func TestNewDownloadProgress_NonTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "testfile")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	defer f.Close()

	p := newDownloadProgress(f)
	if _, ok := p.(*logDownloadProgress); !ok {
		t.Errorf("expected *logDownloadProgress for non-terminal, got %T", p)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test -run "TestTermDownloadBar|TestLogDownloadProgress|TestNewDownloadProgress" ./...
```

Expected: compile errors — `termDownloadBar` and `logDownloadProgress` no longer satisfy `DownloadProgress`.

- [ ] **Step 3: Rewrite `termDownloadBar` in `main.go`**

Find and replace the `termDownloadBar` struct and all its methods (the block from `// termDownloadBar is an in-place terminal progress bar` through `var _ DownloadProgress = &termDownloadBar{}`):

```go
// termFileBar is the per-file handle returned by termDownloadBar.StartFile.
type termFileBar struct {
	parent *termDownloadBar
}

func (f *termFileBar) BytesDownloaded(n int64) {
	f.parent.mu.Lock()
	defer f.parent.mu.Unlock()
	f.parent.totalDownloaded += n
	if time.Since(f.parent.lastRender) >= 100*time.Millisecond {
		f.parent.render()
	}
}

func (f *termFileBar) FileComplete() {
	f.parent.mu.Lock()
	defer f.parent.mu.Unlock()
	f.parent.completedFiles++
	f.parent.render()
}

var _ FileDownloadProgress = &termFileBar{}

// termDownloadBar is an in-place terminal progress bar for PBF file downloads.
// It implements DownloadProgress.
type termDownloadBar struct {
	mu              sync.Mutex
	w               io.Writer
	totalFiles      int
	completedFiles  int
	totalDownloaded int64
	startTime       time.Time
	lastRender      time.Time
}

func (b *termDownloadBar) StartFile(name string, fileIndex int, totalFiles int, totalBytes int64) FileDownloadProgress {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.totalFiles = totalFiles
	if b.startTime.IsZero() {
		b.startTime = time.Now()
	}
	return &termFileBar{parent: b}
}

func (b *termDownloadBar) Done() {
	b.mu.Lock()
	defer b.mu.Unlock()
	elapsed := time.Since(b.startTime)
	fmt.Fprintf(b.w, "\rDownloads complete: %d files, %.1f MB, %s\n",
		b.totalFiles, float64(b.totalDownloaded)/(1024*1024), formatElapsed(elapsed))
}

func (b *termDownloadBar) render() {
	downloadedMB := float64(b.totalDownloaded) / (1024 * 1024)
	elapsed := time.Since(b.startTime)
	fmt.Fprintf(b.w, "\rDownloading: %.1f MB | %d/%d files | %s",
		downloadedMB, b.completedFiles, b.totalFiles, formatElapsed(elapsed))
	b.lastRender = time.Now()
}

var _ DownloadProgress = &termDownloadBar{}
```

- [ ] **Step 4: Rewrite `logDownloadProgress` in `main.go`**

Find and replace the `logDownloadProgress` struct and all its methods (from `// logDownloadProgress is a fallback` through `var _ DownloadProgress = &logDownloadProgress{}`):

```go
// logFileBar is the per-file handle returned by logDownloadProgress.StartFile.
type logFileBar struct {
	parent     *logDownloadProgress
	name       string
	totalBytes int64
	downloaded int64
	lastReport int64
}

func (f *logFileBar) BytesDownloaded(n int64) {
	f.parent.mu.Lock()
	defer f.parent.mu.Unlock()
	f.downloaded += n
	f.parent.totalDownloaded += n
	const tenMB = 10 * 1024 * 1024
	if f.downloaded-f.lastReport >= tenMB {
		f.lastReport = f.downloaded
		downloadedMB := float64(f.downloaded) / (1024 * 1024)
		if f.totalBytes > 0 {
			totalMB := float64(f.totalBytes) / (1024 * 1024)
			pct := int(f.downloaded * 100 / f.totalBytes)
			fmt.Fprintf(f.parent.w, "  %s: downloaded %.1f MB / %.1f MB (%d%%)\n", f.name, downloadedMB, totalMB, pct)
		} else {
			fmt.Fprintf(f.parent.w, "  %s: downloaded %.1f MB\n", f.name, downloadedMB)
		}
	}
}

func (f *logFileBar) FileComplete() {
	f.parent.mu.Lock()
	defer f.parent.mu.Unlock()
	downloadedMB := float64(f.downloaded) / (1024 * 1024)
	fmt.Fprintf(f.parent.w, "  %s: done (%.1f MB)\n", f.name, downloadedMB)
}

var _ FileDownloadProgress = &logFileBar{}

// logDownloadProgress is a fallback DownloadProgress for non-terminal stderr that
// prints a line every 10 MB per file.
type logDownloadProgress struct {
	mu              sync.Mutex
	w               io.Writer
	totalDownloaded int64
	totalFiles      int
	startTime       time.Time
}

func (p *logDownloadProgress) StartFile(name string, _ int, totalFiles int, totalBytes int64) FileDownloadProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalFiles = totalFiles
	if p.startTime.IsZero() {
		p.startTime = time.Now()
	}
	fmt.Fprintf(p.w, "Downloading %s\n", name)
	return &logFileBar{parent: p, name: name, totalBytes: totalBytes}
}

func (p *logDownloadProgress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	elapsed := time.Since(p.startTime)
	fmt.Fprintf(p.w, "Downloads complete: %d files, %.1f MB, %s\n",
		p.totalFiles, float64(p.totalDownloaded)/(1024*1024), formatElapsed(elapsed))
}

var _ DownloadProgress = &logDownloadProgress{}
```

- [ ] **Step 5: Run tests to verify they pass**

```
go test -run "TestTermDownloadBar|TestLogDownloadProgress|TestNewDownloadProgress" ./...
```

Expected: PASS

- [ ] **Step 6: Run full build to catch remaining compile errors**

```
go build ./...
```

Expected: compile errors in `overpass.go` and test files (addressed in Tasks 3–5). No other errors.

- [ ] **Step 7: Commit**

```bash
git add main.go download_progress_test.go
git commit -m "refactor(download): update termDownloadBar and logDownloadProgress for concurrent per-file progress"
```

---

### Task 3: Update `downloadPBF` to accept `FileDownloadProgress`

**Files:**
- Modify: `overpass.go`
- Modify: `overpass_test.go`

**Interfaces:**
- Consumes: `FileDownloadProgress` from Task 1
- Produces: `downloadPBF(url, destPath string, fp FileDownloadProgress) error`

Callers now call `progress.StartFile(...)` themselves and pass the returned `FileDownloadProgress` to `downloadPBF`. This makes `downloadPBF` a pure download function with no knowledge of file indexes or totals.

- [ ] **Step 1: Update `recordingDownloadProgress` and test call sites in `overpass_test.go`**

Replace the `recordingDownloadProgress` struct and its methods, and update the three `TestDownloadPBF_*` functions:

```go
// recordingDownloadProgress records calls to each DownloadProgress method in order.
// It implements both DownloadProgress and FileDownloadProgress so it can be used
// as its own per-file handle.
type recordingDownloadProgress struct {
	calls []string
}

func (r *recordingDownloadProgress) StartFile(name string, fileIndex int, totalFiles int, totalBytes int64) FileDownloadProgress {
	r.calls = append(r.calls, "StartFile")
	return r
}
func (r *recordingDownloadProgress) BytesDownloaded(n int64) {
	r.calls = append(r.calls, "BytesDownloaded")
}
func (r *recordingDownloadProgress) FileComplete() {
	r.calls = append(r.calls, "FileComplete")
}
func (r *recordingDownloadProgress) Done() {
	r.calls = append(r.calls, "Done")
}

func TestDownloadPBF_CallsProgressInOrder(t *testing.T) {
	const body = "fake pbf content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "test.osm.pbf")

	rec := &recordingDownloadProgress{}
	fp := rec.StartFile(destPath, 0, 1, int64(len(body)))
	if err := downloadPBF(srv.URL+"/test.osm.pbf", destPath, fp); err != nil {
		t.Fatalf("downloadPBF returned error: %v", err)
	}

	if _, err := os.Stat(destPath); err != nil {
		t.Fatalf("dest file not created: %v", err)
	}

	// calls[0] is "StartFile" from our explicit call above.
	// Remaining: BytesDownloaded..., FileComplete.
	if len(rec.calls) < 3 {
		t.Fatalf("expected at least 3 calls, got %d: %v", len(rec.calls), rec.calls)
	}
	last := rec.calls[len(rec.calls)-1]
	if last != "FileComplete" {
		t.Errorf("last call should be FileComplete, got %q", last)
	}
	for i := 1; i < len(rec.calls)-1; i++ {
		if rec.calls[i] != "BytesDownloaded" {
			t.Errorf("call[%d] should be BytesDownloaded, got %q", i, rec.calls[i])
		}
	}
}

func TestDownloadPBF_ReturnsError_WhenServerReturnsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "test.osm.pbf")
	fp := NoopDownloadProgress{}.StartFile(destPath, 0, 1, -1)

	err := downloadPBF(srv.URL+"/test.osm.pbf", destPath, fp)
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestDownloadPBF_ReturnsError_WhenURLUnreachable(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "test.osm.pbf")
	fp := NoopDownloadProgress{}.StartFile(destPath, 0, 1, -1)

	err := downloadPBF("http://127.0.0.1:1/test.osm.pbf", destPath, fp)
	if err == nil {
		t.Fatal("expected error for unreachable URL, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test -run "TestDownloadPBF" ./...
```

Expected: compile error — `downloadPBF` still takes the old signature.

- [ ] **Step 3: Update `downloadPBF` in `overpass.go`**

Change the function signature and body. The old signature was:
```go
func downloadPBF(url, destPath string, progress DownloadProgress, fileIndex, totalFiles int) error {
```

Replace with:

```go
func downloadPBF(url, destPath string, fp FileDownloadProgress) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".download-*.osm.pbf")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tmp.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing temp file: %w", writeErr)
			}
			fp.BytesDownloaded(int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("reading response: %w", readErr)
		}
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}
	fp.FileComplete()
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test -run "TestDownloadPBF" ./...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add overpass.go overpass_test.go
git commit -m "refactor(download): simplify downloadPBF to accept FileDownloadProgress directly"
```

---

### Task 4: Add `downloadPBFsParallel` and wire it into `runOverpassStart`

**Files:**
- Modify: `overpass.go`
- Modify: `overpass_test.go`

**Interfaces:**
- Consumes: `downloadPBF(url, destPath string, fp FileDownloadProgress) error` from Task 3
- Consumes: `DownloadProgress.StartFile` from Task 1
- Produces: `downloadPBFsParallel(urls, destPaths []string, progress DownloadProgress) error`

- [ ] **Step 1: Write a failing test**

Add to `overpass_test.go`:

```go
func TestDownloadPBFsParallel_DownloadsInParallel(t *testing.T) {
	const delay = 50 * time.Millisecond

	var inflight atomic.Int32
	var maxInflight atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			old := maxInflight.Load()
			if cur <= old || maxInflight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake"))
	}))
	defer srv.Close()

	urls := []string{
		srv.URL + "/a.osm.pbf",
		srv.URL + "/b.osm.pbf",
	}

	dir := t.TempDir()
	destPaths := []string{
		filepath.Join(dir, "a.osm.pbf"),
		filepath.Join(dir, "b.osm.pbf"),
	}

	start := time.Now()
	err := downloadPBFsParallel(urls, destPaths, NoopDownloadProgress{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("downloadPBFsParallel returned error: %v", err)
	}
	if maxInflight.Load() < 2 {
		t.Errorf("expected at least 2 concurrent downloads, max inflight was %d", maxInflight.Load())
	}
	if elapsed > 2*delay {
		t.Errorf("downloads took %v, expected < %v (should be parallel)", elapsed, 2*delay)
	}
}

func TestDownloadPBFsParallel_ReturnsError_OnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	err := downloadPBFsParallel(
		[]string{srv.URL + "/a.osm.pbf"},
		[]string{filepath.Join(dir, "a.osm.pbf")},
		NoopDownloadProgress{},
	)
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test -run "TestDownloadPBFsParallel" ./...
```

Expected: compile error — `downloadPBFsParallel` is undefined.

- [ ] **Step 3: Add `downloadPBFsParallel` to `overpass.go`**

Add this function directly above `downloadPBF`:

```go
// downloadPBFsParallel downloads all urls[i] to destPaths[i] concurrently.
// All downloads start simultaneously; the first error is returned after all
// goroutines finish.
func downloadPBFsParallel(urls, destPaths []string, progress DownloadProgress) error {
	errs := make([]error, len(urls))
	var wg sync.WaitGroup
	for i := range urls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fp := progress.StartFile(destPaths[idx], idx, len(urls), -1)
			errs[idx] = downloadPBF(urls[idx], destPaths[idx], fp)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
```

Make sure `sync` is imported in `overpass.go`. Add it to the import block if missing.

- [ ] **Step 4: Run tests to verify they pass**

```
go test -run "TestDownloadPBFsParallel" ./...
```

Expected: PASS

- [ ] **Step 5: Replace the sequential loop in `runOverpassStart`**

In `overpass.go`, replace lines 207–222 (the sequential download loop):

```go
// Download any missing PBF files.
downloadProgress := newDownloadProgress(os.Stderr)
for i, region := range allRegions {
    filename := pbfFilename(region)
    destPath := filepath.Join(pbfDir, filename)
    if _, err := os.Stat(destPath); os.IsNotExist(err) {
        url := geofabrikBaseURL + "/" + region + "-latest.osm.pbf"
        fmt.Fprintf(stderr, "Downloading %s -> %s\n", url, destPath)
        if err := downloadPBF(url, destPath, downloadProgress, i, len(allRegions)); err != nil {
            return fmt.Errorf("downloading %s: %w", url, err)
        }
    } else {
        fmt.Fprintf(stderr, "PBF already exists: %s\n", destPath)
    }
}
downloadProgress.Done()
```

with:

```go
// Collect URLs and dest paths for missing PBF files, then download in parallel.
downloadProgress := newDownloadProgress(os.Stderr)
var missingURLs, missingPaths []string
for _, region := range allRegions {
    filename := pbfFilename(region)
    destPath := filepath.Join(pbfDir, filename)
    if _, err := os.Stat(destPath); os.IsNotExist(err) {
        url := geofabrikBaseURL + "/" + region + "-latest.osm.pbf"
        fmt.Fprintf(stderr, "Downloading %s -> %s\n", url, destPath)
        missingURLs = append(missingURLs, url)
        missingPaths = append(missingPaths, destPath)
    } else {
        fmt.Fprintf(stderr, "PBF already exists: %s\n", destPath)
    }
}
if len(missingURLs) > 0 {
    if err := downloadPBFsParallel(missingURLs, missingPaths, downloadProgress); err != nil {
        return fmt.Errorf("downloading PBF: %w", err)
    }
}
downloadProgress.Done()
```

- [ ] **Step 6: Run the full test suite**

```
go test ./...
```

Expected: all pass except possibly `overpass_e2e_validation_test.go` (addressed in Task 5).

- [ ] **Step 7: Commit**

```bash
git add overpass.go overpass_test.go
git commit -m "feat(download): download PBF files in parallel"
```

---

### Task 5: Fix `overpass_e2e_validation_test.go`

**Files:**
- Modify: `overpass_e2e_validation_test.go`

**Interfaces:**
- Consumes: `downloadPBF(url, destPath string, fp FileDownloadProgress) error` from Task 3
- Consumes: `DownloadProgress.StartFile` from Task 1

- [ ] **Step 1: Find the call site**

Open `overpass_e2e_validation_test.go` and locate the call to `downloadPBF` (around line 49).

- [ ] **Step 2: Update the call**

The current code looks like:

```go
progress := newDownloadProgress(os.Stderr)
// ...
if err := downloadPBF(url, destPath, progress, i, len(e2eRegions5)); err != nil {
```

Replace with:

```go
progress := newDownloadProgress(os.Stderr)
// ...
fp := progress.StartFile(destPath, i, len(e2eRegions5), -1)
if err := downloadPBF(url, destPath, fp); err != nil {
```

- [ ] **Step 3: Build to confirm no compile errors**

```
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run full test suite**

```
go test ./...
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add overpass_e2e_validation_test.go
git commit -m "fix(test): update e2e validation test for new downloadPBF signature"
```
