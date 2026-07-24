package quality

import (
	"strconv"
	"strings"
)

type WaySpeedInfo struct {
	SpeedMPH float64
	HasSpeed bool
	LengthM  float64
}

const kmhToMPH = 0.621371

func ParseMaxspeed(raw string) (mph float64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	if numStr, found := strings.CutSuffix(raw, " km/h"); found {
		v, err := strconv.ParseFloat(numStr, 64)
		if err != nil || v <= 0 {
			return 0, false
		}
		return v * kmhToMPH, true
	}

	if numStr, found := strings.CutSuffix(raw, " mph"); found {
		v, err := strconv.ParseFloat(numStr, 64)
		if err != nil || v <= 0 {
			return 0, false
		}
		return v, true
	}

	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func SpeedPassingFraction(speeds []WaySpeedInfo, totalLengthM, minSpeedMPH float64) float64 {
	if totalLengthM <= 0 {
		return 0
	}
	var passingLength, taggedLength float64
	for _, s := range speeds {
		if s.HasSpeed {
			taggedLength += s.LengthM
			if s.SpeedMPH >= minSpeedMPH {
				passingLength += s.LengthM
			}
		}
	}
	if taggedLength <= 0 {
		return 1.0
	}
	return passingLength / taggedLength
}

func WeightedAverageSpeedMPH(speeds []WaySpeedInfo) (mph float64, ok bool) {
	var weightedSum, totalLength float64
	for _, s := range speeds {
		if s.HasSpeed {
			weightedSum += s.SpeedMPH * s.LengthM
			totalLength += s.LengthM
		}
	}
	if totalLength <= 0 {
		return 0, false
	}
	return weightedSum / totalLength, true
}
