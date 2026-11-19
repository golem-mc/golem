package nbt

import (
	"math"
	"testing"
)

// fieldCompound holds one field of every tag type under its type's name.
var fieldCompound = Compound{
	"byte":   Byte(-3),
	"short":  Short(300),
	"int":    Int(70000),
	"long":   Long(1<<32 + 5),
	"float":  Float(-1.5),
	"double": Double(2.75),
	"string": String("7"),
	"ints":   IntArray{1},
	"list":   List{Elem: TypeInt, Items: []Tag{Int(1)}},
	"nested": Compound{"x": Int(1)},
}

// TestIntFieldNarrowsEveryNumericWidth checks that a field of any numeric
// width reads as an int, and that a missing or non-numeric field reads as
// absent.
func TestIntFieldNarrowsEveryNumericWidth(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]int32{
		"byte": -3, "short": 300, "int": 70000, "long": 5, "float": -2, "double": 2,
	} {
		if got, ok := IntField(fieldCompound, key); !ok || got != want {
			t.Errorf("IntField(%q) = %d/%v, want %d/true", key, got, ok, want)
		}
	}
	for _, key := range []string{"missing", "string", "ints", "list", "nested"} {
		if got, ok := IntField(fieldCompound, key); ok || got != 0 {
			t.Errorf("IntField(%q) = %d/%v, want 0/false", key, got, ok)
		}
	}
	if got, ok := IntField(nil, "int"); ok || got != 0 {
		t.Errorf("IntField on a nil compound = %d/%v, want 0/false", got, ok)
	}
}

// TestIntFieldFloorsWhereIntValueTruncates checks that a float field floors
// and clamps, and NaN is 0, where IntValue, a codec's read, truncates.
func TestIntFieldFloorsWhereIntValueTruncates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag       Tag
		field, iv int32
	}{
		{Float(-1.5), -2, -1},
		{Double(-0.5), -1, 0},
		{Float(1.5), 1, 1},
		{Double(-1e12), math.MinInt32, math.MinInt32},
		{Double(1e12), math.MaxInt32, math.MaxInt32},
		{Float(float32(math.NaN())), 0, 0},
	} {
		if got, ok := IntField(Compound{"v": tc.tag}, "v"); !ok || got != tc.field {
			t.Errorf("IntField(%v) = %d/%v, want %d/true", tc.tag, got, ok, tc.field)
		}
		if got, _ := IntValue(tc.tag); got != tc.iv {
			t.Errorf("IntValue(%v) = %d, want %d", tc.tag, got, tc.iv)
		}
	}
}

// TestShortAndByteFieldTakeTheLowBitsOfTheFlooredInt checks that the narrower
// reads keep the low bits of IntField, a float flooring first.
func TestShortAndByteFieldTakeTheLowBitsOfTheFlooredInt(t *testing.T) {
	t.Parallel()
	c := Compound{"wide": Int(0x12345), "neg": Float(-1.5), "big": Double(1e12)}
	if got, ok := ShortField(c, "wide"); !ok || got != 0x2345 {
		t.Errorf("ShortField(wide) = %#x/%v, want 0x2345/true", got, ok)
	}
	if got, ok := ByteField(c, "wide"); !ok || got != 0x45 {
		t.Errorf("ByteField(wide) = %#x/%v, want 0x45/true", got, ok)
	}
	if got, ok := ShortField(c, "neg"); !ok || got != -2 {
		t.Errorf("ShortField(-1.5) = %d/%v, want -2/true", got, ok)
	}
	if got, ok := ByteField(c, "neg"); !ok || got != -2 {
		t.Errorf("ByteField(-1.5) = %d/%v, want -2/true", got, ok)
	}
	// 1e12 clamps to MaxInt32 first, whose low bits are all ones.
	if got, ok := ShortField(c, "big"); !ok || got != -1 {
		t.Errorf("ShortField(1e12) = %d/%v, want -1/true", got, ok)
	}
	if got, ok := ByteField(c, "big"); !ok || got != -1 {
		t.Errorf("ByteField(1e12) = %d/%v, want -1/true", got, ok)
	}
	for _, key := range []string{"missing", "string"} {
		if got, ok := ShortField(fieldCompound, key); ok || got != 0 {
			t.Errorf("ShortField(%q) = %d/%v, want 0/false", key, got, ok)
		}
		if got, ok := ByteField(fieldCompound, key); ok || got != 0 {
			t.Errorf("ByteField(%q) = %d/%v, want 0/false", key, got, ok)
		}
	}
}

