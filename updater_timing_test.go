package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func timedStages(sr ServiceReport) []stageName {
	var out []stageName
	for _, st := range sr.Timings {
		out = append(out, st.Stage)
	}
	return out
}

func hasStage(sr ServiceReport, stage stageName) bool {
	for _, st := range sr.Timings {
		if st.Stage == stage {
			return true
		}
	}
	return false
}

func TestTiming_ValhallaReportsDownloadAndBuild(t *testing.T) {
	e := newValhallaEnv(t, vermont, maine)
	e.makeChanged(vermont)
	e.makeChanged(maine)
	inner := e.runtime.OnRunToCompletion
	e.runtime.OnRunToCompletion = func(spec ContainerSpec) error {
		time.Sleep(5 * time.Millisecond)
		return inner(spec)
	}

	rep := e.update(t, UpdateOptions{})

	v := findService(t, rep, "valhalla")
	for _, st := range []stageName{stageDownload, stageMerge, stageBuild} {
		if !hasStage(v, st) {
			t.Errorf("valhalla timings = %v, missing %s", timedStages(v), st)
		}
	}
	for _, st := range v.Timings {
		if st.Duration <= 0 {
			t.Errorf("stage %s duration = %v, want > 0", st.Stage, st.Duration)
		}
	}
}

func TestTiming_OverpassReportsEveryStage(t *testing.T) {
	e := newPipelineEnv(t)

	rep := e.update(t, nil)

	o := findService(t, rep, "overpass")
	for _, st := range []stageName{stageDownload, stageConvert, stageMerge, stageImport} {
		if !hasStage(o, st) {
			t.Errorf("overpass timings = %v, missing %s", timedStages(o), st)
		}
	}
}

func TestTiming_SkippedServicesAndDryRunReportNoTimings(t *testing.T) {
	e := newValhallaEnv(t, vermont) // up to date

	rep := e.update(t, UpdateOptions{})
	if v := findService(t, rep, "valhalla"); len(v.Timings) != 0 {
		t.Errorf("unchanged service timings = %v, want none", timedStages(v))
	}

	e.makeChanged(vermont)
	rep = e.update(t, UpdateOptions{DryRun: true})
	if v := findService(t, rep, "valhalla"); len(v.Timings) != 0 {
		t.Errorf("dry-run timings = %v, want none", timedStages(v))
	}
}

func TestTiming_FailedStageIsTimedAndLaterStagesAreNot(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	e.runtime.OnRunToCompletion = func(ContainerSpec) error { return errors.New("boom") }

	rep := e.update(t, UpdateOptions{})

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %v, want failed", v.Outcome)
	}
	if !hasStage(v, stageDownload) || !hasStage(v, stageBuild) {
		t.Errorf("timings = %v, want download and the failed build", timedStages(v))
	}
	if hasStage(v, stageSwap) {
		t.Errorf("timings = %v, include a stage that never ran", timedStages(v))
	}
}

func TestTiming_ReportPrintsStageDurations(t *testing.T) {
	rep := UpdateReport{Services: []ServiceReport{
		{Service: ServiceValhalla, Outcome: OutcomeUpdated, Timings: []StageTiming{
			{stageDownload, 1500 * time.Millisecond},
			{stageBuild, 90 * time.Second},
		}},
		{Service: ServiceOverpass, Outcome: OutcomeUnchanged},
	}}
	var buf bytes.Buffer

	renderUpdateReport(&buf, rep, UpdateOptions{})

	out := buf.String()
	if !strings.Contains(out, "download 1.5s") || !strings.Contains(out, "build 1m30s") {
		t.Errorf("report missing timings:\n%s", out)
	}
	if strings.Count(out, "timings:") != 1 {
		t.Errorf("want exactly one timings line (unchanged service has none):\n%s", out)
	}
}
