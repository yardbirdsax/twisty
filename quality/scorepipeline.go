package quality

// ScorePipelineResult holds the output of the scoring pipeline along with
// summary statistics.
type ScorePipelineResult struct {
	ScoredWays         []ScoredWay
	InputWays          int // total ways before filtering
	FilteredWays       int // ways that passed the hard filter
	TotalSegments      int // total segments (zero and non-zero) across scored ways
	ZeroedByDeflection int // segments zeroed by deflection filter
}

// RunScorePipeline executes stages 2–4 on a set of ways:
// hard filter → curvature scoring → deflection filter.
func RunScorePipeline(ways []Way) ScorePipelineResult {
	result := ScorePipelineResult{
		InputWays: len(ways),
	}

	if len(ways) == 0 {
		return result
	}

	// Stage 1: hard filter.
	filtered := HardFilter(ways)
	result.FilteredWays = len(filtered)

	// Stage 2: curvature scoring.
	scored := ScoreWays(filtered)

	// Count total non-zero segments before deflection filter.
	totalSegments := 0
	nonZeroBefore := 0
	for _, sw := range scored {
		totalSegments += len(sw.Segments)
		for _, seg := range sw.Segments {
			if seg.Score != 0 {
				nonZeroBefore++
			}
		}
	}
	result.TotalSegments = totalSegments

	// Stage 3: deflection filter (modifies scored in place).
	ApplyDeflectionFilter(scored)

	// Count non-zero segments after deflection filter.
	nonZeroAfter := 0
	for _, sw := range scored {
		for _, seg := range sw.Segments {
			if seg.Score != 0 {
				nonZeroAfter++
			}
		}
	}
	result.ZeroedByDeflection = nonZeroBefore - nonZeroAfter

	result.ScoredWays = scored
	return result
}