// TestBoolFieldReadsTheLowByte checks that any numeric width counts, that only
// the low byte of the floored int decides, and that a missing or non-numeric
// field is false.
func TestBoolFieldReadsTheLowByte(t *testing.T) {
	t.Parallel()
	c := Compound{
		"one": Byte(1), "zero": Byte(0), "short": Short(2), "long": Long(1 << 40),
		"lowZero": Int(0x100), "float": Float(0.5), "negHalf": Float(-0.5),
		"double": Double(1.0), "string": String("true"),
	}
	for key, want := range map[string]bool{
		"one": true, "zero": false, "short": true, "long": false, "lowZero": false,
		"float": false, "negHalf": true, "double": true, "string": false, "missing": false,
	} {
		if got := BoolField(c, key); got != want {
			t.Errorf("BoolField(%q) = %v, want %v", key, got, want)
		}
	}
}

// TestFloatFieldReadsEveryNumericWidth checks the float read of each width and
// that a missing or non-numeric field reads as absent.
func TestFloatFieldReadsEveryNumericWidth(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]float32{
		"byte": -3, "short": 300, "int": 70000, "long": 1<<32 + 5, "float": -1.5, "double": 2.75,
	} {
		if got, ok := FloatField(fieldCompound, key); !ok || got != want {
			t.Errorf("FloatField(%q) = %v/%v, want %v/true", key, got, ok, want)
		}
	}
	for _, key := range []string{"missing", "string", "ints", "list", "nested"} {
		if got, ok := FloatField(fieldCompound, key); ok || got != 0 {
			t.Errorf("FloatField(%q) = %v/%v, want 0/false", key, got, ok)
		}
	}
	if got, ok := FloatField(Compound{"nan": Double(math.NaN())}, "nan"); !ok || !math.IsNaN(float64(got)) {
		t.Errorf("FloatField(NaN) = %v/%v, want NaN/true", got, ok)
	}
}

// TestDoubleFieldOrFallsBackOnlyWhenNoNumber checks the double read of each
// width, and that the fallback answers only a missing or non-numeric field.
func TestDoubleFieldOrFallsBackOnlyWhenNoNumber(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]float64{
		"byte": -3, "short": 300, "int": 70000, "long": 1<<32 + 5, "float": -1.5, "double": 2.75,
	} {
		if got := DoubleFieldOr(fieldCompound, key, 99); got != want {
			t.Errorf("DoubleFieldOr(%q) = %v, want %v", key, got, want)
		}
	}
	for _, key := range []string{"missing", "string", "ints", "list", "nested"} {
		if got := DoubleFieldOr(fieldCompound, key, 99); got != 99 {
			t.Errorf("DoubleFieldOr(%q) = %v, want the fallback 99", key, got)
		}
	}
	if got := DoubleFieldOr(Compound{"zero": Byte(0)}, "zero", 99); got != 0 {
		t.Errorf("DoubleFieldOr(zero) = %v, want 0, not the fallback", got)
	}
}

// TestIntElementFloorsAFraction checks that a list element reads as IntField
// does, flooring where IntValue truncates.
func TestIntElementFloorsAFraction(t *testing.T) {
	if got, ok := IntElement(Double(-1.5)); !ok || got != -2 {
		t.Errorf("IntElement(-1.5) = %d, %v; want -2", got, ok)
	}
	if _, ok := IntElement(String("x")); ok {
		t.Error("IntElement of a string reported a number")
	}
}
