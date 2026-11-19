package nbt

import (
	"testing"
)

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
