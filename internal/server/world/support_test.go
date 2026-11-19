package world

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestTagReadsFollowPackBlockTags checks the reads a data pack's block tags
// reach after startup: a support tag's neighbour bit, the living corals, the
// blocks a fluid washes away.
func TestTagReadsFollowPackBlockTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	stone := registry.MustState("minecraft:stone", nil)
	torch := registry.MustState("minecraft:torch", nil)
	if hasBit(stone, tagVegetation) || isLivingCoral(registry.BlockStone) || canHoldAny(stone) || !canHoldAny(torch) {
		t.Fatal("stone or the torch is in a tag before any pack")
	}
	entry := func(id string) registry.PackTag {
		return registry.PackTag{Entries: []registry.PackTagEntry{{ID: id, Required: true}}}
	}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:supports_vegetation":   entry("minecraft:stone"),
		"minecraft:corals":                entry("minecraft:stone"),
		"minecraft:washed_away_by_fluids": entry("minecraft:stone"),
	}})
	if !hasBit(stone, tagVegetation) {
		t.Error("stone carries no vegetation bit after a pack added it to supports_vegetation")
	}
	if !isLivingCoral(registry.BlockStone) {
		t.Error("stone is no living coral after a pack added it to corals")
	}
	if !canHoldAny(stone) {
		t.Error("stone holds no fluid after a pack added it to washed_away_by_fluids")
	}

	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:washed_away_by_fluids": {Replace: true, Entries: []registry.PackTagEntry{
			{ID: "minecraft:oak_stairs", Required: true},
		}},
	}})
	if canHoldAny(torch) {
		t.Error("a torch still holds fluid after a replacement removed it from washed_away_by_fluids")
	}
	if !canHoldAny(registry.BlockOakStairs.DefaultState()) {
		t.Error("washed_away_by_fluids overrides a waterloggable container")
	}
	registry.ApplyPackTags(nil)
	if !canHoldAny(torch) {
		t.Error("torch rejects fluid after restoring built-in tags")
	}
}

