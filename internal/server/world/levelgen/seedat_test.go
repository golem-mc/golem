package levelgen

import "testing"

// TestSeedAtMatchesAt checks a reused stream, gaussian cache included, draws
// what a fresh At stream does.
func TestSeedAtMatchesAt(t *testing.T) {
	factories := map[string]PositionalRandomFactory{
		"xoroshiro": XoroshiroPositionalRandomFactory{Lo: 0x1234567, Hi: -99887766},
		"legacy":    LegacyPositionalRandomFactory{Seed: 424242},
	}
	for name, f := range factories {
		used := f.At(5, 6, 7)
		used.NextGaussian() // leaves a cached second value
		for _, p := range [][3]int32{{1, 2, 3}, {-40, 70, 9000}, {0, 0, 0}} {
			f.SeedAt(used, p[0], p[1], p[2])
			fresh := f.At(p[0], p[1], p[2])
			for i := range 20 {
				if g, w := used.NextLong(), fresh.NextLong(); g != w {
					t.Fatalf("%s %v draw %d: got %d, want %d", name, p, i, g, w)
				}
			}
			f.SeedAt(used, p[0], p[1], p[2])
			fresh = f.At(p[0], p[1], p[2])
			if g, w := used.NextGaussian(), fresh.NextGaussian(); g != w {
				t.Fatalf("%s %v gaussian: got %v, want %v", name, p, g, w)
			}
		}
	}
}
