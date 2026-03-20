package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTermProgressBarStatsString(t *testing.T) {
	tests := []struct {
		name        string
		bar         termProgressBar
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "no retries no fetches",
			bar:  termProgressBar{cached: 2, fetched: 3},
			wantContain: []string{"2 cached", "3 fetched"},
			wantAbsent:  []string{"retry", "last:", "avg:"},
		},
		{
			name:        "one retry singular",
			bar:         termProgressBar{cached: 0, fetched: 1, retries: 1},
			wantContain: []string{"1 retry"},
			wantAbsent:  []string{"retries"},
		},
		{
			name:        "multiple retries plural",
			bar:         termProgressBar{cached: 0, fetched: 1, retries: 3},
			wantContain: []string{"3 retries"},
			wantAbsent:  []string{"3 retry"},
		},
		{
			name: "one fetch duration",
			bar: termProgressBar{
				cached:             0,
				fetched:            1,
				fetchCount:         1,
				lastFetchDuration:  8300 * time.Millisecond,
				totalFetchDuration: 8300 * time.Millisecond,
			},
			wantContain: []string{"last: 8.3s", "avg: 8.3s"},
		},
		{
			name: "multiple fetch durations averages correctly",
			bar: termProgressBar{
				cached:             0,
				fetched:            2,
				fetchCount:         2,
				lastFetchDuration:  4000 * time.Millisecond,
				totalFetchDuration: 10000 * time.Millisecond,
			},
			wantContain: []string{"last: 4.0s", "avg: 5.0s"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.bar.statsString()
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("statsString() = %q, want it to contain %q", got, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("statsString() = %q, want it NOT to contain %q", got, absent)
				}
			}
		})
	}
}

func TestTermProgressBarRetryRendersImmediately(t *testing.T) {
	var buf bytes.Buffer
	b := &termProgressBar{w: &buf, total: 5, current: 2, cached: 1, fetched: 1}
	b.Retry()
	if b.retries != 1 {
		t.Errorf("Retry() retries = %d, want 1", b.retries)
	}
	out := buf.String()
	if !strings.Contains(out, "1 retry") {
		t.Errorf("Retry() rendered output %q, want it to contain \"1 retry\"", out)
	}
}

func TestTermProgressBarFetchDurationAccumulates(t *testing.T) {
	b := &termProgressBar{}
	b.FetchDuration(5 * time.Second)
	b.FetchDuration(3 * time.Second)
	if b.fetchCount != 2 {
		t.Errorf("fetchCount = %d, want 2", b.fetchCount)
	}
	if b.lastFetchDuration != 3*time.Second {
		t.Errorf("lastFetchDuration = %v, want 3s", b.lastFetchDuration)
	}
	if b.totalFetchDuration != 8*time.Second {
		t.Errorf("totalFetchDuration = %v, want 8s", b.totalFetchDuration)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"90d", 90 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"6m", 6 * 30 * 24 * time.Hour, false},
		{"1m", 30 * 24 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * 30 * 24 * time.Hour, false}, // Nm suffix always means months
		{"0d", 0, true},
		{"0m", 0, true},
		{"-1d", 0, true},
		{"", 0, true},
		{"abc", 0, true},
		{"1h30m", 0, true},  // ends in 'm' with non-integer prefix; user likely meant minutes, but 'm' means months
		{"30d30m", 0, true}, // ends in 'm' with non-integer prefix "30d"
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseDuration(%q) unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
