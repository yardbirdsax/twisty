package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// The update lock is a file in the shared PBF directory holding the owning
// PID. `update` holds it for its whole run; `start` and `clean` for both
// services refuse while a live process owns it. A lock whose PID is gone is
// stale and is ignored (and replaced by the next `update`).

func updateLockPath(pbfDir string) string {
	return filepath.Join(pbfDir, "update.lock")
}

// staleTakeoverHook is a test seam: it runs between a process judging the lock
// stale and taking it over, the window in which a rival can slip in.
var staleTakeoverHook func()

// pidAlive reports whether a process with this PID exists.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// lockContent returns the trimmed contents of the lock file and the PID in it
// (0 if it is not a number).
func lockContent(path string) (content string, pid int, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	content = strings.TrimSpace(string(b))
	pid, convErr := strconv.Atoi(content)
	if convErr != nil {
		pid = 0
	}
	return content, pid, nil
}

// updateLockOwner returns the PID of a live process holding the lock; ok is
// false if there is no lock or it is stale.
func updateLockOwner(pbfDir string) (pid int, ok bool) {
	_, pid, err := lockContent(updateLockPath(pbfDir))
	if err != nil || !pidAlive(pid) {
		return 0, false
	}
	return pid, true
}

// acquireUpdateLock takes the update lock, failing fast if another live
// process holds it. The returned function releases the lock, and only removes
// it if it still holds our PID.
//
// A stale lock is taken over without a window in which a rival's fresh lock
// could be deleted: the stale file is renamed aside (an atomic move of
// whatever is at the path right now), and what was moved is re-verified. If it
// is not the stale lock we judged, a rival slipped in: it is put back and its
// owner reported. Taking the lock itself is O_EXCL, so only one process wins.
func acquireUpdateLock(pbfDir string) (release func(), err error) {
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating lock directory: %w", err)
	}
	path := updateLockPath(pbfDir)
	me := strconv.Itoa(os.Getpid())
	for attempt := 0; attempt < 5; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, werr := fmt.Fprint(f, me)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				os.Remove(path)
				return nil, fmt.Errorf("writing update lock: %w", errors.Join(werr, cerr))
			}
			return func() { releaseUpdateLock(path, me) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("taking update lock: %w", err)
		}
		content, pid, rerr := lockContent(path)
		if rerr != nil {
			continue // vanished between our create and read: try again
		}
		if pidAlive(pid) {
			return nil, alreadyRunning(pid, path)
		}
		if staleTakeoverHook != nil {
			staleTakeoverHook()
		}
		// Stale: move it aside, then check we moved the file we judged stale.
		aside := path + ".stale." + me
		if err := os.Rename(path, aside); err != nil {
			continue // someone else took it over first
		}
		movedContent, movedPID, rerr := lockContent(aside)
		if rerr == nil && movedContent != content && pidAlive(movedPID) {
			// We moved a rival's fresh lock. Restore it (a no-op if a newer lock
			// already exists) and defer to its owner.
			os.Link(aside, path)
			os.Remove(aside)
			return nil, alreadyRunning(movedPID, path)
		}
		os.Remove(aside)
	}
	return nil, fmt.Errorf("could not take update lock %s", path)
}

func alreadyRunning(pid int, path string) error {
	return fmt.Errorf("another twisty update is already running (pid %d, lock %s)", pid, path)
}

// releaseUpdateLock removes the lock at path only if it still holds our PID: if
// it was taken over, the new owner's lock is not ours to delete.
func releaseUpdateLock(path, me string) {
	if content, _, err := lockContent(path); err == nil && content == me {
		os.Remove(path)
	}
}

// refuseDuringUpdate is a cobra PreRunE for commands that must not run while
// `twisty update` is swapping data underneath them. The lock lives in the
// shared PBF directory, which is always the default one: `start` ignores
// --data-dir for PBFs, and `update --data-dir` does not move it either, so the
// lock location is the same whatever data directories are in play.
func refuseDuringUpdate(cmd *cobra.Command, args []string) error {
	if pid, held := updateLockOwner(resolvePBFDir("")); held {
		return fmt.Errorf("twisty update is running (pid %d); wait for it to finish before running `%s`", pid, cmd.CommandPath())
	}
	return nil
}
