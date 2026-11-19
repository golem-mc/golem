package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func beaconEffect(name string) int32 {
	return registry.MustMobEffect(name)
}

// TestBeaconValidateEffects walks the choices a beacon of each level may
// hold: the primary of the level alone, the secondary only a fourth level
// buys, never a primary regeneration, and the unmade choice that clears.
// vanilla: BeaconBlockEntity.validateEffects
func TestBeaconValidateEffects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		what               string
		primary, secondary int32
		levels             int
		ok                 bool
	}{
		{"speed on one level", beaconEffect("minecraft:speed"), NoBeaconEffect, 1, true},
		{"haste on one level", beaconEffect("minecraft:haste"), NoBeaconEffect, 1, true},
		{"resistance wants two", beaconEffect("minecraft:resistance"), NoBeaconEffect, 1, false},
		{"resistance on two", beaconEffect("minecraft:resistance"), NoBeaconEffect, 2, true},
		{"jump boost wants two", beaconEffect("minecraft:jump_boost"), NoBeaconEffect, 1, false},
		{"strength on three", beaconEffect("minecraft:strength"), NoBeaconEffect, 3, true},
		{"strength wants three", beaconEffect("minecraft:strength"), NoBeaconEffect, 2, false},
		{"speed with no pyramid", beaconEffect("minecraft:speed"), NoBeaconEffect, 0, false},
		{"regeneration is no primary", beaconEffect("minecraft:regeneration"), NoBeaconEffect, 4, false},
		{"regeneration second on four", beaconEffect("minecraft:speed"), beaconEffect("minecraft:regeneration"), 4, true},
		{"regeneration second wants four", beaconEffect("minecraft:speed"), beaconEffect("minecraft:regeneration"), 3, false},
		{"a secondary of a lower level", beaconEffect("minecraft:speed"), beaconEffect("minecraft:haste"), 4, false},
		{"the primary twice on four", beaconEffect("minecraft:speed"), beaconEffect("minecraft:speed"), 4, true},
		{"the primary twice before four", beaconEffect("minecraft:speed"), beaconEffect("minecraft:speed"), 1, false},
		{"the unmade choice clears", NoBeaconEffect, NoBeaconEffect, 0, true},
		{"an unoffered effect never", beaconEffect("minecraft:poison"), NoBeaconEffect, 4, false},
	} {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			got := BeaconValidateEffects(tc.primary, tc.secondary, tc.levels)
			if got != tc.ok {
				t.Errorf("BeaconValidateEffects(%d, %d, %d) = %v, want %v",
					tc.primary, tc.secondary, tc.levels, got, tc.ok)
			}
		})
	}
}

// worldMinY is the overworld's own floor, the low line the pyramid stops
// at for the stubs these tests read.
const worldMinY = int32(-64)

// TestBeaconPyramidLevelCounting counts the pyramid layers a beacon stands
// on: one per unbroken square of base blocks, nothing for a gap in the
// first, the layers above a break to the first uncounted, and the base
// materials the tag holds all counting alike.
// vanilla: BeaconBlockEntity.updateBase
func TestBeaconPyramidLevelCounting(t *testing.T) {
	t.Parallel()
	iron := registry.BlockStates["minecraft:iron_block"]
	gold := registry.BlockStates["minecraft:gold_block"]
	diamond := registry.BlockStates["minecraft:diamond_block"]
	dirt := registry.BlockStates["minecraft:dirt"]

	// A full pyramid of mixed base materials, all in the base tag.
	blocks := stubBlocks{}
	for step := int32(1); step <= 4; step++ {
		for x := -step; x <= step; x++ {
			for z := -step; z <= step; z++ {
				blocks[[3]int32{x, -step, z}] = iron
			}
		}
	}
	if got := BeaconPyramidLevel(blocks, 0, 0, 0, worldMinY); got != 4 {
		t.Errorf("a full pyramid counts %d, want 4", got)
	}
	// The other base materials count alike: still the four levels.
	blocks[[3]int32{0, -1, 0}] = gold
	blocks[[3]int32{0, -2, 0}] = diamond
	if got := BeaconPyramidLevel(blocks, 0, 0, 0, worldMinY); got != 4 {
		t.Errorf("a pyramid of mixed base materials counts %d, want 4", got)
	}

	// A block of the middle of the first layer missing undoes everything.
	delete(blocks, [3]int32{0, -1, 0})
	if got := BeaconPyramidLevel(blocks, 0, 0, 0, worldMinY); got != 0 {
		t.Errorf("a pyramid with a hole in its base counts %d, want 0", got)
	}

	// The fourth layer gone leaves three: each layer below it whole.
	blocks[[3]int32{0, -1, 0}] = iron
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			delete(blocks, [3]int32{x, -4, z})
		}
	}
	if got := BeaconPyramidLevel(blocks, 0, 0, 0, worldMinY); got != 3 {
		t.Errorf("a three-layer pyramid counts %d, want 3", got)
	}

	// A corner of the second layer out costs the layers from it up.
	blocks[[3]int32{2, -2, 2}] = dirt
	if got := BeaconPyramidLevel(blocks, 0, 0, 0, worldMinY); got != 1 {
		t.Errorf("a pyramid with a dirt corner at its second layer counts %d, want 1", got)
	}

	// Nothing but the beacon's own floor stands: no pyramid at all.
	if got := BeaconPyramidLevel(stubBlocks{}, 0, 0, 0, worldMinY); got != 0 {
		t.Errorf("bare ground counts %d, want 0", got)
	}
}

