package levelgen

import "testing"

// TestLegacyRandomDecorationSeed pins the decoration seed and the stream it
// leaves against java.util.Random. With
// deco(s, x, z) = { Random r = new Random(s); long a = r.nextLong() | 1L, b = r.nextLong() | 1L; return (long)x*a + (long)z*b ^ s; }
// vanilla: WorldgenRandom.setDecorationSeed
func TestLegacyRandomDecorationSeed(t *testing.T) {
	for _, c := range []struct {
		seed           int64
		blockX, blockZ int32
		want           int64
	}{
		{0, 0, 0, 0},                          // deco(0, 0, 0)
		{0, 16, -32, -31007559772603472},      // deco(0, 16, -32)
		{12345, -48, 80, 5034657768149126297}, // deco(12345, -48, 80)
		{0, -32, -48, 1167361632575206672},    // deco(0, -32, -48)
	} {
		r := NewLegacyRandom(99)
		if got := r.SetDecorationSeed(c.seed, c.blockX, c.blockZ); got != c.want {
			t.Errorf("SetDecorationSeed(%d, %d, %d) = %d, want %d", c.seed, c.blockX, c.blockZ, got, c.want)
		}
	}
	// new Random(deco(12345, -48, 80)).nextInt(), twice
	r := NewLegacyRandom(0)
	r.SetDecorationSeed(12345, -48, 80)
	if a, b := r.NextInt(), r.NextInt(); a != -1625075488 || b != -1111594941 {
		t.Errorf("the reseeded stream draws %d, %d, want -1625075488, -1111594941", a, b)
	}
}
