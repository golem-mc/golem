package levelgen

import "math"

// OverworldSeaLevel is the overworld's sea level. vanilla: NoiseGeneratorSettings.seaLevel
const OverworldSeaLevel = 63

// PositionSeed hashes a block position into a seed. x*3129871 wraps as a
// Java int before it widens to long. vanilla: Mth.getSeed
func PositionSeed(x, y, z int32) int64 {
	seed := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	seed = seed*seed*42317861 + seed*11
	return seed >> 16
}

// floorDiv is vanilla: Math.floorDiv.
func floorDiv(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// sinTableEntry is entry i of Mth.SIN, which holds float sines.
func sinTableEntry(i int64) float32 {
	return float32(math.Sin(float64(i) / 10430.378350470453))
}

// MthSin reads vanilla's 65536-entry sine table. vanilla: Mth.sin
func MthSin(v float64) float32 {
	return sinTableEntry(int64(float64(v*10430.378350470453)) & 0xFFFF)
}

// MthCos is MthSin a quarter turn on. vanilla: Mth.cos
func MthCos(v float64) float32 {
	return sinTableEntry(int64(float64(v*10430.378350470453)+16384) & 0xFFFF)
}
