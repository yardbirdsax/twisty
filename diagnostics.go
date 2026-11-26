package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ─── in-flight request tracking ──────────────────────────────────────────────

// inflightEntry describes one request currently being served.
type inflightEntry struct {
	ID     uint64
	Path   string
	Remote string
	Start  time.Time
	Note   string // set by the handler, e.g. the route fingerprint of a score request
}

var (
	inflightMu   sync.Mutex
	inflightReqs = map[uint64]*inflightEntry{}
	requestSeq   atomic.Uint64
)

// streamingPaths are long-lived responses that stay open for as long as the UI
// does. They are in flight but are not evidence that a request is burning CPU.
var streamingPaths = map[string]bool{"/api/segments/stream": true}

// inflightEnter records a request and returns the total now in flight.
func inflightEnter(id uint64, path, remote string, start time.Time) int {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	inflightReqs[id] = &inflightEntry{ID: id, Path: path, Remote: remote, Start: start}
	return len(inflightReqs)
}

func inflightLeave(id uint64) {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	delete(inflightReqs, id)
}

func inflightTotal() int {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	return len(inflightReqs)
}

// inflightEntries returns a copy of the requests in flight, oldest first.
func inflightEntries() []inflightEntry {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	out := make([]inflightEntry, 0, len(inflightReqs))
	for _, e := range inflightReqs {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// inflightSnapshot returns the per-path in-flight request counts.
func inflightSnapshot() map[string]int {
	counts := map[string]int{}
	for _, e := range inflightEntries() {
		counts[e.Path]++
	}
	return counts
}

// noteInflight attaches a note to the request carried by ctx. It is a no-op
// outside loggingMiddleware.
func noteInflight(ctx context.Context, note string) {
	id := requestIDFrom(ctx)
	inflightMu.Lock()
	defer inflightMu.Unlock()
	if e, ok := inflightReqs[id]; ok {
		e.Note = note
	}
}

// cpuUnexplainedByRequests reports whether CPU is high while no request that
// could be doing the work is in flight (streams that merely sit open don't count).
func cpuUnexplainedByRequests(cpuPct float64, entries []inflightEntry) bool {
	if cpuPct < unexplainedCPUPercent {
		return false
	}
	for _, e := range entries {
		if !streamingPaths[e.Path] {
			return false
		}
	}
	return true
}

type requestIDKey struct{}

// requestIDFrom returns the id loggingMiddleware assigned to the request, or 0.
func requestIDFrom(ctx context.Context) uint64 {
	id, _ := ctx.Value(requestIDKey{}).(uint64)
	return id
}

// pointsHash is a short stable fingerprint of a polyline. Identical hashes in
// the log mean the client sent the same route more than once.
func pointsHash(points [][2]float64) string {
	h := fnv.New64a()
	var buf [8]byte
	for _, p := range points {
		for _, v := range p {
			binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
			h.Write(buf[:])
		}
	}
	return fmt.Sprintf("%08x", h.Sum64()&0xffffffff)
}

// logScoreStats logs the work one scoring request did, tagged with the request
// id so it can be joined to that request's "http done" line. extra carries
// handler-specific key/value pairs (e.g. the route fingerprint).
func logScoreStats(msg string, r *http.Request, tiles, cached, pending, failed int, sr scoreResult, ensure, score time.Duration, extra ...any) {
	attrs := append([]any{"id", requestIDFrom(r.Context())}, extra...)
	attrs = append(attrs,
		"tiles", tiles,
		"cached_tiles", cached,
		"pending_tiles", pending,
		"failed_tiles", failed,
		"segments_checked", sr.Checked,
		"segments_matched", sr.Matched,
		"ensure_ms", ensure.Milliseconds(),
		"score_ms", score.Milliseconds(),
		"tile_load_ms", sr.TileLoad.Milliseconds(),
		"match_ms", sr.Match.Milliseconds(),
		"client_gone", r.Context().Err() != nil,
	)
	slog.Info(msg, attrs...)
}

// ─── request logging ─────────────────────────────────────────────────────────

// statusWriter records the status code and body size of a response. It keeps
// http.Flusher so streaming endpoints (SSE) still work through the middleware.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) statusOrDefault() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// loggingMiddleware logs the start and completion of every request and gives
// each an id (also carried in the request context, so handler log lines can be
// joined to these). The completion line carries status, duration, and whether
// the client had already gone away (client_gone=true means the work was wasted).
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestSeq.Add(1)
		start := time.Now()
		total := inflightEnter(id, r.URL.Path, r.RemoteAddr, start)
		slog.Info("http request", "id", id, "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "inflight", total)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))

		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			inflightLeave(id)
			slog.Info("http done",
				"id", id,
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.statusOrDefault(),
				"bytes", sw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"client_gone", r.Context().Err() != nil,
				"inflight", inflightTotal(),
			)
		}()
		next.ServeHTTP(sw, r)
	})
}
