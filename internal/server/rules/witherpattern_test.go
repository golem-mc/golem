package rules

import "testing"

// TestAWitherTAlongEitherAxisIsMatched: the skulls over the soul sand bar and
// stem match along X and along Z, and the axis is told.
func TestAWitherTAlongEitherAxisIsMatched(t *testing.T) {
	sand := defaultState(t, "minecraft:soul_sand")
	skull := defaultState(t, "minecraft:wither_skeleton_skull")
	for _, tc := range []struct {
		name      string
		cell      func(across, y int32) [3]int32
		forwardsX bool
	}{
		{"x", func(a, y int32) [3]int32 { return [3]int32{a, y, 3} }, false},
		{"z", func(a, y int32) [3]int32 { return [3]int32{2, y, a} }, true},
	} {
		w := testWorld{}
		for a := int32(1); a <= 3; a++ {
			c := tc.cell(a, 66)
			w.set(c[0], c[1], c[2], skull)
			c = tc.cell(a, 65)
			w.set(c[0], c[1], c[2], sand)
		}
		stem := tc.cell(2, 64)
		w.set(stem[0], stem[1], stem[2], sand)

		spawn, ok := WitherSpawnOf(w, 2, 66, 3)
		if tc.name == "z" {
			spawn, ok = WitherSpawnOf(w, 2, 66, 2)
		}
		if !ok {
			t.Fatalf("%s: the T was not matched", tc.name)
		}
		if spawn.Feet != stem || spawn.ForwardsX != tc.forwardsX || len(spawn.Clear) != 9 {
			t.Errorf("%s: feet %v forwards X %v over %d cells, want %v %v over 9", tc.name, spawn.Feet, spawn.ForwardsX, len(spawn.Clear), stem, tc.forwardsX)
		}
	}
}