// TestCanSurviveMatchesVanilla sets each block on its support in an empty
// column and asks canSurvive, the answers being the vanilla ones.
func TestCanSurviveMatchesVanilla(t *testing.T) {
	w, _ := newPhysicsWorld(t)
	state := func(s string) uint16 {
		t.Helper()
		name, props := s, map[string]string{}
		if i := strings.IndexByte(s, '['); i >= 0 {
			name = s[:i]
			props = registry.ParseStateProps(s[i+1 : len(s)-1])
		}
		id, ok := registry.FindState("minecraft:"+name, props)
		if !ok {
			t.Fatalf("no state %s", s)
		}
		return id
	}
	for i, tc := range []struct {
		below, block, above string
		want                bool
	}{
		{"oak_fence", "torch", "", true},
		{"oak_fence", "lantern", "", true},
		{"", "lantern[hanging=true]", "oak_fence", true},
		{"", "lantern[hanging=true]", "oak_fence_gate", false}, // unstable_bottom_center
		{"", "oak_hanging_sign", "oak_fence", true},
		{"oak_fence", "oak_sign", "", true},
		{"oak_fence", "stone_pressure_plate", "", true},
		{"oak_fence", "rail", "", false},
		{"hopper", "rail", "", true},
		{"hopper", "redstone_wire", "", true},
		{"hopper", "repeater", "", true},
		{"stone_slab", "rail", "", false},
		{"stone_slab[type=top]", "rail", "", true},
		{"ice", "snow", "", false},
		{"packed_ice", "snow", "", false},
		{"oak_fence", "snow", "", false},
		{"stone_slab", "snow", "", false},
		{"soul_sand", "snow", "", true},
		{"snow[layers=8]", "snow", "", true},
		{"snow[layers=7]", "snow", "", false},
		{"dirt", "brown_mushroom", "", false}, // daylight
		{"mycelium", "brown_mushroom", "", true},
		{"sand", "bamboo", "", true},
		{"stone", "leaf_litter", "", true},
		{"soul_soil", "crimson_roots", "", true},
		{"mycelium", "crimson_fungus", "", true},
		{"soul_soil", "nether_sprouts", "", true},
		{"clay", "azalea", "", true},
		{"stone", "cactus_flower", "", true},
		{"stone", "poppy", "", false},
		{"dirt", "poppy", "", true},
		{"stone", "white_carpet", "", true},
		{"", "white_carpet", "", false},
		{"", "pale_moss_carpet", "", false},
		{"stone", "big_dripleaf_stem", "", false},
		{"clay", "big_dripleaf_stem", "big_dripleaf", true},
		{"", "pointed_dripstone[vertical_direction=down]", "pointed_dripstone[vertical_direction=down]", true},
		{"", "pointed_dripstone[vertical_direction=down]", "pointed_dripstone[vertical_direction=up]", false},
		{"", "vine[north=true]", "vine[north=true]", true},
		{"", "vine[south=true]", "vine[north=true]", false},
		{"", "spore_blossom", "stone", true},
		{"", "hanging_roots", "oak_fence", false},
		{"", "weeping_vines", "weeping_vines_plant", true},
		{"", "grindstone[face=floor]", "", true},
		{"", "end_rod", "", true},
		{"", "skeleton_skull", "", true},
		{"", "tripwire", "", true},
		{"", "candle", "", false},
		{"stone", "oak_door[half=lower]", "", true},
		{"", "oak_door[half=lower]", "", false},
		{"oak_door[half=upper]", "oak_door[half=lower]", "", false},
		{"oak_door[half=lower]", "oak_door[half=upper]", "", true},
		{"stone", "bell[attachment=floor,facing=north]", "", true},
		{"", "bell[attachment=floor,facing=north]", "", false},
		{"", "bell[attachment=ceiling,facing=north]", "stone", true},
		{"", "bell[attachment=ceiling,facing=north]", "", false},
		{"pitcher_crop[half=lower,age=1]", "pitcher_crop[half=upper,age=1]", "", true},
		{"stone", "cake", "", true},
		{"", "cake", "", false},
		{"stone", "sea_pickle", "", true},
		{"stone_slab[type=top]", "sea_pickle", "", true},
		{"oak_fence", "sea_pickle", "", true}, // collision reaches the cell top
		{"stone_slab[type=bottom]", "sea_pickle", "", false},
		{"", "sea_pickle", "", false},
		{"stone", "brain_coral", "", true},
		{"", "brain_coral", "", false},
		{"", "dead_brain_coral_fan", "", false},
		{"stone", "brain_coral_fan", "", true},
		{"stone", "seagrass", "", true},
		{"", "seagrass", "", false},
		{"magma_block", "seagrass", "", false},
		{"stone", "kelp", "", true},
		{"", "kelp", "", false},
		{"magma_block", "kelp", "", false},
		{"kelp_plant", "kelp", "", true},
		{"kelp", "kelp_plant", "", true},
		{"", "mangrove_propagule", "", false},
		{"dirt", "mangrove_propagule", "", true},
		{"stone", "mangrove_propagule", "", false},
		{"", "mangrove_propagule[hanging=true]", "mangrove_leaves", true},
		{"", "mangrove_propagule[hanging=true]", "", false},
	} {
		x, z := int32(i%16)*2, int32(i/16)*2
		w.SetBlock(x, GroundLevel-1, z, 0)
		if tc.below != "" {
			w.SetBlock(x, GroundLevel-1, z, state(tc.below))
		}
		if tc.above != "" {
			w.SetBlock(x, GroundLevel+1, z, state(tc.above))
		}
		if got := w.Survives(x, GroundLevel, z, state(tc.block)); got != tc.want {
			t.Errorf("%s under %q over %q survives = %v, want %v", tc.block, tc.above, tc.below, got, tc.want)
		}
	}
}

