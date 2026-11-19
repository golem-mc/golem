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

func TestFireOddsRespectWaterlogging(t *testing.T) {
	for _, tc := range []struct {
		block        Block
		ignite, burn int
	}{
		{BlockOakPlanks, 5, 20}, {BlockOakFence, 5, 20}, {BlockOakLeaves, 30, 60},
		{BlockWhiteWool, 30, 60}, {BlockHayBlock, 60, 20}, {BlockStone, 0, 0}, {BlockAir, 0, 0},
	} {
		t.Run(tc.block.Name(), func(t *testing.T) {
			state := tc.block.DefaultState()
			if got := FireIgniteOdds(state); got != tc.ignite {
				t.Errorf("ignite = %d, want %d", got, tc.ignite)
			}
			if got := FireBurnOdds(state); got != tc.burn {
				t.Errorf("burn = %d, want %d", got, tc.burn)
			}
			if wet, ok := WithBool(state, PropWaterlogged, true); ok {
				if FireIgniteOdds(wet) != 0 || FireBurnOdds(wet) != 0 {
					t.Error("waterlogged fuel still burns")
				}
			}
		})
	}
	for _, invalid := range []uint16{StateCount, 65535} {
		if FireIgniteOdds(invalid) != 0 || FireBurnOdds(invalid) != 0 {
			t.Errorf("invalid state %d burns", invalid)
		}
	}
}

func TestDamageAnvilPreservesEveryFacing(t *testing.T) {
	for _, facing := range []string{"north", "south", "east", "west"} {
		for _, tc := range []struct {
			from, to Block
			gone     bool
		}{
			{BlockAnvil, BlockChippedAnvil, false}, {BlockChippedAnvil, BlockDamagedAnvil, false},
			{BlockDamagedAnvil, BlockAir, true},
		} {
			state := MustState(tc.from.Name(), map[string]string{"facing": facing})
			want := uint16(0)
			if !tc.gone {
				want = MustState(tc.to.Name(), map[string]string{"facing": facing})
			}
			if next, gone := DamageAnvilState(state); next != want || gone != tc.gone {
				t.Errorf("%s facing %s: (%d, %v), want (%d, %v)", tc.from.Name(), facing, next, gone, want, tc.gone)
			}
		}
	}
	for _, state := range []uint16{BlockStone.DefaultState(), StateCount, 65535} {
		if next, gone := DamageAnvilState(state); next != state || gone {
			t.Errorf("non-anvil %d: (%d, %v)", state, next, gone)
		}
	}
}
