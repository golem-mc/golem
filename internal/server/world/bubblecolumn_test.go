package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// bubbleWorld lays a water column of the given height over a base block at
// (0, GroundLevel-1, 0), with the engine's visits not yet run.
func bubbleWorld(t *testing.T, base uint16, height int32) (*World, *Physics, *int64) {
	w, p := newPhysicsWorld(t)
	clock := new(int64)
	setBlock(t, w, p, 0, GroundLevel-1, 0, base)
	for y := range height {
		setBlock(t, w, p, 0, GroundLevel+y, 0, waterByLevel[0])
	}
	return w, p, clock
}

func runTicks(p *Physics, clock *int64, n int) {
	for range n {
		*clock++
		p.Tick(*clock)
	}
}

func TestSoulSandGrowsALiftingColumn(t *testing.T) {
	w, p, clock := bubbleWorld(t, registry.BlockStates["minecraft:soul_sand"], 4)
	runTicks(p, clock, 19)
	if got := w.BlockAt(0, GroundLevel, 0); got == bubbleUpState {
		t.Fatal("the column grew before its 20-tick check")
	}
	runTicks(p, clock, 1)
	for y := range int32(4) {
		if got := w.BlockAt(0, GroundLevel+y, 0); got != bubbleUpState {
			t.Errorf("block %d above the soul sand = %d, want a lifting column (%d)", y, got, bubbleUpState)
		}
	}
	if got := w.BlockAt(0, GroundLevel+4, 0); got != 0 {
		t.Errorf("the cell above the water = %d, want air", got)
	}
}

func TestMagmaBlockGrowsADraggingColumn(t *testing.T) {
	w, p, clock := bubbleWorld(t, registry.BlockStates["minecraft:magma_block"], 3)
	runTicks(p, clock, 22)
	for y := range int32(3) {
		if got := w.BlockAt(0, GroundLevel+y, 0); got != bubbleDownState {
			t.Errorf("block %d above the magma = %d, want a dragging column (%d)", y, got, bubbleDownState)
		}
	}
}

func TestBreakingTheBaseRemovesTheColumn(t *testing.T) {
	w, p, clock := bubbleWorld(t, registry.BlockStates["minecraft:soul_sand"], 3)
	runTicks(p, clock, 22)
	setBlock(t, w, p, 0, GroundLevel-1, 0, registry.BlockStates["minecraft:cobblestone"])
	runTicks(p, clock, 7)
	for y := range int32(3) {
		if got := w.BlockAt(0, GroundLevel+y, 0); got != waterByLevel[0] {
			t.Errorf("block %d above the stone = %d, want still water", y, got)
		}
	}
}

func TestPlacingSoulSandUnderWaterStartsAColumn(t *testing.T) {
	w, p, clock := bubbleWorld(t, registry.BlockStates["minecraft:cobblestone"], 3)
	runTicks(p, clock, 30)
	setBlock(t, w, p, 0, GroundLevel-1, 0, registry.BlockStates["minecraft:soul_sand"])
	runTicks(p, clock, 22)
	if got := w.BlockAt(0, GroundLevel+2, 0); got != bubbleUpState {
		t.Errorf("top of the water over placed soul sand = %d, want a lifting column", got)
	}
}

func TestColumnStopsAtFlowingWater(t *testing.T) {
	w, p, clock := bubbleWorld(t, registry.BlockStates["minecraft:soul_sand"], 2)
	setBlock(t, w, p, 0, GroundLevel+2, 0, waterByLevel[1])
	runTicks(p, clock, 22)
	if got := w.BlockAt(0, GroundLevel+1, 0); got != bubbleUpState {
		t.Errorf("top source = %d, want a column", got)
	}
	if got := w.BlockAt(0, GroundLevel+2, 0); got == bubbleUpState {
		t.Error("the column grew through flowing water")
	}
}
