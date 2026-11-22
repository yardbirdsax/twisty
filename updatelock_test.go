package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// deadPID returns the PID of a process that has already exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func writeLock(t *testing.T, pbfDir string, pid int) {
	t.Helper()
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(updateLockPath(pbfDir), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdate_RefusesWhileAnotherUpdateHoldsLock(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)
	writeLock(t, e.root.PBFDir, os.Getpid()) // a live process

	_, err := Update(context.Background(), e.deps(), UpdateOptions{})

	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v, want 'already running' refusal", err)
	}
	if len(e.upstream.heads) != 0 {
		t.Errorf("update proceeded despite lock: %v", e.upstream.heads)
	}
}

func TestUpdate_IgnoresAndReplacesStaleLock(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)
	writeLock(t, e.root.PBFDir, deadPID(t))

	if _, err := Update(context.Background(), e.deps(), UpdateOptions{}); err != nil {
		t.Fatalf("stale lock should be ignored: %v", err)
	}
	if _, err := os.Stat(updateLockPath(e.root.PBFDir)); !os.IsNotExist(err) {
		t.Errorf("lock should be released after run, stat err = %v", err)
	}
}

func TestUpdate_ReleasesLockOnSuccess(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)

	if _, err := Update(context.Background(), e.deps(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(updateLockPath(e.root.PBFDir)); !os.IsNotExist(err) {
		t.Errorf("lock not released, stat err = %v", err)
	}
}

func TestUpdate_ReleasesLockOnFailure(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t1, t1)
	delete(e.upstream.info, "north-america/us/vermont") // fake upstream errors

	rep, err := Update(context.Background(), e.deps(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Failed() {
		t.Fatal("expected failed report")
	}
	if _, err := os.Stat(updateLockPath(e.root.PBFDir)); !os.IsNotExist(err) {
		t.Errorf("lock not released after failure, stat err = %v", err)
	}
}

func TestUpdate_ReleasesLockOnInvalidOptions(t *testing.T) {
	e := newUpdaterEnv(t)
	if _, err := Update(context.Background(), e.deps(), UpdateOptions{Only: "bogus"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(updateLockPath(e.root.PBFDir)); !os.IsNotExist(err) {
		t.Errorf("lock left behind, stat err = %v", err)
	}
}

// ─── command wiring ──────────────────────────────────────────────────────────

func TestStartAndCleanRefuseWhileUpdateLockHeld(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeLock(t, resolvePBFDir(""), os.Getpid())
	if filepath.Dir(resolvePBFDir("")) != filepath.Join(home, ".twisty") {
		t.Fatalf("HOME override did not take effect: %s", resolvePBFDir(""))
	}

	for name, run := range map[string]func() error{
		"overpass start": func() error {
			c := newOverpassCmd()
			c.SetArgs([]string{"start", "--regions", "x"})
			return c.Execute()
		},
		"overpass clean": func() error { c := newOverpassCmd(); c.SetArgs([]string{"clean"}); return c.Execute() },
		"valhalla start": func() error {
			c := newValhallaCmd()
			c.SetArgs([]string{"start", "--regions", "x"})
			return c.Execute()
		},
		"valhalla clean": func() error { c := newValhallaCmd(); c.SetArgs([]string{"clean"}); return c.Execute() },
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "twisty update is running") {
			t.Errorf("%s: err = %v, want refusal naming the running update", name, err)
		}
	}
}
