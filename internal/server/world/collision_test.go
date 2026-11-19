package world

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestCollisionBoxes is the entity sweep's per-state read: the modelled
// families' own boxes, the generated table's height for everything else,
// and nothing where the table carries no shape at all.
func TestCollisionBoxes(t *testing.T) {
	t.Parallel()
	post := CollisionBox{MinX: 6.0 / 16, MinZ: 6.0 / 16, MaxX: 10.0 / 16, MaxY: 1.5, MaxZ: 10.0 / 16}
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  []CollisionBox
	}{
		{"minecraft:stone", nil, []CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}},
		{"minecraft:farmland", nil, []CollisionBox{{MaxX: 1, MaxY: 15.0 / 16, MaxZ: 1}}},
		{"minecraft:white_carpet", nil, []CollisionBox{{MaxX: 1, MaxY: 1.0 / 16, MaxZ: 1}}},
		{"minecraft:oak_fence", nil, []CollisionBox{post}},
		{"minecraft:oak_slab", map[string]string{"type": "bottom"},
			[]CollisionBox{{MaxX: 1, MaxY: 0.5, MaxZ: 1}}},
		{"minecraft:oak_slab", map[string]string{"type": "top"},
			[]CollisionBox{{MinY: 0.5, MaxX: 1, MaxY: 1, MaxZ: 1}}},
		{"minecraft:chest", nil,
			[]CollisionBox{{MinX: 1.0 / 16, MinZ: 1.0 / 16, MaxX: 15.0 / 16, MaxY: 14.0 / 16, MaxZ: 15.0 / 16}}},
		{"minecraft:lily_pad", nil, []CollisionBox{{MaxX: 1, MaxY: 1.5 / 16, MaxZ: 1}}},
		// An end portal frame is a 13/16 base; the eye adds the 8x8 middle
		// column that reaches the full 16/16.
		{"minecraft:end_portal_frame", map[string]string{"eye": "false", "facing": "north"},
			[]CollisionBox{{MaxX: 1, MaxY: 13.0 / 16, MaxZ: 1}}},
		{"minecraft:end_portal_frame", map[string]string{"eye": "true", "facing": "north"},
			[]CollisionBox{{MaxX: 1, MaxY: 13.0 / 16, MaxZ: 1},
				{MinX: 4.0 / 16, MinY: 13.0 / 16, MinZ: 4.0 / 16,
					MaxX: 12.0 / 16, MaxY: 1, MaxZ: 12.0 / 16}}},
		// Frogspawn models a 1.5-sixteenth plate for the flow, but the
		// generated table carries no collision for it, so the entity sweep
		// must read nothing.
		{"minecraft:frogspawn", nil, nil},
		{"minecraft:oak_fence_gate", map[string]string{"open": "true"}, nil},
		{"minecraft:torch", nil, nil},
	} {
		state := mustFind(t, tc.name, tc.props)
		got := CollisionBoxes(state)
		if len(got) != len(tc.want) {
			t.Errorf("CollisionBoxes(%s %v) = %v, want %v", tc.name, tc.props, got, tc.want)
			continue
		}
		wantTop := 0.0
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("box %d of CollisionBoxes(%s %v) = %v, want %v",
					i, tc.name, tc.props, got[i], tc.want[i])
			}
			wantTop = max(wantTop, tc.want[i].MaxY)
		}
		if top := CollisionTop(state); top != wantTop {
			t.Errorf("CollisionTop(%s %v) = %v, want the boxes' top %v", tc.name, tc.props, top, wantTop)
		}
	}
}

