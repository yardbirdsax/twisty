package main

import (
	"testing"
	"time"
)

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
