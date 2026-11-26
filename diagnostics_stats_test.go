package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCPUPercent(t *testing.T) {
	t0 := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		prev cpuSample
		cur  cpuSample
		want float64
	}{
		{"idle", cpuSample{t0, 10 * time.Second}, cpuSample{t0.Add(10 * time.Second), 10 * time.Second}, 0},
		{"one core busy", cpuSample{t0, 0}, cpuSample{t0.Add(10 * time.Second), 10 * time.Second}, 100},
		{"four cores busy", cpuSample{t0, 0}, cpuSample{t0.Add(10 * time.Second), 40 * time.Second}, 400},
		{"zero elapsed", cpuSample{t0, 0}, cpuSample{t0, time.Second}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cpuPercent(tt.prev, tt.cur); got != tt.want {
				t.Errorf("cpuPercent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHotTracker_triggersAfterSustainedLoadThenCoolsDown(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	h := &hotTracker{threshold: 150, sustained: 3, cooldown: 10 * time.Minute, maxCaptures: 5}
	step := func(pct float64) bool {
		now = now.Add(10 * time.Second)
		return h.observe(now, pct)
	}

	if anyTrue(step(300), step(300)) {
		t.Fatal("must not trigger before the load has been sustained")
	}
	if !step(300) {
		t.Fatal("expected a capture on the third consecutive hot interval")
	}
	if anyTrue(step(300), step(300), step(300)) {
		t.Fatal("must not capture again during the cooldown")
	}

	now = now.Add(11 * time.Minute)
	if anyTrue(step(300), step(300)) {
		t.Fatal("hot streak must be rebuilt after the cooldown")
	}
	if !step(300) {
		t.Fatal("expected a second capture once the cooldown passed and load stayed hot")
	}
}

// anyTrue reports whether any argument is true. Unlike ||, every argument has
// already been evaluated by the caller, so side-effecting steps all run.
func anyTrue(bs ...bool) bool {
	for _, b := range bs {
		if b {
			return true
		}
	}
	return false
}

func TestHotTracker_aCoolIntervalResetsTheStreak(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	h := &hotTracker{threshold: 150, sustained: 3, cooldown: time.Minute, maxCaptures: 5}
	for i, pct := range []float64{300, 300, 10, 300, 300} {
		now = now.Add(10 * time.Second)
		if h.observe(now, pct) {
			t.Fatalf("interval %d: unexpected capture; the 10%% interval should have reset the streak", i)
		}
	}
}

func TestHotTracker_stopsAtMaxCaptures(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	h := &hotTracker{threshold: 150, sustained: 1, cooldown: time.Second, maxCaptures: 2}
	captures := 0
	for range 10 {
		now = now.Add(time.Minute)
		if h.observe(now, 400) {
			captures++
		}
	}
	if captures != 2 {
		t.Errorf("expected exactly 2 captures, got %d", captures)
	}
}

func TestCaptureProfiles_writesCPUGoroutinesAndInflight(t *testing.T) {
	captureLogs(t)
	dir := t.TempDir()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	inflightEnter(9999, "/api/score", "127.0.0.1:51126", now.Add(-90*time.Second))
	defer inflightLeave(9999)

	files, err := captureProfiles(dir, 50*time.Millisecond, now)
	if err != nil {
		t.Fatalf("captureProfiles: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files (cpu, goroutines, inflight), got %v", files)
	}
	for _, f := range files {
		if filepath.Dir(f) != dir {
			t.Errorf("%s written outside %s", f, dir)
		}
		if fi, err := os.Stat(f); err != nil || fi.Size() == 0 {
			t.Errorf("expected non-empty file %s (err=%v)", f, err)
		}
	}
	inflight := readFileWithSuffix(t, files, "inflight.txt")
	for _, want := range []string{"id=9999", "path=/api/score", "remote=127.0.0.1:51126", "age=1m3"} {
		if !strings.Contains(inflight, want) {
			t.Errorf("expected the in-flight snapshot to contain %q, got %q", want, inflight)
		}
	}
	goroutines := readFileWithSuffix(t, files, "goroutines.txt")
	if !strings.Contains(goroutines, "goroutine ") {
		t.Errorf("expected a goroutine dump, got %.80q", goroutines)
	}
}

func readFileWithSuffix(t *testing.T, files []string, suffix string) string {
	t.Helper()
	for _, f := range files {
		if strings.HasSuffix(f, suffix) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatalf("no file with suffix %q in %v", suffix, files)
	return ""
}