// TestCollisionFull checks a sample of states, picked from the vanilla
// dump, whose collision fullness doesn't match their look, both ways.
func TestCollisionFull(t *testing.T) {
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
	for _, tc := range []struct {
		s    string
		want bool
	}{
		{"stone", true},
		{"glass", true},
		{"beacon", true},
		{"spawner", true},
		{"loom", true},
		{"brain_coral_block", true},
		{"copper_grate", true},
		{"chiseled_bookshelf", true},
		{"frosted_ice", true},
		{"chorus_flower", true},
		{"mangrove_roots", true},
		{"sculk_catalyst", true},
		{"vault", true},
		{"trial_spawner", true},
		{"test_instance_block", true},
		{"oak_slab[type=double]", true},
		{"piston[facing=up]", true},
		{"piston[facing=up,extended=true]", false},
		{"mud", false},
		{"soul_sand", false},
		{"moss_carpet", false},
		{"oak_stairs", false},
		{"oak_slab", false},
		{"oak_door[half=lower]", false},
		{"snow[layers=1]", false},
	} {
		if got := CollisionFull(state(tc.s)); got != tc.want {
			t.Errorf("CollisionFull(%s) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// sweepBoxes is the sweep's read of boxes drawn in sixteenths, the form the
// vanilla block classes write theirs in.
func sweepBoxes(boxes ...[6]float64) []CollisionBox {
	out := make([]CollisionBox, len(boxes))
	for i, b := range boxes {
		out[i] = CollisionBox{b[0] / 16, b[1] / 16, b[2] / 16, b[3] / 16, b[4] / 16, b[5] / 16}
	}
	return out
}

// TestCollisionBoxesOfStairCornersAndShelves holds the corner and the shelf
// shapes, every box set the vanilla one: a stair's corner trims its tall half
// to the outer corner or wings it out to the inner one, and a shelf's plate
// and lip stand in front of its back panel whichever way it faces. The
// straight stair stays as it was.
// vanilla: StairBlock.getShape, ShelfBlock.getShape
func TestCollisionBoxesOfStairCornersAndShelves(t *testing.T) {
	t.Parallel()
	slab := [6]float64{0, 0, 0, 16, 8, 16}
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  []CollisionBox
	}{
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "bottom", "shape": "straight"},
			sweepBoxes(slab, [6]float64{0, 8, 0, 16, 16, 8})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "bottom", "shape": "outer_left"},
			sweepBoxes(slab, [6]float64{0, 8, 0, 8, 16, 8})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "bottom", "shape": "outer_right"},
			sweepBoxes(slab, [6]float64{8, 8, 0, 16, 16, 8})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "bottom", "shape": "inner_left"},
			sweepBoxes(slab,
				[6]float64{0, 8, 0, 8, 16, 16}, [6]float64{8, 8, 0, 16, 16, 8})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "bottom", "shape": "inner_right"},
			sweepBoxes(slab,
				[6]float64{0, 8, 0, 16, 16, 8}, [6]float64{8, 8, 8, 16, 16, 16})},
		// The top stair's own decomposition joins the back strip's lower
		// half with the slab above it into one box.
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "top", "shape": "inner_left"},
			sweepBoxes([6]float64{0, 0, 0, 8, 16, 16},
				[6]float64{8, 0, 0, 16, 16, 8}, [6]float64{8, 8, 8, 16, 16, 16})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "top", "shape": "outer_right"},
			sweepBoxes([6]float64{8, 0, 0, 16, 16, 8},
				[6]float64{0, 8, 0, 8, 16, 16}, [6]float64{8, 8, 8, 16, 16, 16})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "north", "half": "top", "shape": "inner_right"},
			sweepBoxes([6]float64{0, 0, 0, 16, 16, 8},
				[6]float64{8, 0, 8, 16, 16, 16}, [6]float64{0, 8, 8, 8, 16, 16})},
		// A quarter turn turns the whole shape with it, corners included.
		{"minecraft:oak_stairs",
			map[string]string{"facing": "east", "half": "bottom", "shape": "inner_left"},
			sweepBoxes(slab,
				[6]float64{0, 8, 0, 16, 16, 8}, [6]float64{8, 8, 8, 16, 16, 16})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "south", "half": "bottom", "shape": "outer_left"},
			sweepBoxes(slab, [6]float64{8, 8, 8, 16, 16, 16})},
		{"minecraft:oak_stairs",
			map[string]string{"facing": "west", "half": "top", "shape": "inner_right"},
			sweepBoxes([6]float64{0, 0, 0, 8, 16, 16},
				[6]float64{8, 0, 0, 16, 16, 8}, [6]float64{8, 8, 8, 16, 16, 16})},
		{"minecraft:oak_shelf",
			map[string]string{"facing": "north", "side_chain": "unconnected"},
			sweepBoxes([6]float64{0, 0, 13, 16, 16, 16},
				[6]float64{0, 12, 11, 16, 16, 13}, [6]float64{0, 0, 11, 16, 4, 13})},
		{"minecraft:oak_shelf",
			map[string]string{"facing": "west", "side_chain": "unconnected"},
			sweepBoxes([6]float64{13, 0, 0, 16, 16, 16},
				[6]float64{11, 12, 0, 13, 16, 16}, [6]float64{11, 0, 0, 13, 4, 16})},
		{"minecraft:oak_shelf",
			map[string]string{"facing": "south", "side_chain": "unconnected"},
			sweepBoxes([6]float64{0, 0, 0, 16, 16, 3},
				[6]float64{0, 12, 3, 16, 16, 5}, [6]float64{0, 0, 3, 16, 4, 5})},
		{"minecraft:oak_shelf",
			map[string]string{"facing": "east", "side_chain": "unconnected"},
			sweepBoxes([6]float64{0, 0, 0, 3, 16, 16},
				[6]float64{3, 12, 0, 5, 16, 16}, [6]float64{3, 0, 0, 5, 4, 16})},
		// The side chain and the powered bit change nothing the boxes say.
		{"minecraft:oak_shelf",
			map[string]string{"facing": "west", "powered": "true", "side_chain": "left"},
			sweepBoxes([6]float64{13, 0, 0, 16, 16, 16},
				[6]float64{11, 12, 0, 13, 16, 16}, [6]float64{11, 0, 0, 13, 4, 16})},
	} {
		state := mustFind(t, tc.name, tc.props)
		got := CollisionBoxes(state)
		if len(got) != len(tc.want) {
			t.Errorf("CollisionBoxes(%s %v) = %v, want %v", tc.name, tc.props, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("box %d of CollisionBoxes(%s %v) = %v, want %v",
					i, tc.name, tc.props, got[i], tc.want[i])
			}
		}
	}
}

