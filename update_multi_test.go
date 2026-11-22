package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// newTwoServiceEnv loads vermont into Valhalla only and maine into Overpass
// only, both changed upstream, so each service has its own independent work
// (no shared region stamp) and both can be refreshed through the fake runtime.
func newTwoServiceEnv(t *testing.T) *stagedOverpassEnv {
	t.Helper()
	e := newStagedOverpassEnv(t, maine)
	e.load("valhalla", vermont, t0, t1)
	e.makeChanged(maine)
	e.liveTilesFor(t)
	return e
}

// serviceOfCall classifies a fakeRuntime call as belonging to a service, or ""
// for calls that identify neither (e.g. a readiness URL of an unknown port).
func serviceOfCall(call string) string {
	switch {
	case strings.Contains(call, "valhalla"), strings.Contains(call, ":8002/"):
		return "valhalla"
	case strings.Contains(call, "overpass"):
		return "overpass"
	}
	return ""
}

func TestMultiService_RunsValhallaThenOverpassSequentially(t *testing.T) {
	e := newTwoServiceEnv(t)

	rep := e.update(t, UpdateOptions{})

	for _, s := range rep.Services {
		if s.Outcome != OutcomeUpdated {
			t.Fatalf("%s = %v (%s), want updated", s.Service, s.Outcome, s.Detail)
		}
	}
	lastValhalla, firstOverpass := -1, -1
	for i, c := range e.runtime.calls {
		switch serviceOfCall(c) {
		case "valhalla":
			lastValhalla = i
		case "overpass":
			if firstOverpass < 0 {
				firstOverpass = i
			}
		}
	}
	if lastValhalla < 0 || firstOverpass < 0 {
		t.Fatalf("expected calls for both services, got %v", e.runtime.calls)
	}
	if lastValhalla > firstOverpass {
		t.Errorf("valhalla work must finish before overpass starts; calls = %v", e.runtime.calls)
	}
}

func TestMultiService_ValhallaFailureStillAttemptsOverpass(t *testing.T) {
	e := newTwoServiceEnv(t)
	e.upstream.downloadErr[vermont] = errors.New("boom")

	rep := e.update(t, UpdateOptions{})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeFailed {
		t.Errorf("valhalla = %v, want failed", v.Outcome)
	}
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Errorf("overpass = %v (%s), want updated despite valhalla failure", o.Outcome, o.Detail)
	}
}

func TestMultiService_OverpassFailureLeavesValhallaUpdated(t *testing.T) {
	e := newTwoServiceEnv(t)
	e.convErr = errors.New("conversion failed")

	rep := e.update(t, UpdateOptions{})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUpdated {
		t.Errorf("valhalla = %v (%s), want updated", v.Outcome, v.Detail)
	}
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeFailed {
		t.Errorf("overpass = %v, want failed", o.Outcome)
	}
}

func TestMultiService_OnlyLimitsToOneService(t *testing.T) {
	for _, only := range []string{"valhalla", "overpass"} {
		t.Run(only, func(t *testing.T) {
			e := newTwoServiceEnv(t)

			rep := e.update(t, UpdateOptions{Only: only})

			if len(rep.Services) != 1 || string(rep.Services[0].Service) != only {
				t.Fatalf("services = %+v, want only %s", rep.Services, only)
			}
			for _, c := range e.runtime.calls {
				if other := serviceOfCall(c); other != "" && other != only {
					t.Errorf("--only %s touched %s: %q", only, other, c)
				}
			}
		})
	}
}

// runUpdateWith is the command-level seam: it takes injected dependencies, runs
// the update, writes the report and summary, and returns the exit error.
func TestRunUpdateWith_SummaryListsEachServiceAndFailureIsNonZero(t *testing.T) {
	e := newTwoServiceEnv(t)
	e.convErr = errors.New("conversion failed")
	var buf bytes.Buffer

	err := runUpdateWith(context.Background(), e.deps(), UpdateOptions{}, &buf)

	if err == nil {
		t.Error("exit error = nil, want non-nil when a service failed")
	}
	out := buf.String()
	for _, want := range []string{"Summary", "valhalla: updated", "overpass: failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunUpdateWith_AllSucceededExitsZeroAndSummarizes(t *testing.T) {
	e := newTwoServiceEnv(t)
	var buf bytes.Buffer

	if err := runUpdateWith(context.Background(), e.deps(), UpdateOptions{}, &buf); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "Summary") {
		t.Errorf("output missing summary:\n%s", buf.String())
	}
}

func TestRunUpdateWith_UnchangedServiceListedAsUnchanged(t *testing.T) {
	e := newTwoServiceEnv(t)
	e.upstream.info[vermont] = UpstreamInfo{LastModified: t0, ContentLength: 100} // valhalla up to date
	var buf bytes.Buffer

	if err := runUpdateWith(context.Background(), e.deps(), UpdateOptions{}, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"valhalla: unchanged", "overpass: updated"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
