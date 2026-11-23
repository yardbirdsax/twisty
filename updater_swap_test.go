package main

import (
	"errors"
	"testing"
)

// The old container is sampled right before the swap stops it, not before the
// (hours-long) import: one that died meanwhile is not resurrected by a rollback.
func TestOverpassUpdate_RollbackDoesNotRestartContainerThatDiedDuringImport(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	liveURL := overpassReadyURL(defaultOverpassPort)
	stagingURL := overpassReadyURL(defaultOverpassStagingPort)
	e.runtime.OnWaitReady = func(url string) error {
		switch url {
		case stagingURL:
			e.runtime.running[overpassContainerName] = false // dies while importing
		case liveURL:
			return errors.New("never became ready")
		}
		return nil
	}

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %v, want failed", o.Outcome)
	}
	if d := e.liveData(t); d != "old" {
		t.Errorf("live db = %q, want the old one restored", d)
	}
	runs := 0
	for _, c := range e.runtime.calls {
		if c == "run "+overpassContainerName {
			runs++
		}
	}
	if runs != 1 {
		t.Errorf("live container started %d times, want 1 (the new one only); calls = %v", runs, e.runtime.calls)
	}
	if e.runtime.IsRunning(overpassContainerName) {
		t.Error("the failed new container must not be left running")
	}
}

func TestValhallaUpdate_RollbackDoesNotRestartContainerThatWasNotRunning(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	e.runtime.running[valhallaContainerName] = false
	e.runtime.OnWaitReady = func(string) error { return errors.New("never ready") }

	e.update(t, UpdateOptions{})

	if e.runtime.IsRunning(valhallaContainerName) {
		t.Error("a container that was down before the swap must stay down after rollback")
	}
}
