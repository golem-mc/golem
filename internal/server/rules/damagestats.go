package rules

import "math"

// DamageStat is the amount a damage statistic counts for health lost or
// turned aside: tenths of a health point, rounded half up.
// vanilla: Math.round(damage * 10.0f)
func DamageStat(damage float64) int32 {
	return int32(math.Floor(damage*10 + 0.5))
}
