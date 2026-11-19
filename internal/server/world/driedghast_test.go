package world

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// waterloggedGhastState builds the waterlogged dried ghast state the tests
// book and hatch.
func waterloggedGhastState(t *testing.T, facing string, hydration int) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:dried_ghast", map[string]string{
		"facing": facing, "hydration": strconv.Itoa(hydration), "waterlogged": "true",
	})
	if !ok {
		t.Fatalf("no waterlogged dried ghast facing %s at hydration %d", facing, hydration)
	}
	return state
}

// TestABookedDriedGhastLeavesItsCellsFluidVisitFree checks that the hydration
// visit a dried ghast books is its own kind: a flood's fluid visit of the same
// waterlogged cell still lands, and the source mining the block leaves does
// still spread.
// vanilla: DriedGhastBlock.hasScheduledTick
func TestABookedDriedGhastLeavesItsCellsFluidVisitFree(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	state := waterloggedGhastState(t, "north", 0)
	setBlock(t, w, p, 0, GroundLevel, 0, state)
	p.driedGhastRandomTick(0, GroundLevel, 0, state)

	// The booking is the ghast's own: a plain fluid visit of the same cell
	// still goes on the queue beside it.
	p.schedule(0, GroundLevel, 0, p.now+waterTickDelay)
	if len(p.queue) != 2 {
		t.Fatalf("queue entries after booking the ghast and a fluid visit = %d, want 2", len(p.queue))
	}

	// Replacing the block with the source mining it leaves: the spread must
	// not be held back by the booking.
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	for range 100 {
		clock++
		p.Tick(clock)
	}
	assertFluid(t, w, 1, GroundLevel, 0, fluidWater, 1, "the mining source spreads")
}

// TestAHatchLeavesAWaterSourceThatSpreads checks that the hatch writes the
// cell's water with flag 3: the source books its own spread and reaches an
// open neighbour, rather than standing as one block forever.
// vanilla: Level.removeBlock, LiquidBlock.onPlace
func TestAHatchLeavesAWaterSourceThatSpreads(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	setBlock(t, w, p, 0, GroundLevel, 0, waterloggedGhastState(t, "north", driedGhastMaxHydration))
	p.scheduleDriedGhast(0, GroundLevel, 0, p.now)

	clock++
	p.Tick(clock)
	if got := w.BlockAt(0, GroundLevel, 0); got != waterByLevel[0] {
		t.Fatalf("the hatch left block %d, want the water source %d", got, waterByLevel[0])
	}

	for range 100 {
		clock++
		p.Tick(clock)
	}
	assertFluid(t, w, 1, GroundLevel, 0, fluidWater, 1, "the hatch water spreads")
}
