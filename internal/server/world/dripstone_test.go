package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// speleothemState looks up a dry speleothem state by column direction and
// thickness.
func speleothemState(t *testing.T, name, direction, thickness string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, map[string]string{
		"vertical_direction": direction, "thickness": thickness, "waterlogged": "false",
	})
	if !ok {
		t.Fatalf("no %s %s %s", name, direction, thickness)
	}
	return s
}

// TestAStalactitesThicknessIgnoresAPendingFluidVisit checks the gate
// SpeleothemBlock.updateShape runs on its thickness recompute: vanilla asks
// for a scheduled block tick of the speleothem itself, so a fluid visit a
// flood booked must not stop the recompute, while a booked fall must.
// vanilla: SpeleothemBlock.updateShape, hasScheduledTick
func TestAStalactitesThicknessIgnoresAPendingFluidVisit(t *testing.T) {
	w, p := newPhysicsWorld(t)
	stone := registry.BlockStates["minecraft:stone"]

	// A tip hangs from stone; a flood books a fluid visit of its cell where it
	// becomes a waterlogged container. That visit is not a block tick of the
	// speleothem, so a tip placed under it still retips the upper block as a
	// frustum. Both pointed dripstone and its sulfur spike subclass go through
	// the same shape rule.
	for i, name := range []string{"minecraft:pointed_dripstone", "minecraft:sulfur_spike"} {
		x := int32(i * 4)
		setBlock(t, w, p, x, GroundLevel+2, 0, stone)
		setBlock(t, w, p, x, GroundLevel+1, 0, speleothemState(t, name, "down", "tip"))
		p.schedule(x, GroundLevel+1, 0, p.now+waterTickDelay)
		setBlock(t, w, p, x, GroundLevel, 0, speleothemState(t, name, "down", "tip"))
		if got, want := w.BlockAt(x, GroundLevel+1, 0), speleothemState(t, name, "down", "frustum"); got != want {
			t.Errorf("%s stalactite over a new tip under a pending fluid visit = %d, want %d (frustum)", name, got, want)
		}
	}

	// The stalactite's own fall tick does gate the recompute: the stone above
	// gone books the 2-tick fall, and a tip placed under it leaves the upper
	// block's thickness alone.
	setBlock(t, w, p, 8, GroundLevel+2, 0, stone)
	setBlock(t, w, p, 8, GroundLevel+1, 0, speleothemState(t, "minecraft:pointed_dripstone", "down", "tip"))
	setBlock(t, w, p, 8, GroundLevel+2, 0, airState)
	if !p.fallBooked[blockKey{8, GroundLevel + 1, 0}] {
		t.Fatalf("the stalactite whose hold went has no fall booked")
	}
	setBlock(t, w, p, 8, GroundLevel, 0, speleothemState(t, "minecraft:pointed_dripstone", "down", "tip"))
	if got, want := w.BlockAt(8, GroundLevel+1, 0), speleothemState(t, "minecraft:pointed_dripstone", "down", "tip"); got != want {
		t.Errorf("falling stalactite over a new tip = %d, want %d (thickness unchanged)", got, want)
	}
}

// TestSurvivalVisitLeavesTheFluidVisitOnItsTime checks that a survival visit
// booked beside a pending water visit neither moves the water visit nor is
// dropped by it, and that a block tick booked twice keeps its first time.
// vanilla: LevelChunkTicks.schedule
func TestSurvivalVisitLeavesTheFluidVisitOnItsTime(t *testing.T) {
	_, p := newPhysicsWorld(t)
	p.schedule(1, GroundLevel, 1, p.now+waterTickDelay)
	p.scheduleSurvival(1, GroundLevel, 1, p.now+1)
	p.scheduleSurvival(1, GroundLevel, 1, p.now+2)
	due := map[uint8]int64{}
	for _, tk := range p.queue {
		due[tk.event] = tk.due
	}
	if len(p.queue) != 2 || due[0] != p.now+waterTickDelay || due[survivalTickEvent] != p.now+1 {
		t.Errorf("queue = %+v, want a water visit at +%d and a survival visit at +1", p.queue, waterTickDelay)
	}
}

// TestScheduleKeepsTheFirstBooking checks that a later booking of an earlier
// time leaves a pending entry where it was.
// vanilla: LevelChunkTicks.schedule
func TestScheduleKeepsTheFirstBooking(t *testing.T) {
	_, p := newPhysicsWorld(t)
	p.schedule(1, GroundLevel, 1, p.now+39)
	p.schedule(1, GroundLevel, 1, p.now+30)
	if len(p.queue) != 1 || p.queue[0].due != p.now+39 {
		t.Errorf("queue = %+v, want one entry due at +39", p.queue)
	}
}

func TestSulfurSpikeTipMergeRepairedByPostProcessing(t *testing.T) {
	c := newChunk(-68, 31)
	NewOverworldGenerator(0).Generate(c)
	s := c.BlockAt(-1088&15, 59, 503&15)
	if registry.BlockOf(s) != registry.BlockSulfurSpike {
		t.Fatalf("block = %v, want sulfur_spike", registry.BlockOf(s))
	}
	if got := registry.StateValue(s, registry.PropThickness); got != "tip" {
		t.Errorf("thickness = %s, want tip", got)
	}
	if got := registry.StateValue(s, registry.PropVerticalDirection); got != "up" {
		t.Errorf("direction = %s, want up", got)
	}
	if !registry.StateBool(s, registry.PropWaterlogged) {
		t.Error("not waterlogged")
	}
}

func TestLakeMarkedSulfurSpikeTipAfterNeighbours(t *testing.T) {
	g := NewOverworldGenerator(0)
	var c *Chunk
	for x := int32(-98); x <= -96; x++ {
		for z := int32(14); z <= 16; z++ {
			cc := newChunk(x, z)
			g.Generate(cc)
			if x == -97 && z == 15 {
				c = cc
			}
		}
	}
	s := c.BlockAt(-1550&15, -32, 254&15)
	if registry.BlockOf(s) != registry.BlockSulfurSpike {
		t.Fatalf("block = %v, want sulfur_spike", registry.BlockOf(s))
	}
	if got := registry.StateValue(s, registry.PropThickness); got != "tip" {
		t.Errorf("thickness = %s, want tip", got)
	}
}