// TestCollisionBoxesOfTheGeneratedShapeTable holds the families the
// generated table carries — the beds, the cakes, the anvils, the cauldrons,
// the hoppers, the bells, the pots and the pitcher crop — whose boxes no Go
// builder models. Every set is the vanilla one.
// vanilla: BedBlock, CakeBlock, CandleCakeBlock, AnvilBlock,
// AbstractCauldronBlock, HopperBlock, BellBlock, FlowerPotBlock,
// PitcherCropBlock
func TestCollisionBoxesOfTheGeneratedShapeTable(t *testing.T) {
	t.Parallel()
	cauldronWalls := [][6]float64{
		{0, 0, 0, 2, 16, 4}, {0, 0, 12, 2, 16, 16},
		{2, 0, 0, 4, 16, 2}, {2, 0, 14, 4, 16, 16},
		{12, 0, 0, 16, 16, 2}, {12, 0, 14, 16, 16, 16},
		{14, 0, 2, 16, 16, 4}, {14, 0, 12, 16, 16, 14},
		{0, 3, 4, 16, 4, 12}, {2, 3, 2, 14, 4, 4},
		{2, 3, 12, 14, 4, 14}, {4, 3, 0, 12, 16, 2},
		{4, 3, 14, 12, 16, 16}, {0, 4, 4, 2, 16, 12},
		{14, 4, 4, 16, 16, 12},
	}
	hopperMouth := [][6]float64{
		{0, 10, 0, 4, 11, 16}, {4, 10, 0, 16, 11, 4},
		{4, 10, 12, 16, 11, 16}, {12, 10, 4, 16, 11, 12},
		{0, 11, 0, 2, 16, 16}, {2, 11, 0, 16, 16, 2},
		{2, 11, 14, 16, 16, 16}, {14, 11, 2, 16, 16, 14},
	}
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  []CollisionBox
	}{
		{"minecraft:white_bed",
			map[string]string{"facing": "north", "part": "head"},
			sweepBoxes([6]float64{0, 0, 0, 3, 9, 3}, [6]float64{13, 0, 0, 16, 9, 3},
				[6]float64{0, 3, 3, 16, 9, 16}, [6]float64{3, 3, 0, 13, 9, 3})},
		{"minecraft:white_bed",
			map[string]string{"facing": "east", "part": "foot"},
			sweepBoxes([6]float64{0, 0, 0, 3, 9, 3}, [6]float64{0, 0, 13, 3, 9, 16},
				[6]float64{0, 3, 3, 16, 9, 13}, [6]float64{3, 3, 0, 16, 9, 3},
				[6]float64{3, 3, 13, 16, 9, 16})},
		{"minecraft:cake", map[string]string{"bites": "2"},
			sweepBoxes([6]float64{5, 0, 1, 15, 8, 15})},
		{"minecraft:cake", map[string]string{"bites": "6"},
			sweepBoxes([6]float64{13, 0, 1, 15, 8, 15})},
		{"minecraft:candle_cake", map[string]string{"lit": "false"},
			sweepBoxes([6]float64{1, 0, 1, 15, 8, 15}, [6]float64{7, 8, 7, 9, 14, 9})},
		{"minecraft:anvil", map[string]string{"facing": "north"},
			sweepBoxes([6]float64{2, 0, 2, 14, 4, 14},
				[6]float64{4, 4, 3, 12, 5, 13}, [6]float64{6, 5, 4, 10, 16, 12},
				[6]float64{3, 10, 0, 6, 16, 16}, [6]float64{6, 10, 0, 13, 16, 4},
				[6]float64{6, 10, 12, 13, 16, 16}, [6]float64{10, 10, 4, 13, 16, 12})},
		{"minecraft:anvil", map[string]string{"facing": "east"},
			sweepBoxes([6]float64{2, 0, 2, 14, 4, 14},
				[6]float64{3, 4, 4, 13, 5, 12}, [6]float64{4, 5, 6, 12, 16, 10},
				[6]float64{0, 10, 3, 4, 16, 13}, [6]float64{4, 10, 3, 16, 16, 6},
				[6]float64{4, 10, 10, 16, 16, 13}, [6]float64{12, 10, 6, 16, 16, 10})},
		// Every cauldron reads the same wall and rim, its content's level
		// aside. vanilla: AbstractCauldronBlock's SHAPE
		{"minecraft:cauldron", nil, sweepBoxes(cauldronWalls...)},
		{"minecraft:water_cauldron", map[string]string{"level": "2"},
			sweepBoxes(cauldronWalls...)},
		// The hopper facing down carries the spout under its bowl; a
		// horizontal one sends it out the side. vanilla: HopperBlock.makeShapes
		{"minecraft:hopper", map[string]string{"facing": "down"},
			sweepBoxes(append([][6]float64{
				{6, 0, 6, 10, 11, 10},
				{4, 4, 4, 6, 11, 12}, {6, 4, 4, 12, 11, 6},
				{6, 4, 10, 12, 11, 12}, {10, 4, 6, 12, 11, 10},
			}, hopperMouth...)...)},
		{"minecraft:hopper", map[string]string{"facing": "east"},
			sweepBoxes(append([][6]float64{
				{4, 4, 4, 12, 11, 12},
				{12, 4, 6, 16, 8, 10},
			}, hopperMouth...)...)},
		// A bell's collision depends on where it hangs: a frame filling the
		// slice across its cell on the floor, the bell and its cap on the
		// ceiling, and the swing bridge across a wall. vanilla: BellBlock.getShape
		{"minecraft:bell", map[string]string{"attachment": "floor", "facing": "north"},
			sweepBoxes([6]float64{0, 0, 4, 16, 16, 12})},
		{"minecraft:bell", map[string]string{"attachment": "ceiling", "facing": "east"},
			sweepBoxes([6]float64{4, 4, 4, 12, 6, 12},
				[6]float64{5, 6, 5, 11, 13, 11}, [6]float64{7, 13, 7, 9, 16, 9})},
		{"minecraft:bell", map[string]string{"attachment": "single_wall", "facing": "north"},
			sweepBoxes([6]float64{4, 4, 4, 12, 6, 12},
				[6]float64{5, 6, 5, 11, 13, 11}, [6]float64{7, 13, 0, 9, 15, 13})},
		{"minecraft:bell", map[string]string{"attachment": "double_wall", "facing": "east"},
			sweepBoxes([6]float64{4, 4, 4, 12, 6, 12},
				[6]float64{5, 6, 5, 11, 13, 11}, [6]float64{0, 13, 7, 16, 15, 9})},
		{"minecraft:flower_pot", nil,
			sweepBoxes([6]float64{5, 0, 5, 11, 6, 11})},
		{"minecraft:potted_oak_sapling", nil,
			sweepBoxes([6]float64{5, 0, 5, 11, 6, 11})},
		// The pitcher crop's lower half reaches a sixteenth under its cell;
		// the upper half holds nothing at all.
		{"minecraft:pitcher_crop", map[string]string{"age": "0", "half": "lower"},
			sweepBoxes([6]float64{5, -1, 5, 11, 3, 11})},
		{"minecraft:pitcher_crop", map[string]string{"age": "4", "half": "lower"},
			sweepBoxes([6]float64{3, -1, 3, 13, 5, 13})},
		{"minecraft:pitcher_crop", map[string]string{"age": "4", "half": "upper"}, nil},
	} {
		state := mustFind(t, tc.name, tc.props)
		got := CollisionBoxes(state)
		if len(got) != len(tc.want) {
			t.Errorf("CollisionBoxes(%s %v) = %v, want %v", tc.name, tc.props, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("box %d of CollisionBoxes(%s %v) = %v, want %v",
					i, tc.name, tc.props, got[i], tc.want[i])
			}
		}
		if top := CollisionTop(state); top != topOf(tc.want) {
			t.Errorf("CollisionTop(%s %v) = %v, want the boxes' top %v",
				tc.name, tc.props, top, topOf(tc.want))
		}
	}
}

