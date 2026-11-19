package rules

import (
	"testing"
)

// The conduit's frame: what counts, what the water gate takes, and the
// reach the frame's size draws.

// frameWorldAt is a world of one state but for the cells named.
type frameWorldAt struct {
	state   uint16
	replace map[[3]int32]uint16
}

func (w frameWorldAt) BlockAt(x, y, z int32) uint16 {
	if s, ok := w.replace[[3]int32{x, y, z}]; ok {
		return s
	}
	return w.state
}

// conduitWaterCells is the 3×3×3 the water gate reads, listed literally so
// the test does not re-derive the scan it checks.
var conduitWaterCells = [][3]int32{
	{-1, -1, -1}, {-1, -1, 0}, {-1, -1, 1}, {-1, 0, -1}, {-1, 0, 0}, {-1, 0, 1},
	{-1, 1, -1}, {-1, 1, 0}, {-1, 1, 1},
	{0, -1, -1}, {0, -1, 0}, {0, -1, 1}, {0, 0, -1}, {0, 0, 0}, {0, 0, 1},
	{0, 1, -1}, {0, 1, 0}, {0, 1, 1},
	{1, -1, -1}, {1, -1, 0}, {1, -1, 1}, {1, 0, -1}, {1, 0, 0}, {1, 0, 1},
	{1, 1, -1}, {1, 1, 0}, {1, 1, 1},
}

// conduitRingCells is the sixteen frame cells of the conduit's own level,
// the smallest frame that activates a conduit.
var conduitRingCells = [][3]int32{
	{-2, 0, -2}, {-2, 0, -1}, {-2, 0, 0}, {-2, 0, 1}, {-2, 0, 2},
	{2, 0, -2}, {2, 0, -1}, {2, 0, 0}, {2, 0, 1}, {2, 0, 2},
	{-1, 0, -2}, {0, 0, -2}, {1, 0, -2},
	{-1, 0, 2}, {0, 0, 2}, {1, 0, 2},
}

// conduitCageCells is the 42 cells a complete cage is built of, the 5×5×5
// shell the arms close over around each axis.
var conduitCageCells = [][3]int32{
	{-2, -2, 0}, {-2, -1, 0}, {-2, 0, -2}, {-2, 0, -1}, {-2, 0, 0}, {-2, 0, 1},
	{-2, 0, 2}, {-2, 1, 0}, {-2, 2, 0},
	{-1, -2, 0}, {-1, 0, -2}, {-1, 0, 2}, {-1, 2, 0},
	{0, -2, -2}, {0, -2, -1}, {0, -2, 0}, {0, -2, 1}, {0, -2, 2},
	{0, -1, -2}, {0, -1, 2}, {0, 0, -2}, {0, 0, 2}, {0, 1, -2}, {0, 1, 2},
	{0, 2, -2}, {0, 2, -1}, {0, 2, 0}, {0, 2, 1}, {0, 2, 2},
	{1, -2, 0}, {1, 0, -2}, {1, 0, 2}, {1, 2, 0},
	{2, -2, 0}, {2, -1, 0}, {2, 0, -2}, {2, 0, -1}, {2, 0, 0}, {2, 0, 1},
	{2, 0, 2}, {2, 1, 0}, {2, 2, 0},
}

// cageWorld builds a world of stone with the water cube and the frame
// cells named, about the conduit standing at (0, 64, 0). The conduit's
// own cell counts through its waterlogging, which the water written there
// stands in for.
func cageWorld(t *testing.T, water, frame [][3]int32) frameWorldAt {
	t.Helper()
	world := frameWorldAt{state: stateOf(t, "minecraft:stone"), replace: map[[3]int32]uint16{}}
	for _, p := range water {
		world.replace[[3]int32{p[0], 64 + p[1], p[2]}] = stateOf(t, "minecraft:water")
	}
	for _, p := range frame {
		world.replace[[3]int32{p[0], 64 + p[1], p[2]}] = stateOf(t, "minecraft:prismarine")
	}
	return world
}

