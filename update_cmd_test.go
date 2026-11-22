package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUpdateCmd_FlagsMapToOptions(t *testing.T) {
	var got UpdateOptions
	cmd := newUpdateCmdWith(func(_ context.Context, opts UpdateOptions, _ io.Writer) error {
		got = opts
		return nil
	})
	cmd.SetArgs([]string{"--only", "overpass", "--force", "--dry-run", "--in-place"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	want := UpdateOptions{Only: "overpass", Force: true, DryRun: true, InPlace: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("options = %+v, want %+v", got, want)
	}
}

func TestUpdateCmd_DefaultsAreOff(t *testing.T) {
	var got UpdateOptions
	cmd := newUpdateCmdWith(func(_ context.Context, opts UpdateOptions, _ io.Writer) error {
		got = opts
		return nil
	})
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, UpdateOptions{}) {
		t.Errorf("options = %+v, want zero value", got)
	}
}

func TestUpdateCmd_RunnerErrorIsReturned(t *testing.T) {
	want := errors.New("boom")
	cmd := newUpdateCmdWith(func(context.Context, UpdateOptions, io.Writer) error { return want })
	cmd.SetArgs(nil)
	if err := cmd.Execute(); !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestUpdateCmd_RegisteredOnRoot(t *testing.T) {
	found := false
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "update" {
			found = true
		}
	}
	if !found {
		t.Error("update command not registered on root")
	}
}

func TestRenderUpdateReport(t *testing.T) {
	rep := UpdateReport{Services: []ServiceReport{
		{Service: "valhalla", Outcome: OutcomeChanged, Regions: []RegionStatus{
			{Region: "us/vermont", Changed: true, Reason: "upstream newer", DataAge: 36 * time.Hour, HasDataAge: true},
			{Region: "us/maine", Reason: "up to date"},
		}},
		{Service: "overpass", Outcome: OutcomeFailed, Detail: "checking upstream: us/maine: HEAD: 503"},
	}}
	var buf bytes.Buffer

	renderUpdateReport(&buf, rep, UpdateOptions{DryRun: true})

	out := buf.String()
	for _, want := range []string{
		"valhalla: changed",
		"us/vermont", "changed (upstream newer)", "data age 1d12h",
		"us/maine", "unchanged",
		"overpass: failed", "HEAD: 503",
		"dry run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderUpdateReport_NothingChanged(t *testing.T) {
	rep := UpdateReport{Services: []ServiceReport{{Service: "valhalla", Outcome: OutcomeUnchanged}}}
	var buf bytes.Buffer
	renderUpdateReport(&buf, rep, UpdateOptions{})
	if !strings.Contains(buf.String(), "Nothing to update") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRenderUpdateReport_PrintsClearedCaches(t *testing.T) {
	rep := UpdateReport{Services: []ServiceReport{
		{Service: "overpass", Outcome: OutcomeUpdated, Detail: "cleared caches: /c/overpass, /c/scores"},
	}}
	var buf bytes.Buffer

	renderUpdateReport(&buf, rep, UpdateOptions{})

	out := buf.String()
	if !strings.Contains(out, "cleared caches: /c/overpass, /c/scores") {
		t.Errorf("output missing cache removals:\n%s", out)
	}
	if strings.Contains(out, "not implemented") {
		t.Errorf("output has stale message:\n%s", out)
	}
}

func TestRenderUpdateReport_UnrefreshedChangeIsNotClaimedUnimplemented(t *testing.T) {
	rep := UpdateReport{Services: []ServiceReport{{Service: "overpass", Outcome: OutcomeChanged}}}
	var buf bytes.Buffer

	renderUpdateReport(&buf, rep, UpdateOptions{})

	if strings.Contains(buf.String(), "not implemented") {
		t.Errorf("stale message in output:\n%s", buf.String())
	}
}
