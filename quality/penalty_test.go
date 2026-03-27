package quality

import (
	"reflect"
	"testing"
)

func TestApplyPenalties_Motorway(t *testing.T) {
	collections := []RoadCollection{
		{
			Name:         "I-90",
			HighwayTypes: []string{"motorway"},
			TotalScore:   1000.0,
			TotalLength:  10000.0,
			ScorePerKm:   100.0,
		},
	}

	ApplyPenalties(collections)

	if got, want := collections[0].HighwayPenaltyFactor, 0.3; got != want {
		t.Errorf("HighwayPenaltyFactor = %v, want %v", got, want)
	}
	if got, want := collections[0].PenalizedScore, 300.0; got != want {
		t.Errorf("PenalizedScore = %v, want %v", got, want)
	}
	if got, want := collections[0].PenalizedPerKm, 30.0; got != want {
		t.Errorf("PenalizedPerKm = %v, want %v", got, want)
	}
}

func TestApplyPenalties_Mixed(t *testing.T) {
	// secondary (0.9) and trunk (0.5) — minimum is 0.5
	collections := []RoadCollection{
		{
			Name:         "Route 1",
			HighwayTypes: []string{"secondary", "trunk"},
			TotalScore:   500.0,
			TotalLength:  5000.0,
			ScorePerKm:   100.0,
		},
	}

	ApplyPenalties(collections)

	if got, want := collections[0].HighwayPenaltyFactor, 0.5; got != want {
		t.Errorf("HighwayPenaltyFactor = %v, want %v", got, want)
	}
	if got, want := collections[0].PenalizedScore, 250.0; got != want {
		t.Errorf("PenalizedScore = %v, want %v", got, want)
	}
	if got, want := collections[0].PenalizedPerKm, 50.0; got != want {
		t.Errorf("PenalizedPerKm = %v, want %v", got, want)
	}
}

func TestApplyPenalties_NoPenalty(t *testing.T) {
	collections := []RoadCollection{
		{
			Name:         "Skyline Drive",
			HighwayTypes: []string{"tertiary"},
			TotalScore:   800.0,
			TotalLength:  8000.0,
			ScorePerKm:   100.0,
		},
	}

	ApplyPenalties(collections)

	if got, want := collections[0].HighwayPenaltyFactor, 1.0; got != want {
		t.Errorf("HighwayPenaltyFactor = %v, want %v", got, want)
	}
	if got, want := collections[0].PenalizedScore, 800.0; got != want {
		t.Errorf("PenalizedScore = %v, want %v", got, want)
	}
	if got, want := collections[0].PenalizedPerKm, 100.0; got != want {
		t.Errorf("PenalizedPerKm = %v, want %v", got, want)
	}
}

func TestApplyPenalties_UnknownType(t *testing.T) {
	collections := []RoadCollection{
		{
			Name:         "Dirt Road",
			HighwayTypes: []string{"track"},
			TotalScore:   200.0,
			TotalLength:  2000.0,
			ScorePerKm:   100.0,
		},
	}

	ApplyPenalties(collections)

	if got, want := collections[0].HighwayPenaltyFactor, DefaultHighwayPenalty; got != want {
		t.Errorf("PenaltyFactor = %v, want %v (DefaultHighwayPenalty)", got, want)
	}
	if got, want := collections[0].PenalizedScore, 200.0; got != want {
		t.Errorf("PenalizedScore = %v, want %v", got, want)
	}
	if got, want := collections[0].PenalizedPerKm, 100.0; got != want {
		t.Errorf("PenalizedPerKm = %v, want %v", got, want)
	}
}

func TestApplyPenalties_SegmentScoresUnchanged(t *testing.T) {
	seg := ScoredSegment{Tier: 3, Score: 42.0, Length: 100.0}
	collections := []RoadCollection{
		{
			Name:         "Test Road",
			HighwayTypes: []string{"motorway"},
			TotalScore:   42.0,
			TotalLength:  100.0,
			ScorePerKm:   420.0,
			Segments:     []ScoredSegment{seg},
		},
	}

	ApplyPenalties(collections)

	if !reflect.DeepEqual(seg, collections[0].Segments[0]) {
		t.Errorf("segment was modified after ApplyPenalties: got %+v, want %+v", collections[0].Segments[0], seg)
	}
}

func TestPenaltyForTypes(t *testing.T) {
	tests := []struct {
		name  string
		types []string
		want  float64
	}{
		{"motorway only", []string{"motorway"}, 0.3},
		{"trunk only", []string{"trunk"}, 0.5},
		{"primary only", []string{"primary"}, 0.8},
		{"secondary only", []string{"secondary"}, 0.9},
		{"tertiary only", []string{"tertiary"}, 1.0},
		{"unclassified only", []string{"unclassified"}, 1.0},
		{"residential only", []string{"residential"}, 0.5},
		{"service only", []string{"service"}, 0.3},
		{"motorway_link only", []string{"motorway_link"}, 0.3},
		{"trunk_link only", []string{"trunk_link"}, 0.5},
		{"primary_link only", []string{"primary_link"}, 0.8},
		{"secondary_link only", []string{"secondary_link"}, 0.9},
		{"tertiary_link only", []string{"tertiary_link"}, 1.0},
		{"mixed secondary trunk", []string{"secondary", "trunk"}, 0.5},
		{"mixed tertiary motorway", []string{"tertiary", "motorway"}, 0.3},
		{"unknown type", []string{"track"}, 1.0},
		{"empty types", []string{}, 1.0},
		{"nil types", nil, 1.0},
		{"unknown mixed with secondary", []string{"living_street", "secondary"}, 0.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := penaltyForTypes(tt.types)
			if got != tt.want {
				t.Errorf("penaltyForTypes(%v) = %v, want %v", tt.types, got, tt.want)
			}
		})
	}
}