// TestBeaconBeamClear checks the column a beacon's beam climbs: whatever
// occludes light fully stops it, the glasses it passes through pick up,
// and bedrock above is the one opaque block the beam climbs past.
// vanilla: BeaconBlockEntity.tick
func TestBeaconBeamClear(t *testing.T) {
	t.Parallel()
	blocks := stubBlocks{}
	if !BeaconBeamClear(blocks, 0, 64, 0, 64) {
		t.Error("a clear column in the air does not let the beam out")
	}
	blocks[[3]int32{0, 65, 0}] = registry.BlockStates["minecraft:stone"]
	if BeaconBeamClear(blocks, 0, 64, 0, 70) {
		t.Error("a stone above the beacon did not stop the beam")
	}
	blocks[[3]int32{0, 65, 0}] = registry.BlockStates["minecraft:glass"]
	if !BeaconBeamClear(blocks, 0, 64, 0, 70) {
		t.Error("glass above the beacon stopped the beam")
	}
	blocks[[3]int32{0, 65, 0}] = registry.BlockStates["minecraft:red_stained_glass"]
	if !BeaconBeamClear(blocks, 0, 64, 0, 70) {
		t.Error("stained glass above the beacon stopped the beam")
	}
	blocks[[3]int32{0, 80, 0}] = registry.BlockStates["minecraft:bedrock"]
	if !BeaconBeamClear(blocks, 0, 64, 0, 90) {
		t.Error("bedrock above the beacon stopped the beam")
	}
	blocks[[3]int32{0, 80, 0}] = registry.BlockStates["minecraft:tinted_glass"]
	if BeaconBeamClear(blocks, 0, 64, 0, 90) {
		t.Error("tinted glass above the beacon did not stop the beam")
	}
}

// TestBeaconRange checks the reach a level applies its effects in: ten
// blocks around the beacon's own plus ten more per level.
// vanilla: BeaconBlockEntity.applyEffects (levels * 10 + 10)
func TestBeaconRange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		levels int
		want   float64
	}{
		{0, 10}, {1, 20}, {2, 30}, {3, 40}, {4, 50},
	} {
		if got := BeaconRange(tc.levels); got != tc.want {
			t.Errorf("BeaconRange(%d) = %v, want %v", tc.levels, got, tc.want)
		}
	}
}

// TestBeaconEffectDuration checks how long one cycle's application lasts:
// eleven seconds at one level, two seconds more per level above it.
// vanilla: BeaconBlockEntity.applyEffects ((9 + levels * 2) * 20)
func TestBeaconEffectDuration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		levels int
		want   int32
	}{
		{1, 220}, {2, 260}, {3, 300}, {4, 340},
	} {
		if got := BeaconEffectDuration(tc.levels); got != tc.want {
			t.Errorf("BeaconEffectDuration(%d) = %d, want %d", tc.levels, got, tc.want)
		}
	}
}

// TestBeaconEffectAmplifier checks the tier the primary applies at: one when
// a fourth level feeds it the same choice as its secondary, zero any other
// time.
// vanilla: BeaconBlockEntity.applyEffects
func TestBeaconEffectAmplifier(t *testing.T) {
	t.Parallel()
	speed, haste := beaconEffect("minecraft:speed"), beaconEffect("minecraft:haste")
	for _, tc := range []struct {
		what               string
		levels             int
		primary, secondary int32
		want               int32
	}{
		{"the same choice on four", 4, speed, speed, 1},
		{"the same choice before four", 3, speed, speed, 0},
		{"another choice on four", 4, speed, haste, 0},
		{"no secondary", 4, speed, NoBeaconEffect, 0},
		{"no primary", 4, NoBeaconEffect, speed, 0},
	} {
		if got := BeaconEffectAmplifier(tc.levels, tc.primary, tc.secondary); got != tc.want {
			t.Errorf("%s: BeaconEffectAmplifier(%d, %d, %d) = %d, want %d",
				tc.what, tc.levels, tc.primary, tc.secondary, got, tc.want)
		}
	}
}

// TestBeaconInReach checks the box a beacon's effects land in: the level's
// reach around its own block, open to any height above it and closed a
// range below it, its far side past the block's own edge.
// vanilla: BeaconBlockEntity.applyEffects
func TestBeaconInReach(t *testing.T) {
	t.Parallel()
	// One level reaches twenty blocks: the beacon at (0,64,0).
	if !BeaconInReach(20, 0.5, 64, 0.5, 0, 64, 0) {
		t.Error("the player at the beacon's side is out of its reach")
	}
	if BeaconInReach(20, 21.4, 64, 0.5, 0, 64, 0) {
		t.Error("a player twenty-one blocks out is in the reach")
	}
	if BeaconInReach(20, 0.5, 42, 0.5, 0, 64, 0) {
		t.Error("a player twenty-two blocks below is in the reach")
	}
	if !BeaconInReach(20, 0.5, 300, 0.5, 0, 64, 0) {
		t.Error("a player high above the beacon is out of its reach")
	}
	if !BeaconInReach(20, 20.4, 64, 0.5, 0, 64, 0) {
		t.Error("a player just inside the reach's far side is out of it")
	}
}
