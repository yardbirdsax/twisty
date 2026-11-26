package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/quality"
)

// captureLogs routes the default slog logger to a buffer for the duration of
// the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// serverWithCachedTile returns a buildServer whose cache holds one tile with a
// single hairpin road, so scoring has real segments to work through.
func serverWithCachedTile(t *testing.T) *buildServer {
	t.Helper()
	cacheDir := t.TempDir()
	tile := quality.Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := cache.Write(tile, tileData); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}
	return &buildServer{tileSize: 0.1, cacheDir: cacheDir}
}

// logLine returns the first line of out containing msg=<msg>.
func logLine(t *testing.T, out, msg string) string {
	t.Helper()
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, `msg="`+msg+`"`) {
			return l
		}
	}
	t.Fatalf("no %q line in:\n%s", msg, out)
	return ""
}

func TestScoreStats_shareRequestIDWithCompletionLine(t *testing.T) {
	logs := captureLogs(t)
	srv := serverWithCachedTile(t)
	req := httptest.NewRequest("POST", "/api/score", strings.NewReader(`{"points":[[40.010,-75.990],[40.012,-75.988]]}`))

	loggingMiddleware(http.HandlerFunc(srv.handleScore)).ServeHTTP(httptest.NewRecorder(), req)

	statsID := intField(t, logLine(t, logs.String(), "score stats"), "id")
	doneID := intField(t, logLine(t, logs.String(), "http done"), "id")
	if statsID != doneID {
		t.Errorf("score stats id=%d must match http done id=%d so the two lines can be joined", statsID, doneID)
	}
}

func TestInflightEntries_describeWhatEachRequestIsDoing(t *testing.T) {
	captureLogs(t)
	started := make(chan uint64)
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noteInflight(r.Context(), "points=812 points_hash=deadbeef")
		started <- requestIDFrom(r.Context())
		<-release
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/api/score", nil)
		req.RemoteAddr = "127.0.0.1:51126"
		loggingMiddleware(inner).ServeHTTP(httptest.NewRecorder(), req)
	}()
	id := <-started

	entries := inflightEntries()
	close(release)
	<-done

	if len(entries) != 1 {
		t.Fatalf("expected 1 in-flight entry, got %+v", entries)
	}
	e := entries[0]
	if e.ID != id || e.Path != "/api/score" || e.Remote != "127.0.0.1:51126" {
		t.Errorf("entry does not describe the request: %+v (want id=%d)", e, id)
	}
	if !strings.Contains(e.Note, "points_hash=deadbeef") {
		t.Errorf("expected the handler's note on the entry, got %q", e.Note)
	}
	if e.Start.IsZero() {
		t.Error("expected a start time so age can be computed")
	}
}

func TestCPUUnexplainedByRequests(t *testing.T) {
	score := inflightEntry{ID: 1, Path: "/api/score"}
	sse := inflightEntry{ID: 2, Path: "/api/segments/stream"}
	tests := []struct {
		name    string
		pct     float64
		entries []inflightEntry
		want    bool
	}{
		{"hot with nothing in flight", 300, nil, true},
		{"hot with a score request running", 300, []inflightEntry{score}, false},
		{"hot with only the long-lived SSE stream open", 300, []inflightEntry{sse}, true},
		{"hot with SSE and a score request", 300, []inflightEntry{sse, score}, false},
		{"quiet with nothing in flight", 5, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cpuUnexplainedByRequests(tt.pct, tt.entries); got != tt.want {
				t.Errorf("cpuUnexplainedByRequests(%v, %v) = %v, want %v", tt.pct, tt.entries, got, tt.want)
			}
		})
	}
}

func TestHandleViewportScore_logsWorkDone(t *testing.T) {
	logs := captureLogs(t)
	srv := serverWithCachedTile(t)
	req := httptest.NewRequest("POST", "/api/score/viewport", strings.NewReader(`{"west":-75.99,"south":40.05,"east":-75.98,"north":40.06}`))

	srv.handleViewportScore(httptest.NewRecorder(), req)

	line := logLine(t, logs.String(), "viewport score stats")
	if intField(t, line, "segments_checked") == 0 {
		t.Errorf("expected segments_checked > 0, got: %s", line)
	}
	for _, want := range []string{"tiles=", "cached_tiles=", "score_ms=", "client_gone=false"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %q in %s", want, line)
		}
	}
}

func TestHandleScore_logsWorkDone(t *testing.T) {
	logs := captureLogs(t)
	srv := serverWithCachedTile(t)
	req := httptest.NewRequest("POST", "/api/score", strings.NewReader(`{"points":[[40.010,-75.990],[40.012,-75.988]]}`))

	srv.handleScore(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, `msg="score stats"`) {
		t.Fatalf("expected a score stats line, got:\n%s", out)
	}
	for _, want := range []string{"points=2", "tiles=1", "cached_tiles=1", "points_hash=", "ensure_ms=", "score_ms=", "client_gone=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected score stats to contain %q, got:\n%s", want, out)
		}
	}
	checked := intField(t, out, "segments_checked")
	matched := intField(t, out, "segments_matched")
	if checked == 0 {
		t.Errorf("expected segments_checked > 0, got %d", checked)
	}
	if matched == 0 || matched > checked {
		t.Errorf("expected 0 < segments_matched <= segments_checked, got matched=%d checked=%d", matched, checked)
	}
}

// intField extracts key=<int> from a slog text line.
func intField(t *testing.T, line, key string) int {
	t.Helper()
	m := regexp.MustCompile(key + `=(\d+)`).FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("field %q not found in:\n%s", key, line)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("field %q: %v", key, err)
	}
	return n
}

func TestLoggingMiddleware_logsCompletionWithStatusAndDuration(t *testing.T) {
	logs := captureLogs(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("nope"))
	})

	loggingMiddleware(inner).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/thing", nil))

	out := logs.String()
	for _, want := range []string{`msg="http done"`, "path=/api/thing", "status=404", "bytes=4", "duration_ms=", "client_gone=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected log to contain %q, got:\n%s", want, out)
		}
	}
}

func TestLoggingMiddleware_reportsClientGoneWhenContextCanceled(t *testing.T) {
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel() // the browser went away while the handler was still working
	})
	req := httptest.NewRequest(http.MethodPost, "/api/score", nil).WithContext(ctx)

	loggingMiddleware(inner).ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(logs.String(), "client_gone=true") {
		t.Errorf("expected client_gone=true, got:\n%s", logs.String())
	}
}

func TestLoggingMiddleware_preservesFlusher(t *testing.T) {
	var flushable bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flushable = w.(http.Flusher)
	})

	loggingMiddleware(inner).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/segments/stream", nil))

	if !flushable {
		t.Error("wrapped ResponseWriter must keep http.Flusher so SSE endpoints still stream")
	}
}

func TestLoggingMiddleware_tracksInflightRequests(t *testing.T) {
	captureLogs(t)
	started := make(chan struct{})
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		loggingMiddleware(inner).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/score", nil))
	}()
	<-started

	during := inflightSnapshot()["/api/score"]
	close(release)
	<-done
	after := inflightSnapshot()["/api/score"]

	if during != 1 {
		t.Errorf("expected 1 in-flight /api/score during the request, got %d", during)
	}
	if after != 0 {
		t.Errorf("expected 0 in-flight /api/score after the request, got %d", after)
	}
}
