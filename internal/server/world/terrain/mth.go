package terrain

import "math"

// floorInt is Java's (int)Math.floor: NaN to 0 and out-of-range values pinned
// to the int range. vanilla: Mth.floor
func floorInt(v float64) int32 {
	f := math.Floor(v)
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	return int32(f)
}

// smoothstep is 6x^5 - 15x^4 + 10x^3 in vanilla's evaluation order. vanilla: Mth.smoothstep
func smoothstep(x float32) float32 {
	return x * x * x * (float32(x*(float32(x*6.0)-15.0)) + 10.0)
}

// lerp is vanilla: Mth.lerp(float, float, float).
func lerp(alpha, p0, p1 float32) float32 { return p0 + float32(alpha*(p1-p0)) }

// lerp2 is vanilla: Mth.lerp2.
func lerp2(a1, a2, x00, x10, x01, x11 float32) float32 {
	return lerp(a2, lerp(a1, x00, x10), lerp(a1, x01, x11))
}

// lerp3 is vanilla: Mth.lerp3.
func lerp3(a1, a2, a3, x000, x100, x010, x110, x001, x101, x011, x111 float32) float32 {
	return lerp(a3, lerp2(a1, a2, x000, x100, x010, x110), lerp2(a1, a2, x001, x101, x011, x111))
}

// clampF is vanilla: Mth.clamp(float, float, float).
func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	return min(v, hi)
}

// sqrtF is vanilla: Mth.sqrt(float).
func sqrtF(x float32) float32 { return float32(math.Sqrt(float64(x))) }

// floatBits is Float.floatToRawIntBits.
func floatBits(v float32) uint32 { return math.Float32bits(v) }
