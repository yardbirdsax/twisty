package main

import (
	"bytes"
	"strings"
	"testing"
)

// A download the plan did not foresee still gets its own position, after the
// planned ones, rather than a misleading 1 of 1.
func TestUpdateProgress_UnplannedRegionExtendsThePlan(t *testing.T) {
	rec := &recordingProgress{}
	run := newUpdateRun(UpdateDeps{Progress: rec}, []ServiceReport{{
		Service: ServiceValhalla, Outcome: OutcomeChanged,
		Regions: []RegionStatus{{Region: vermont, Changed: true, ContentLength: 100}},
	}})

	run.startDownload(vermont, "/pbf/vermont.osm.pbf")
	run.startDownload(maine, "/pbf/maine.osm.pbf") // not in the plan

	want := []startCall{
		{name: "/pbf/vermont.osm.pbf", index: 0, total: 1, totalBytes: 100},
		{name: "/pbf/maine.osm.pbf", index: 1, total: 2, totalBytes: -1},
	}
	if len(rec.starts) != 2 || rec.starts[0] != want[0] || rec.starts[1] != want[1] {
		t.Errorf("StartFile calls = %+v, want %+v", rec.starts, want)
	}
}

// A finished file stays on screen as a line of its own, so a later failure
// cannot leave a bar that looks like more got done than did.
func TestSequentialTermBar_FileCompleteLeavesALineOfItsOwn(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf, sequential: true}
	fp := bar.StartFile("/pbf/vermont-latest.osm.pbf", 1, 3, 10*1024*1024)
	fp.BytesDownloaded(10 * 1024 * 1024)
	buf.Reset()

	fp.FileComplete()

	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("completed file did not end its line: %q", out)
	}
	for _, want := range []string{"vermont-latest.osm.pbf", "10.0 MB", "2/3"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in %q", want, out)
		}
	}
}
