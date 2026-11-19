package nbt

import (
	"math"
	"testing"
)

// TestStringifyNormalisesNegativeZero checks the float writer against the
// vanilla tag writer: a negative zero is written as the zero the SNBT reader
// keeps, while Double.toString keeps the sign.
// vanilla: StringTagVisitor.visitDouble
func TestStringifyNormalisesNegativeZero(t *testing.T) {
	negZero := math.Copysign(0, -1)
	if got := JavaFloatText(negZero, 64); got != "-0.0" {
		t.Errorf("JavaFloatText(-0.0) = %q, want -0.0", got)
	}
	if got := Stringify(Double(negZero)); got != "0.0d" {
		t.Errorf("Stringify(-0.0d) = %q, want 0.0d", got)
	}
	if got := Stringify(Float(negZero)); got != "0.0f" {
		t.Errorf("Stringify(-0.0f) = %q, want 0.0f", got)
	}
	if got := JavaFloatText(-1.5, 64); got != "-1.5" {
		t.Errorf("JavaFloatText(-1.5) = %q, want -1.5", got)
	}
}

func TestStringifyTags(t *testing.T) {
	for _, tc := range []struct {
		tag  Tag
		want string
	}{
		{Byte(1), "1b"},
		{Long(3), "3L"},
		{Float(1e7), "1.0E7f"},
		{Double(0.001), "0.001d"},
		{String(`it's`), `"it's"`},
		{Compound{"true": Int(1), "a.b": String("x")}, `{a.b:"x","true":1}`},
		{NewList([]Tag{Int(1), String("a")}), `[1,"a"]`},
		{IntArray{1, 2}, "[I;1,2]"},
	} {
		if got := Stringify(tc.tag); got != tc.want {
			t.Errorf("Stringify(%#v) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}

// vanilla: StringTagVisitor.visitByteArray
func TestStringifyByteArrayUsesSignedBytes(t *testing.T) {
	tag := ByteArray{0, 127, 128, 255}
	got := Stringify(tag)
	if got != "[B;0B,127B,-128B,-1B]" {
		t.Fatalf("Stringify = %q", got)
	}
	parsed, n, err := ParseValueTag(got)
	if err != nil || n != len(got) || !Equal(parsed, tag) {
		t.Fatalf("round trip = %v, %d, %v", parsed, n, err)
	}
}
