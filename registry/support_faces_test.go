package registry

import "testing"

func TestCollisionFaceFull(t *testing.T) {
	for _, tc := range []struct {
		state uint16
		mask  uint8
	}{
		{BlockStone.DefaultState(), 63},
		{BlockAir.DefaultState(), 0},
		{BlockOakLeaves.DefaultState(), 63},
		{BlockStoneSlab.DefaultState(), 1},
		{MustState("minecraft:stone_brick_stairs", map[string]string{"facing": "north", "half": "bottom", "shape": "straight"}), 1 | 1<<2},
	} {
		var mask uint8
		for dir := range 6 {
			if CollisionFaceFull(tc.state, dir) {
				mask |= 1 << dir
			}
		}
		if mask != tc.mask {
			t.Errorf("%s %v: faces %06b, want %06b", BlockOf(tc.state).Name(), StateProps(tc.state), mask, tc.mask)
		}
	}
}
