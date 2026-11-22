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

// pidAlive reports whether a process with this PID exists.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// updateLockOwner returns the PID of a live process holding the lock; ok is
// false if there is no lock or it is stale.
func updateLockOwner(pbfDir string) (pid int, ok bool) {
	b, err := os.ReadFile(updateLockPath(pbfDir))
	if err != nil {
		return 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || !pidAlive(pid) {
		return 0, false
	}
	return pid, true
}

// acquireUpdateLock takes the update lock, failing fast if another live
// process holds it. The returned function releases the lock.
func acquireUpdateLock(pbfDir string) (release func(), err error) {
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating lock directory: %w", err)
	}
	path := updateLockPath(pbfDir)
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, werr := fmt.Fprint(f, os.Getpid())
			cerr := f.Close()
			if werr != nil || cerr != nil {
				os.Remove(path)
				return nil, fmt.Errorf("writing update lock: %w", errors.Join(werr, cerr))
			}
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("taking update lock: %w", err)
		}
		if pid, held := updateLockOwner(pbfDir); held {
			return nil, fmt.Errorf("another twisty update is already running (pid %d, lock %s)", pid, path)
		}
		os.Remove(path) // stale: owner is gone
	}
	return nil, fmt.Errorf("could not take update lock %s", path)
}

// refuseDuringUpdate is a cobra PreRunE for commands that must not run while
// `twisty update` is swapping data underneath them.
func refuseDuringUpdate(cmd *cobra.Command, args []string) error {
	if pid, held := updateLockOwner(resolvePBFDir("")); held {
		return fmt.Errorf("twisty update is running (pid %d); wait for it to finish before running `%s`", pid, cmd.CommandPath())
	}
	return nil
}
