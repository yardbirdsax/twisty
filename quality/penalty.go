package quality

// ApplyPenalties adjusts the scores of road collections based on
// their highway types. Mutates the collections in place.
func ApplyPenalties(collections []RoadCollection) {
	for i := range collections {
		factor := penaltyForTypes(collections[i].HighwayTypes)
		collections[i].HighwayPenaltyFactor = factor
		collections[i].PenalizedScore = collections[i].TotalScore * factor
		collections[i].PenalizedPerKm = collections[i].ScorePerKm * factor
	}
}

// penaltyForTypes returns the minimum penalty multiplier across
// the given highway types.
func penaltyForTypes(types []string) float64 {
	min := DefaultHighwayPenalty
	for _, t := range types {
		p, ok := HighwayPenalty[t]
		if !ok {
			p = DefaultHighwayPenalty
		}
		if p < min {
			min = p
		}
	}
	return min
}
