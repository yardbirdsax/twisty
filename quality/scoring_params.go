// Package quality provides road-quality scoring and filtering for the twisty
// pipeline. This file is the single source of truth for all scoring and
// filtering parameters. Every other package that needs tier thresholds, tier
// weights, or deflection-filter settings must import these constants rather
// than re-defining them locally.
package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Tier radius thresholds (metres). A segment's circumradius must be strictly
// less than the threshold to qualify for that tier. Thresholds are checked
// from tightest to widest so that each tier represents a non-overlapping band.
const (
	// TierRadius4 is the upper bound (exclusive) for the tightest curve tier.
	// Segments with circumradius < 30 m are assigned tier 4.
	TierRadius4 = 30.0

	// TierRadius3 is the upper bound (exclusive) for tier 3 curves.
	// Segments with circumradius in [30, 60) m are assigned tier 3.
	TierRadius3 = 60.0

	// TierRadius2 is the upper bound (exclusive) for tier 2 curves.
	// Segments with circumradius in [60, 100) m are assigned tier 2.
	TierRadius2 = 100.0

	// TierRadius1 is the upper bound (exclusive) for tier 1 curves.
	// Segments with circumradius in [100, 175) m are assigned tier 1.
	// Segments with circumradius >= 175 m are assigned tier 0 (straight).
	TierRadius1 = 175.0
)

// Tier weights are multiplied by a segment's length contribution when
// computing the road's overall curvature score. Higher tiers carry more weight
// because tighter curves are more characteristically "twisty".
const (
	// TierWeight4 is the score multiplier for tier-4 (tightest) segments.
	TierWeight4 = 2.0

	// TierWeight3 is the score multiplier for tier-3 segments.
	TierWeight3 = 1.6

	// TierWeight2 is the score multiplier for tier-2 segments.
	TierWeight2 = 1.3

	// TierWeight1 is the score multiplier for tier-1 (gentlest scoring) segments.
	TierWeight1 = 1.0

	// TierWeight0 is the score multiplier for tier-0 (straight) segments.
	// Straight segments contribute nothing to the curvature score.
	TierWeight0 = 0.0
)

// ConnectedEndpointProximityM is the maximum distance (in meters) between the
// endpoints of two ways for them to be considered connected (part of the same
// geographic road segment).
const ConnectedEndpointProximityM = 100.0

// StraightGapSplitM is the minimum accumulated length (in meters) of
// contiguous tier-0 (straight) segments that triggers a split of a road
// collection into two separate collections. 2,414 m equals 1.5 miles,
// matching the threshold used by the Curvature project.
const StraightGapSplitM = 2414.0

// MinRoadLengthM is the minimum total length (in meters) for a road collection
// to be included in output. Collections shorter than this are filtered out.
// 4,828 m equals 3 miles.
const MinRoadLengthM = 4828.0

// Deflection filter constants are derived from the Curvature project
// (github.com/awebre/curvature). A road section is only considered "twisty"
// if it accumulates a meaningful heading change within a look-ahead window.
const (
	// DeflectionLookAheadM is the sliding window length (metres) used when
	// computing cumulative heading change along a road. 2.4 km matches the
	// value used by the Curvature project.
	DeflectionLookAheadM = 2400.0

	// DeflectionMinHeadingChange is the minimum total heading change (degrees)
	// that must occur within a DeflectionLookAheadM window for a road section
	// to pass the deflection filter. 20° matches the Curvature project default.
	DeflectionMinHeadingChange = 20.0
)

// AssignTier maps a circumradius (in metres) to its curvature tier and the
// associated score weight. Tiers are checked from tightest to widest; the
// first threshold the radius falls below determines the tier.
//
//	radius < TierRadius4 (30 m)   → tier 4, TierWeight4
//	radius < TierRadius3 (60 m)   → tier 3, TierWeight3
//	radius < TierRadius2 (100 m)  → tier 2, TierWeight2
//	radius < TierRadius1 (175 m)  → tier 1, TierWeight1
//	radius >= TierRadius1         → tier 0, TierWeight0
func AssignTier(radius float64) (tier int, weight float64) {
	switch {
	case radius < TierRadius4:
		return 4, TierWeight4
	case radius < TierRadius3:
		return 3, TierWeight3
	case radius < TierRadius2:
		return 2, TierWeight2
	case radius < TierRadius1:
		return 1, TierWeight1
	default:
		return 0, TierWeight0
	}
}

// HighwayPenalty maps highway types to score penalty multipliers.
// A multiplier of 1.0 means no penalty.
var HighwayPenalty = map[string]float64{
	"motorway":       0.3,
	"motorway_link":  0.3,
	"trunk":          0.5,
	"trunk_link":     0.5,
	"primary":        0.8,
	"primary_link":   0.8,
	"secondary":      0.9,
	"secondary_link": 0.9,
	"tertiary":       1.0,
	"tertiary_link":  1.0,
	"unclassified":   1.0,
	"residential":    1.0,
	"service":        1.0,
}

// DefaultHighwayPenalty is used for highway types not in the HighwayPenalty map.
const DefaultHighwayPenalty = 1.0

// ScoringParamsHash returns a SHA-256 hash of all scoring constants, encoded
// as a hex string prefixed with "sha256:". The hash is deterministic across
// calls and across process restarts. It is used by the score cache (Task 007)
// to detect when parameters have changed and cached scores must be
// invalidated.
//
// NOTE: HighwayPenalty multipliers are intentionally excluded from this hash.
// Penalties are applied post-cache at the aggregation level (stage 6), not
// during per-way scoring (stage 3). Including them would cause unnecessary
// cache invalidation when only penalty values change.
//
// NOTE: The cache format changed in Task 001 (added Tags field to ScoredWay).
// Existing cache entries without tags will silently omit tags on read.
// Users upgrading from a pre-Task-001 cache should run `twisty score --clear-cache`
// or manually delete the score cache directory.
func ScoringParamsHash() string {
	input := fmt.Sprintf(
		"TierRadius4=%v,TierRadius3=%v,TierRadius2=%v,TierRadius1=%v,"+
			"TierWeight4=%v,TierWeight3=%v,TierWeight2=%v,TierWeight1=%v,TierWeight0=%v,"+
			"DeflectionLookAheadM=%v,DeflectionMinHeadingChange=%v,"+
			"ConnectedEndpointProximityM=%v,StraightGapSplitM=%v",
		TierRadius4, TierRadius3, TierRadius2, TierRadius1,
		TierWeight4, TierWeight3, TierWeight2, TierWeight1, TierWeight0,
		DeflectionLookAheadM, DeflectionMinHeadingChange,
		ConnectedEndpointProximityM, StraightGapSplitM,
	)
	sum := sha256.Sum256([]byte(input))
	return "sha256:" + hex.EncodeToString(sum[:])
}
