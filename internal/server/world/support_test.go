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

func TestSoulFirePlacementSurvives(t *testing.T) {
	w, _ := newPhysicsWorld(t)
	for _, tc := range []struct {
		base registry.Block
		want bool
	}{
		{registry.BlockSoulSand, true},
		{registry.BlockSoulSoil, true},
		{registry.BlockStone, false},
		{registry.BlockAir, false},
	} {
		w.SetBlock(4, GroundLevel-1, 4, tc.base.DefaultState())
		if got := w.PlacementSurvives(registry.BlockSoulFire.DefaultState(), 4, GroundLevel, 4); got != tc.want {
			t.Errorf("soul fire on %s: PlacementSurvives = %v, want %v", tc.base.Name(), got, tc.want)
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

// TestAVineDropsTheFaceItLost takes away one of the two walls a vine hangs
// on: the vine keeps the other face, then goes with the second wall.
func TestAVineDropsTheFaceItLost(t *testing.T) {
	w, p := newPhysicsWorld(t)
	stone := registry.BlockStates["minecraft:stone"]
	vine, _ := registry.FindState("minecraft:vine", map[string]string{"north": "true", "east": "true"})
	north, _ := registry.FindState("minecraft:vine", map[string]string{"north": "true"})
	setBlock(t, w, p, 4, GroundLevel+2, 3, stone)
	setBlock(t, w, p, 5, GroundLevel+2, 4, stone)
	setBlock(t, w, p, 4, GroundLevel+2, 4, vine)

	setBlock(t, w, p, 5, GroundLevel+2, 4, 0)
	if got := w.BlockAt(4, GroundLevel+2, 4); got != north {
		t.Fatalf("vine after its east wall went = %d, want %d (north only)", got, north)
	}
	setBlock(t, w, p, 4, GroundLevel+2, 3, 0)
	if got := w.BlockAt(4, GroundLevel+2, 4); got != 0 {
		t.Errorf("vine after both walls went = %d, want air", got)
	}
}

// TestSulfurSpikeTakesTheSpeleothemShape: SulfurSpikeBlock extends
// SpeleothemBlock, so every state collides as the matching dripstone does,
// at the same per-position offset.
func TestSulfurSpikeTakesTheSpeleothemShape(t *testing.T) {
	t.Parallel()
	for _, s := range registry.AllStates["minecraft:sulfur_spike"] {
		drip, ok := registry.FindState("minecraft:pointed_dripstone", registry.ParseStateProps(s.Props))
		if !ok {
			t.Fatalf("no dripstone state for %s", s.Props)
		}
		got, want := CollisionBoxes(s.ID), CollisionBoxes(drip)
		if len(got) != 1 || len(want) != 1 || got[0] != want[0] {
			t.Errorf("sulfur_spike[%s] collides as %v, want %v", s.Props, got, want)
		}
		gx, gz := collisionOffset(s.ID, 7, -3)
		wx, wz := collisionOffset(drip, 7, -3)
		if gx != wx || gz != wz || gx == 0 && gz == 0 {
			t.Errorf("sulfur_spike[%s] offset (%v, %v), want (%v, %v)", s.Props, gx, gz, wx, wz)
		}
	}
}

// TestAWallHangingSignCollidesAsItsPlank: WallHangingSignBlock's collision
// is SHAPES_PLANK, the plank across the facing, at the top of the cell.
func TestAWallHangingSignCollidesAsItsPlank(t *testing.T) {
	t.Parallel()
	for facing, want := range map[string]CollisionBox{
		"east":  cboxF(6, 14, 0, 10, 16, 16),
		"west":  cboxF(6, 14, 0, 10, 16, 16),
		"north": cboxF(0, 14, 6, 16, 16, 10),
		"south": cboxF(0, 14, 6, 16, 16, 10),
	} {
		s, _ := registry.FindState("minecraft:oak_wall_hanging_sign", map[string]string{"facing": facing})
		if got := CollisionBoxes(s); len(got) != 1 || got[0] != want {
			t.Errorf("oak_wall_hanging_sign[facing=%s] collides as %v, want %v", facing, got, want)
		}
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