// topOf is the highest top among a state's boxes.
func topOf(boxes []CollisionBox) float64 {
	var top float64
	for _, b := range boxes {
		top = max(top, b.MaxY)
	}
	return top
}

// TestCollidesAnew is the moved-wrongly read: only a shape the new box
// crosses and the old one did not counts, a face touched is not crossed, and
// scaffolding is never asked.
func TestCollidesAnew(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{X: 0, Z: 0})
	w.SetBlock(2, 100, 0, mustFind(t, "minecraft:stone", nil))
	w.SetBlock(0, 100, 0, mustFind(t, "minecraft:stone", nil))
	w.SetBlock(4, 100, 0, mustFind(t, "minecraft:scaffolding", map[string]string{"distance": "0", "bottom": "false", "waterlogged": "false"}))
	w.SetBlock(6, 100, 0, mustFind(t, "minecraft:cactus", map[string]string{"age": "0"}))
	w.SetBlock(8, 100, 0, mustFind(t, "minecraft:honey_block", nil))
	body := func(x, y, z float64) [6]float64 {
		return [6]float64{x - 0.3, y, z - 0.3, x + 0.3, y + 1.8, z + 0.3}
	}
	for _, tc := range []struct {
		name     string
		from, to [6]float64
		want     bool
	}{
		{"into a block", body(1.5, 100, 0.5), body(2.5, 100, 0.5), true},
		{"up against its face", body(1.5, 100, 0.5), body(1.7, 100, 0.5), false},
		{"onto its top", body(2.5, 102, 0.5), body(2.5, 101, 0.5), false},
		{"within one it was already in", body(0.5, 100, 0.5), body(0.6, 100, 0.5), false},
		// Shapes the table only rounds up to a whole cell are never refused.
		{"up against a cactus", body(5.5, 100, 0.5), body(6.0625-0.3, 100, 0.5), false},
		{"onto honey", body(7.5, 100, 0.5), body(8.5, 100+15.0/16, 0.5), false},
		{"into scaffolding", body(4.5, 101, 0.5), body(4.5, 100.5, 0.5), false},
	} {
		if got := w.CollidesAnew(tc.from, tc.to); got != tc.want {
			t.Errorf("%s: CollidesAnew = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestShapedBlocksAreNotWholeCells pins blocks whose generated shape is
// smaller than their cell.
func TestShapedBlocksAreNotWholeCells(t *testing.T) {
	for _, c := range []struct {
		name string
		top  float64
	}{
		{"minecraft:sea_pickle", 12.0 / 32},
		{"minecraft:composter", 1},
		{"minecraft:chorus_plant", 26.0 / 32},
		{"minecraft:sniffer_egg", 1},
	} {
		state := registry.MustState(c.name, nil)
		if c.name == "minecraft:chorus_plant" {
			state = registry.MustState(c.name, map[string]string{"north": "true"})
		}
		if CollisionTop(state) != c.top {
			t.Errorf("%s top = %v, want %v", c.name, CollisionTop(state), c.top)
		}
	}
	sniffer := registry.MustState("minecraft:sniffer_egg", nil)
	if StateCollides(sniffer, 0, 0, 0, 0, 0.1, 0, 0.05, 0.2, 0.05) {
		t.Error("sniffer egg collides in the cell corner its shape leaves open")
	}
	comp := registry.MustState("minecraft:composter", nil)
	if StateCollides(comp, 0, 0, 0, 0.4, 0.5, 0.4, 0.6, 0.9, 0.6) {
		t.Error("composter collides in its open basin")
	}
}