// TestAscendingRailNeedsItsSlope removes the block an ascending rail climbs.
func TestAscendingRailNeedsItsSlope(t *testing.T) {
	w, _ := newPhysicsWorld(t)
	rail, _ := registry.FindState("minecraft:rail", map[string]string{"shape": "ascending_east"})
	w.SetBlock(1, GroundLevel, 0, registry.BlockStates["minecraft:stone"])
	if !w.Survives(0, GroundLevel, 0, rail) {
		t.Error("ascending rail beside its slope block did not survive")
	}
	w.SetBlock(1, GroundLevel, 0, 0)
	if w.Survives(0, GroundLevel, 0, rail) {
		t.Error("ascending rail without its slope block survived")
	}
}

// TestWaterAndSeabedPlantsPopWithTheirSupport stands each block on, or
// against, a stone, takes the stone away and ticks: the block goes and
// drops.
func TestWaterAndSeabedPlantsPopWithTheirSupport(t *testing.T) {
	w, p := newPhysicsWorld(t)
	stone := registry.BlockStates["minecraft:stone"]
	var clock int64
	for i, tc := range []struct {
		block string
		props map[string]string
		// stone is the offset from the block to its support.
		stone [3]int32
	}{
		{"cake", nil, [3]int32{0, -1, 0}},
		{"sea_pickle", nil, [3]int32{0, -1, 0}},
		{"brain_coral", nil, [3]int32{0, -1, 0}},
		{"brain_coral_fan", nil, [3]int32{0, -1, 0}},
		{"dead_brain_coral", nil, [3]int32{0, -1, 0}},
		{"dead_brain_coral_fan", nil, [3]int32{0, -1, 0}},
		{"brain_coral_wall_fan", map[string]string{"facing": "east"}, [3]int32{-1, 0, 0}},
		{"dead_brain_coral_wall_fan", map[string]string{"facing": "north"}, [3]int32{0, 0, 1}},
		{"seagrass", nil, [3]int32{0, -1, 0}},
		{"kelp", nil, [3]int32{0, -1, 0}},
		{"kelp_plant", nil, [3]int32{0, -1, 0}},
		{"mangrove_propagule", map[string]string{"hanging": "false"}, [3]int32{0, -1, 0}},
		{"mangrove_propagule", map[string]string{"hanging": "true"}, [3]int32{0, 1, 0}},
	} {
		x, y, z := int32(4+i*3), int32(GroundLevel+4), int32(4)
		state, ok := registry.FindState("minecraft:"+tc.block, tc.props)
		if !ok {
			t.Fatalf("no state %s %v", tc.block, tc.props)
		}
		support := stone
		if tc.block == "mangrove_propagule" && tc.props["hanging"] == "false" {
			support = registry.BlockStates["minecraft:dirt"]
		}
		if tc.props["hanging"] == "true" {
			support = registry.BlockStates["minecraft:mangrove_leaves"]
		}
		sx, sy, sz := x+tc.stone[0], y+tc.stone[1], z+tc.stone[2]
		setBlock(t, w, p, sx, sy, sz, support)
		w.SetBlock(x, y, z, state)
		if got := w.BlockAt(x, y, z); got != state {
			t.Fatalf("%s: setup left %d", tc.block, got)
		}
		_, effects, _ := func() ([]BlockChange, []Effect, []TNTPrime) {
			old := w.BlockAt(sx, sy, sz)
			w.SetBlock(sx, sy, sz, 0)
			return p.NotifyChanged(sx, sy, sz, old)
		}()
		for range 8 {
			clock++
			_, _, _, e := p.Tick(clock)
			effects = append(effects, e...)
		}
		if got, want := w.BlockAt(x, y, z), RemovalLeaves(state); got != want {
			t.Errorf("%s %v after its support went = state %d, want %d", tc.block, tc.props, got, want)
		}
		drops := 0
		for _, e := range effects {
			if e.BreakDrop == state {
				drops++
			}
		}
		if drops != 1 {
			t.Errorf("%s %v dropped %d times, want 1", tc.block, tc.props, drops)
		}
	}
}
