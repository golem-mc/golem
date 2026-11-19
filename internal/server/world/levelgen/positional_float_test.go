package levelgen

import "testing"

func TestNextFloatAtMatchesAt(t *testing.T) {
	f := NewXoroshiroNamedRandom(12345, "minecraft:bedrock_floor").(XoroshiroPositionalRandomFactory)
	for x := int32(-40); x < 40; x += 7 {
		for y := int32(-64); y < 10; y += 3 {
			if got, want := f.NextFloatAt(x, y, x*3), f.At(x, y, x*3).NextFloat(); got != want {
				t.Fatalf("(%d,%d): %v, want %v", x, y, got, want)
			}
		}
	}
}