// TestConduitFrameCountsTheCage checks the scan's answers to the shapes
// that matter: no water about the conduit is no frame, the sixteen-cell
// ring reads sixteen, every family block counts alike, and a whole cage
// is the forty-two the shape's arithmetic says.
// vanilla: ConduitBlockEntity.updateShape
func TestConduitFrameCountsTheCage(t *testing.T) {
	water, ring, whole := conduitWaterCells, conduitRingCells, conduitCageCells
	if len(water) != 27 || len(whole) != 42 || len(ring) != 16 {
		t.Fatalf("the cube holds %d cells, the cage %d, %d of them the ring's, want 27, 42 and 16",
			len(water), len(whole), len(ring))
	}
	// A cage of water and stone: no frame cells, however wet.
	if got := ConduitFrame(cageWorld(t, water, nil), 0, 64, 0); got != 0 {
		t.Errorf("water alone reads a frame of %d, want 0", got)
	}
	// The prismarine frame cells count one apiece.
	if got := ConduitFrame(cageWorld(t, water, ring), 0, 64, 0); got != 16 {
		t.Errorf("the ring reads %d, want 16", got)
	}
	if got := ConduitFrame(cageWorld(t, water, whole[:41]), 0, 64, 0); got != 41 {
		t.Errorf("a cage one cell short reads %d, want 41", got)
	}
	if got := ConduitFrame(cageWorld(t, water, whole), 0, 64, 0); got != 42 {
		t.Errorf("the whole cage reads %d, want 42", got)
	}
	// Every block of the family counts alike: the sea lantern's cage is
	// the prismarine's.
	seaLanternCage := cageWorld(t, water, nil)
	for _, p := range whole {
		seaLanternCage.replace[[3]int32{p[0], 64 + p[1], p[2]}] = stateOf(t, "minecraft:sea_lantern")
	}
	if got := ConduitFrame(seaLanternCage, 0, 64, 0); got != 42 {
		t.Errorf("a sea lantern cage reads %d, want 42", got)
	}
	// A kelp cell among the twenty-seven holds water too, its fluid state a
	// source although the block carries no waterlogged property.
	// vanilla: KelpBlock.getFluidState
	kelpCage := cageWorld(t, water, whole)
	kelpCage.replace[[3]int32{1, 64, 1}] = stateOf(t, "minecraft:kelp")
	if got := ConduitFrame(kelpCage, 0, 64, 0); got != 42 {
		t.Errorf("a cage with a kelp cell reads %d, want 42", got)
	}
	// A single dry cell among the twenty-seven takes the whole frame with
	// it: the water gate holds whole or nothing.
	caged := cageWorld(t, water, whole)
	caged.replace[[3]int32{1, 64, 1}] = stateOf(t, "minecraft:stone")
	if got := ConduitFrame(caged, 0, 64, 0); got != 0 {
		t.Errorf("a dry cell about the conduit reads %d, want 0", got)
	}
}

// TestConduitHuntsTheCube checks the pick's box: a body at the far corner of
// the eight-block cube is a candidate although the sphere the kept target
// is measured against does not reach it.
// vanilla: ConduitBlockEntity.selectNewTarget, AABB(pos).inflate(8)
func TestConduitHuntsTheCube(t *testing.T) {
	// A body at offset (7, 7, 7): its own box starts there.
	if !ConduitHunts(0, 64, 0, 7, 71, 7, 7.6, 71.9, 7.6) {
		t.Errorf("a body at the cube's (7,7,7) corner is not a candidate")
	}
	if ConduitReaches(0, 64, 0, 7.5, 71.5, 7.5, ConduitHuntRange) {
		t.Errorf("the reach unexpectedly takes the (7,7,7) corner; the test no longer proves the cube")
	}
	// The box is the block's own box grown eight each way, so its edges
	// stand at x-8 and x+9: a body wholly at 9 or past -9 on an axis meets
	// neither, a body at -8 still does.
	if ConduitHunts(0, 64, 0, 9, 64, 0, 9.6, 64.9, 0.6) {
		t.Errorf("a body nine blocks out on x is a candidate, want the box to stop at 9")
	}
	if !ConduitHunts(0, 64, 0, -8, 64, 0, -7.4, 64.9, 0.6) {
		t.Errorf("a body at x=-8 still meets the box, whose near edge is -8")
	}
	if ConduitHunts(0, 64, 0, -8.7, 64, 0, -8.1, 64.9, 0.6) {
		t.Errorf("a body wholly past x=-8 is a candidate, want the box to stop there")
	}
}

// rainStub rains on the cells it holds.
type rainStub map[[3]int32]bool

func (r rainStub) RainingAt(x, y, z int32) bool { return r[[3]int32{x, y, z}] }
