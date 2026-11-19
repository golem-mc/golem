package world

import "testing"

// newPerlin returns the same table for the same seed and salt, which is what
// makes two generator instances of one world interchangeable, and no two
// seeds may agree on a field. The terrains those fields build are what the
// overworld tests below hold to.
func TestPerlinIsDeterministicPerSeed(t *testing.T) {
	t.Parallel()

	same := newPerlin(42, saltForest)
	other := newPerlin(42, saltForest)
	if same.perm != other.perm {
		t.Error("two fields of one seed and salt differ")
	}

	different := newPerlin(43, saltForest)
	differs := false
	for i := range same.perm {
		if same.perm[i] != different.perm[i] {
			differs = true
			break
		}
	}
	if !differs {
		t.Error("two different seeds produced the same permutation")
	}

	// The noise itself: bounded, seed-dependent, and never the degenerate
	// zero a plain coordinate-aligned lattice point would give.
	seen := map[float64]bool{}
	for i := range 10 {
		x := float64(i)*7.31 + 0.3
		v := same.noise2(x, x*1.7)
		if v < -1.5 || v > 1.5 {
			t.Errorf("noise2(%g) = %g, outside the expected range", x, v)
		}
		if seen[v] {
			t.Error("the field repeated a value across distinct points")
		}
		seen[v] = true
		if v3 := same.noise3(x, x/2, x*0.9); v3 < -1.5 || v3 > 1.5 {
			t.Errorf("noise3(%g) = %g, outside the expected range", x, v3)
		}
	}

	// A fractal stays normalized, and two fields of the same seed but
	// different salts are independent.
	c := same.fbm2(3.25, -8.5, 1/340.0, 5)
	if c < -1 || c > 1 {
		t.Errorf("fractal = %g, outside the normalized range", c)
	}
	hills := newPerlin(42, saltSandstone)
	if hills.fbm2(3.25, -8.5, 1/340.0, 5) == c {
		t.Error("two salted fields of one seed produced the same value")
	}
}
