package quality_test

import (
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/quality"
)

func TestAssignTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		radius         float64
		wantTier       int
		wantWeight     float64
	}{
		{name: "tier4_below_threshold", radius: 29.9, wantTier: 4, wantWeight: quality.TierWeight4},
		{name: "tier3_at_tier4_boundary", radius: 30.0, wantTier: 3, wantWeight: quality.TierWeight3},
		{name: "tier3_below_threshold", radius: 59.9, wantTier: 3, wantWeight: quality.TierWeight3},
		{name: "tier2_at_tier3_boundary", radius: 60.0, wantTier: 2, wantWeight: quality.TierWeight2},
		{name: "tier2_below_threshold", radius: 99.9, wantTier: 2, wantWeight: quality.TierWeight2},
		{name: "tier1_at_tier2_boundary", radius: 100.0, wantTier: 1, wantWeight: quality.TierWeight1},
		{name: "tier1_below_threshold", radius: 174.9, wantTier: 1, wantWeight: quality.TierWeight1},
		{name: "tier0_at_tier1_boundary", radius: 175.0, wantTier: 0, wantWeight: quality.TierWeight0},
		{name: "tier0_large_radius", radius: 1000.0, wantTier: 0, wantWeight: quality.TierWeight0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotTier, gotWeight := quality.AssignTier(tc.radius)
			if gotTier != tc.wantTier {
				t.Errorf("AssignTier(%v): got tier %d, want %d", tc.radius, gotTier, tc.wantTier)
			}
			if gotWeight != tc.wantWeight {
				t.Errorf("AssignTier(%v): got weight %v, want %v", tc.radius, gotWeight, tc.wantWeight)
			}
		})
	}
}

func TestScoringParamsHash_Deterministic(t *testing.T) {
	t.Parallel()

	h1 := quality.ScoringParamsHash()
	h2 := quality.ScoringParamsHash()

	if h1 != h2 {
		t.Errorf("ScoringParamsHash is not deterministic: %q != %q", h1, h2)
	}
}

func TestScoringParamsHash_Prefix(t *testing.T) {
	t.Parallel()

	h := quality.ScoringParamsHash()
	if !strings.HasPrefix(h, "sha256:") {
		t.Errorf("ScoringParamsHash() = %q, want prefix %q", h, "sha256:")
	}
}
