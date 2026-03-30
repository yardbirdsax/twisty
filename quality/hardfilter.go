package quality

var unpavedSurfaces = map[string]bool{
	"unpaved": true,
	"gravel":  true,
	"dirt":    true,
	"mud":     true,
	"sand":    true,
}

var nonMotorVehicleHighways = map[string]bool{
	"track":       true,
	"path":        true,
	"footway":     true,
	"cycleway":    true,
	"bridleway":   true,
	"steps":       true,
	"residential": true,
}

// isHardFiltered returns true if a way should be removed by the hard filter.
func isHardFiltered(tags map[string]string) bool {
	if surface, ok := tags["surface"]; ok && unpavedSurfaces[surface] {
		return true
	}

	switch tags["access"] {
	case "private", "no":
		return true
	}

	switch tags["motor_vehicle"] {
	case "no", "private":
		return true
	}

	if nonMotorVehicleHighways[tags["highway"]] {
		return true
	}

	return false
}

// HardFilter returns only the ways that are suitable for curvature scoring.
// Ways with unpaved surfaces, private/restricted access, or non-motor-vehicle
// highway types are removed.
func HardFilter(ways []Way) []Way {
	result := make([]Way, 0, len(ways))
	for _, w := range ways {
		if !isHardFiltered(w.Tags) {
			result = append(result, w)
		}
	}
	return result
}
