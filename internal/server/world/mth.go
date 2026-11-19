package world

import "math"

// MthSin reads vanilla's 65536-entry sine table for radians v: scaled to an
// index, wrapped, at the entries' float precision.
// vanilla: Mth.sin, Mth.SIN
func MthSin(v float64) float32 {
	return sinEntry(JavaLong(v*10430.378350470453) & 0xFFFF)
}

// MthCos is MthSin at the table's quarter-turn offset. vanilla: Mth.cos, Mth.SIN
func MthCos(v float64) float32 {
	return sinEntry(JavaLong(v*10430.378350470453+16384) & 0xFFFF)
}

// JavaLong converts a double to a long like Java's cast: NaN to 0, out-of-range
// values pinned to the nearest end, where Go's conversion is
// implementation-defined.
// vanilla: JLS 5.1.3
func JavaLong(v float64) int64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= 9223372036854775808.0:
		return math.MaxInt64
	case v <= -9223372036854775808.0:
		return math.MinInt64
	}
	return int64(v)
}

func sinEntry(i int64) float32 {
	return float32(math.Sin(float64(i) / 10430.378350470453))
}
