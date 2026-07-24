package quality

import (
	"math"
	"testing"
)

func TestParseMaxspeed(t *testing.T) {
	tests := []struct {
		raw    string
		wantV  float64
		wantOK bool
		approx bool // use approximate comparison for km/h conversion
	}{
		{"55", 55, true, false},
		{"55 mph", 55, true, false},
		{"90 km/h", 55.923, true, true},
		{"30", 30, true, false},
		{"", 0, false, false},
		{"none", 0, false, false},
		{"walk", 0, false, false},
		{"30;50", 0, false, false},
		{"signals", 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, ok := ParseMaxspeed(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ParseMaxspeed(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}
			if tt.approx {
				if math.Abs(got-tt.wantV) > 0.1 {
					t.Fatalf("ParseMaxspeed(%q) = %.3f, want ~%.3f", tt.raw, got, tt.wantV)
				}
			} else if got != tt.wantV {
				t.Fatalf("ParseMaxspeed(%q) = %v, want %v", tt.raw, got, tt.wantV)
			}
		})
	}
}

func TestSpeedPassingFraction(t *testing.T) {
	tests := []struct {
		name     string
		speeds   []WaySpeedInfo
		totalLen float64
		minSpeed float64
		want     float64
	}{
		{
			name: "all ways pass",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 65, HasSpeed: true, LengthM: 2000},
			},
			totalLen: 3000,
			minSpeed: 50,
			want:     1.0,
		},
		{
			name: "all ways fail",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 25, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 30, HasSpeed: true, LengthM: 2000},
			},
			totalLen: 3000,
			minSpeed: 50,
			want:     0.0,
		},
		{
			name: "majority passes",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 2000},
				{SpeedMPH: 25, HasSpeed: true, LengthM: 1000},
			},
			totalLen: 3000,
			minSpeed: 50,
			want:     2000.0 / 3000.0,
		},
		{
			name: "no speed data - passes through",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 0, HasSpeed: false, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 2000},
			},
			totalLen: 3000,
			minSpeed: 50,
			want:     1.0,
		},
		{
			name: "partial data - tagged ways pass",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 5000},
			},
			totalLen: 6000,
			minSpeed: 50,
			want:     1.0,
		},
		{
			name: "partial data - tagged ways fail",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 25, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 5000},
			},
			totalLen: 6000,
			minSpeed: 50,
			want:     0.0,
		},
		{
			name: "partial data - mixed tagged ways",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 2000},
				{SpeedMPH: 25, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 4000},
			},
			totalLen: 7000,
			minSpeed: 50,
			want:     2000.0 / 3000.0,
		},
		{
			name:     "empty slice",
			speeds:   nil,
			totalLen: 0,
			minSpeed: 50,
			want:     0.0,
		},
		{
			name: "zero total length",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 0},
			},
			totalLen: 0,
			minSpeed: 50,
			want:     0.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SpeedPassingFraction(tt.speeds, tt.totalLen, tt.minSpeed)
			if math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("SpeedPassingFraction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWeightedAverageSpeedMPH(t *testing.T) {
	tests := []struct {
		name    string
		speeds  []WaySpeedInfo
		wantMPH float64
		wantOK  bool
	}{
		{
			name:    "empty slice",
			speeds:  nil,
			wantMPH: 0,
			wantOK:  false,
		},
		{
			name: "all untagged",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 0, HasSpeed: false, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 2000},
			},
			wantMPH: 0,
			wantOK:  false,
		},
		{
			name: "single tagged entry",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 1000},
			},
			wantMPH: 55,
			wantOK:  true,
		},
		{
			name: "two equal-length tagged entries",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 40, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 60, HasSpeed: true, LengthM: 1000},
			},
			wantMPH: 50,
			wantOK:  true,
		},
		{
			name: "length-weighted: longer segment dominates",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 25, HasSpeed: true, LengthM: 500},
				{SpeedMPH: 55, HasSpeed: true, LengthM: 4500},
			},
			// (25*500 + 55*4500) / 5000 = (12500 + 247500) / 5000 = 260000/5000 = 52
			wantMPH: 52,
			wantOK:  true,
		},
		{
			name: "untagged entries are ignored",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 9000},
			},
			wantMPH: 55,
			wantOK:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := WeightedAverageSpeedMPH(tt.speeds)
			if ok != tt.wantOK {
				t.Fatalf("WeightedAverageSpeedMPH() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && math.Abs(got-tt.wantMPH) > 0.01 {
				t.Fatalf("WeightedAverageSpeedMPH() = %.4f, want %.4f", got, tt.wantMPH)
			}
		})
	}
}
