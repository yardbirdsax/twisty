package osmconv

import (
	"bytes"
	"testing"
	"time"
)

func TestReadPBFHeaderTimestamp(t *testing.T) {
	want := time.Date(2026, 11, 20, 21, 20, 0, 0, time.UTC)
	data := BuildPBFBytesWithReplicationTimestamp(want.Unix())

	got, ok, err := ReadPBFHeaderTimestamp(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || !got.Equal(want) {
		t.Errorf("got (%v, %v), want (%v, true)", got, ok, want)
	}
}

func TestReadPBFHeaderTimestamp_Absent(t *testing.T) {
	_, ok, err := ReadPBFHeaderTimestamp(bytes.NewReader(BuildMinimalPBFBytes()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false when header has no replication timestamp")
	}
}

func TestReadPBFHeaderTimestamp_NotAPBF(t *testing.T) {
	if _, _, err := ReadPBFHeaderTimestamp(bytes.NewReader([]byte("garbage"))); err == nil {
		t.Error("expected error for non-PBF input")
	}
}
