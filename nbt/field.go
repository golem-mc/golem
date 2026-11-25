package nbt

import "math"

// The lenient field reads: a compound's numeric field read at the width the
// caller wants, whatever width the file stored it at. A missing key, or a
// field that is no number, reads as absent. A float floors (Mth.floor) where
// a codec's read of the same tag truncates toward zero, as IntValue does.
// vanilla: CompoundTag.getXOr, TagValueInput.getXOr, NumericTag.xValue

// IntField reads a numeric field as an int: integers keep their low 32 bits,
// floats floor and clamp, NaN is 0.
func IntField(c Compound, key string) (int32, bool) {
	return IntElement(c[key])
}

// IntElement reads one tag as IntField reads a field, for a list element.
// vanilla: ListTag.getIntOr
func IntElement(tag Tag) (int32, bool) {
	switch v := tag.(type) {
	case Float:
		return clampInt(math.Floor(float64(v))), true
	case Double:
		return clampInt(math.Floor(float64(v))), true
	}
	return IntValue(tag)
}

// ShortField reads a numeric field as ShortValue narrows it.
func ShortField(c Compound, key string) (int16, bool) {
	return ShortValue(c[key])
}

// ByteField reads a numeric field as ByteValue narrows it.
func ByteField(c Compound, key string) (int8, bool) {
	return ByteValue(c[key])
}

// BoolField reads a numeric field as a boolean: its ByteValue is not nought,
// so -0.5 is true and 256 is false. A missing or non-numeric field is false.
// vanilla: CompoundTag.getBooleanOr
func BoolField(c Compound, key string) bool {
	v, ok := ByteValue(c[key])
	return ok && v != 0
}

// FloatField reads a numeric field as a float.
func FloatField(c Compound, key string) (float32, bool) {
	return FloatValue(c[key])
}

// FloatValue reads a numeric tag as a float, converting a long straight to
// float rather than through a double, which can round onto a float midpoint.
// vanilla: NumericTag.floatValue
func FloatValue(tag Tag) (float32, bool) {
	switch v := tag.(type) {
	case Byte:
		return float32(v), true
	case Short:
		return float32(v), true
	case Int:
		return float32(v), true
	case Long:
		return float32(v), true
	case Float:
		return float32(v), true
	case Double:
		return float32(v), true
	}
	return 0, false
}

// DoubleFieldOr reads a numeric field as a double, or fallback when it is
// missing or no number.
func DoubleFieldOr(c Compound, key string, fallback float64) float64 {
	switch v := c[key].(type) {
	case Byte:
		return float64(v)
	case Short:
		return float64(v)
	case Int:
		return float64(v)
	case Long:
		return float64(v)
	case Float:
		return float64(v)
	case Double:
		return float64(v)
	}
	return fallback
}
