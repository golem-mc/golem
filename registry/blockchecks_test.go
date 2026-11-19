package registry

import "testing"

func TestFallingAndBurningBlockChecks(t *testing.T) {
	for _, tc := range []struct {
		block         Block
		free, burning bool
	}{
		{BlockAir, true, false}, {BlockCaveAir, true, false}, {BlockVoidAir, true, false},
		{BlockFire, true, true}, {BlockSoulFire, true, true}, {BlockWater, true, false},
		{BlockLava, true, true}, {BlockShortGrass, true, false}, {BlockSeagrass, true, false},
		{BlockKelp, false, false}, {BlockStone, false, false}, {BlockOakFence, false, false},
		{BlockMagmaBlock, false, true},
	} {
		t.Run(tc.block.Name(), func(t *testing.T) {
			if got := tc.block.CanFallThrough(); got != tc.free {
				t.Errorf("CanFallThrough = %v, want %v", got, tc.free)
			}
			if got := CanFallThrough(tc.block.DefaultState()); got != tc.free {
				t.Errorf("state CanFallThrough = %v, want %v", got, tc.free)
			}
			if got := IsBurningBlock(tc.block.DefaultState()); got != tc.burning {
				t.Errorf("IsBurningBlock = %v, want %v", got, tc.burning)
			}
		})
	}
	for _, tc := range []struct {
		state   uint16
		burning bool
	}{
		{MustState("minecraft:campfire", map[string]string{"lit": "true"}), true},
		{MustState("minecraft:soul_campfire", map[string]string{"lit": "true"}), true},
		{MustState("minecraft:campfire", map[string]string{"lit": "false"}), false},
		{BlockLavaCauldron.DefaultState(), true},
		{BlockCauldron.DefaultState(), false},
		{StateCount, false},
	} {
		if got := IsBurningBlock(tc.state); got != tc.burning {
			t.Errorf("IsBurningBlock(state %d) = %v, want %v", tc.state, got, tc.burning)
		}
	}
	wet := MustState("minecraft:oak_fence", map[string]string{"waterlogged": "true"})
	if CanFallThrough(wet) {
		t.Error("waterlogged fence let a falling block through")
	}
	for _, invalid := range []uint16{StateCount, 65535} {
		if CanFallThrough(invalid) {
			t.Errorf("invalid state %d let a falling block through", invalid)
		}
	}
}
