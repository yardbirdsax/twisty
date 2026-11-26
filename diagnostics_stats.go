package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"syscall"
	"time"
)

// CPU percentages below use 100 = one full core.
const (
	statsInterval         = 10 * time.Second
	statsLogCPUPercent    = 20  // heartbeat lines are logged at or above this, or while requests are in flight
	unexplainedCPUPercent = 50  // at or above this with no request in flight is worth a warning
	hotCPUPercent         = 150 // sustained at or above this triggers a capture
	hotSustainedIntervals = 3
	hotCaptureCooldown    = 10 * time.Minute
	hotMaxCaptures        = 5
	cpuCaptureDuration    = 10 * time.Second
)

func defaultHotTracker() *hotTracker {
	return &hotTracker{
		threshold:   hotCPUPercent,
		sustained:   hotSustainedIntervals,
		cooldown:    hotCaptureCooldown,
		maxCaptures: hotMaxCaptures,
	}
}

// cpuSample is the process's cumulative CPU time at a point in wall-clock time.
type cpuSample struct {
	at  time.Time
	cpu time.Duration
}

// cpuPercent returns CPU use between two samples where 100 means one full core.
func cpuPercent(prev, cur cpuSample) float64 {
	wall := cur.at.Sub(prev.at)
	if wall <= 0 {
		return 0
	}
	return float64(cur.cpu-prev.cpu) / float64(wall) * 100
}

func processCPUTime() (time.Duration, error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, err
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano()), nil
}

// hotTracker decides when sustained high CPU warrants capturing profiles. It
// limits how often and how many times it fires so a long-running process
// cannot fill the disk.
type hotTracker struct {
	threshold   float64       // cpu percent (100 = one core) that counts as hot
	sustained   int           // consecutive hot intervals required
	cooldown    time.Duration // minimum gap between captures
	maxCaptures int

	streak      int
	captures    int
	lastCapture time.Time
}

// observe records one interval and reports whether to capture now.
func (h *hotTracker) observe(now time.Time, cpuPct float64) bool {
	if cpuPct < h.threshold {
		h.streak = 0
		return false
	}
	h.streak++
	if h.streak < h.sustained {
		return false
	}
	h.streak = 0 // rebuild the streak whether or not this one is allowed to fire
	if h.captures >= h.maxCaptures {
		return false
	}
	if !h.lastCapture.IsZero() && now.Sub(h.lastCapture) < h.cooldown {
		return false
	}
	h.captures++
	h.lastCapture = now
	return true
}

// captureProfiles writes, into dir, a goroutine dump and in-flight request
// snapshot (taken immediately) and a CPU profile of length cpuDur. It returns
// the files written.
func captureProfiles(dir string, cpuDur time.Duration, now time.Time) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	stamp := now.Format("20060102-150405")
	var files []string

	goroutines := filepath.Join(dir, stamp+"-goroutines.txt")
	if err := writeGoroutineDump(goroutines); err != nil {
		return files, err
	}
	files = append(files, goroutines)

	inflight := filepath.Join(dir, stamp+"-inflight.txt")
	if err := os.WriteFile(inflight, []byte(formatInflight(inflightEntries(), now)), 0o644); err != nil {
		return files, err
	}
	files = append(files, inflight)

	cpu := filepath.Join(dir, stamp+"-cpu.pprof")
	if err := writeCPUProfile(cpu, cpuDur); err != nil {
		// Typically another CPU profile (e.g. via the pprof endpoint) is running.
		slog.Warn("cpu profile not captured", "error", err)
		return files, nil
	}
	return append(files, cpu), nil
}

func writeGoroutineDump(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pprof.Lookup("goroutine").WriteTo(f, 2)
}

func writeCPUProfile(path string, d time.Duration) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		os.Remove(path)
		return err
	}
	time.Sleep(d)
	pprof.StopCPUProfile()
	return nil
}

// formatInflight renders one line per request so a capture shows which are old
// or stuck, not just how many there are.
func formatInflight(entries []inflightEntry, now time.Time) string {
	if len(entries) == 0 {
		return "no requests in flight\n"
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "id=%d path=%s remote=%s age=%s %s\n",
			e.ID, e.Path, e.Remote, now.Sub(e.Start).Round(time.Second), e.Note)
	}
	return b.String()
}

// formatInflightCounts renders per-path counts on one line, e.g. "/api/score 3".
func formatInflightCounts(entries []inflightEntry) string {
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Path]++
	}
	paths := make([]string, 0, len(counts))
	for p := range counts {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		parts = append(parts, fmt.Sprintf("%s %d", p, counts[p]))
	}
	return strings.Join(parts, ", ")
}

// startStatsLogger logs process CPU, memory, goroutines and in-flight requests
// every interval, and captures profiles into diagDir when CPU stays hot. A line
// with a high cpu_pct and inflight=map[] would mean the CPU is being burned by
// something other than a request.
func startStatsLogger(ctx context.Context, interval time.Duration, diagDir string, hot *hotTracker) {
	go func() {
		cpu, err := processCPUTime()
		if err != nil {
			slog.Warn("stats logger disabled: cannot read process CPU time", "error", err)
			return
		}
		prev := cpuSample{at: time.Now(), cpu: cpu}
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C:
				cpu, err := processCPUTime()
				if err != nil {
					continue
				}
				cur := cpuSample{at: now, cpu: cpu}
				pct := cpuPercent(prev, cur)
				prev = cur

				entries := inflightEntries()
				if pct >= statsLogCPUPercent || len(entries) > 0 {
					var ms runtime.MemStats
					runtime.ReadMemStats(&ms)
					slog.Info("stats",
						"cpu_pct", int(pct),
						"goroutines", runtime.NumGoroutine(),
						"heap_mb", ms.HeapAlloc>>20,
						"inflight", formatInflightCounts(entries),
					)
				}
				if cpuUnexplainedByRequests(pct, entries) {
					slog.Warn("high CPU with no request in flight", "cpu_pct", int(pct))
				}
				if hot.observe(now, pct) {
					go func() {
						files, err := captureProfiles(diagDir, cpuCaptureDuration, now)
						slog.Warn("sustained high CPU: captured diagnostics", "cpu_pct", int(pct), "files", files, "error", err)
					}()
				}
			}
		}
	}()
}

