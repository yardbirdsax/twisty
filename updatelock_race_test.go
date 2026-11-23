package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// Two processes can both see the same stale lock. If one of them has already
// replaced it with its own fresh lock by the time the other takes over, the
// other must not delete that fresh lock.
func TestAcquireUpdateLock_DoesNotRemoveFreshLockReplacingTheStaleOne(t *testing.T) {
	dir := t.TempDir()
	writeLock(t, dir, deadPID(t))
	lockHeld := strconv.Itoa(os.Getpid())
	staleTakeoverHook = func() {
		// the rival got there first: its fresh lock replaces the stale one
		os.WriteFile(updateLockPath(dir), []byte(lockHeld), 0o644)
	}
	t.Cleanup(func() { staleTakeoverHook = nil })

	release, err := acquireUpdateLock(dir)

	if err == nil {
		release()
		t.Fatal("acquired the lock although a live process had just taken it")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("err = %v, want 'already running'", err)
	}
	b, rerr := os.ReadFile(updateLockPath(dir))
	if rerr != nil || strings.TrimSpace(string(b)) != lockHeld {
		t.Errorf("the rival's fresh lock was destroyed: %q, %v", b, rerr)
	}
}

func TestAcquireUpdateLock_ReleaseOnlyRemovesLockStillHoldingOurPID(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireUpdateLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Someone else legitimately took over (e.g. our lock was judged stale).
	other := deadPID(t) + 100000
	writeLock(t, dir, other)

	release()

	b, rerr := os.ReadFile(updateLockPath(dir))
	if rerr != nil || strings.TrimSpace(string(b)) != strconv.Itoa(other) {
		t.Errorf("release removed a lock it no longer owned: %q, %v", b, rerr)
	}
}

func TestAcquireUpdateLock_LeavesNoTakeoverFilesBehind(t *testing.T) {
	dir := t.TempDir()
	writeLock(t, dir, deadPID(t))

	release, err := acquireUpdateLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	release()

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("leftover files after takeover and release: %v", entries)
	}
}
