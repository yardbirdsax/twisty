package quality

// ScorePipelineResult holds the output of the scoring pipeline along with
// summary statistics.
type ScorePipelineResult struct {
	ScoredWays    ScoredWays
	InputWays     int // total ways before filtering
	FilteredWays  int // ways that passed the hard filter
	TotalSegments int // total segments (zero and non-zero) across scored ways
}

// RunScorePipeline executes stages 2–3 on a set of ways:
// hard filter → curvature scoring.
// Deflection filtering is applied post-aggregation (after ways are assembled
// into full roads) to give the 2400m look-ahead window cross-way-boundary
// visibility.
func RunScorePipeline(ways []Way) ScorePipelineResult {
	result := ScorePipelineResult{InputWays: len(ways)}
	if len(ways) == 0 {
		return result
	}

	filtered := HardFilter(ways)
	result.FilteredWays = len(filtered)

	scored := ScoreWays(filtered)

	totalSegments := 0
	for _, sw := range scored {
		totalSegments += len(sw.Segments)
	}
	result.TotalSegments = totalSegments
	result.ScoredWays = scored
	return result
}
