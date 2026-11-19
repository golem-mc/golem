package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestACopperChestHalfFollowsItsPartner: a double copper chest half whose
// partner became another copper chest becomes that block too, keeping its
// own properties. vanilla: CopperChestBlock.updateShape
func TestACopperChestHalfFollowsItsPartner(t *testing.T) {
	w, p := newPhysicsWorld(t)
	state := func(name, typ string) uint16 {
		s, _ := registry.FindState(name, map[string]string{"facing": "north", "type": typ, "waterlogged": "false"})
		return s
	}
	// Facing north, the left half's partner is east of it.
	w.SetBlock(0, GroundLevel, 0, state("minecraft:weathered_copper_chest", "left"))
	w.SetBlock(1, GroundLevel, 0, state("minecraft:weathered_copper_chest", "right"))
	setBlock(t, w, p, 0, GroundLevel, 0, state("minecraft:exposed_copper_chest", "left"))
	if got, want := w.BlockAt(1, GroundLevel, 0), state("minecraft:exposed_copper_chest", "right"); got != want {
		t.Errorf("the partner = %d, want %d (exposed, still the right half)", got, want)
	}
}

// TestLeastOxidizedCopperChest: a mixed pair settles on the less oxidized
// stage, with the wax coming off both when just one is waxed; an equal stage
// keeps the first. The first row is a waxed unaffected chest, which beats an
// unwaxed oxidized one once both are unwaxed.
// vanilla: CopperChestBlock.getLeastOxidizedChestOfConnectedBlocks
func TestLeastOxidizedCopperChest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, partner, want registry.Block }{
		{registry.BlockWaxedCopperChest, registry.BlockOxidizedCopperChest, registry.BlockCopperChest},
		{registry.BlockExposedCopperChest, registry.BlockOxidizedCopperChest, registry.BlockExposedCopperChest},
		{registry.BlockOxidizedCopperChest, registry.BlockCopperChest, registry.BlockCopperChest},
		{registry.BlockWaxedExposedCopperChest, registry.BlockWaxedOxidizedCopperChest, registry.BlockWaxedExposedCopperChest},
		{registry.BlockWaxedExposedCopperChest, registry.BlockExposedCopperChest, registry.BlockExposedCopperChest},
		{registry.BlockWaxedOxidizedCopperChest, registry.BlockWaxedCopperChest, registry.BlockWaxedCopperChest},
	} {
		if got := LeastOxidizedCopperChest(c.name, c.partner); got != c.want {
			t.Errorf("LeastOxidizedCopperChest(%d, %d) = %d, want %d", c.name, c.partner, got, c.want)
		}
	}
}

// TestACopperChestHalfAdoptsADanglingSingle: a half whose pair side holds a
// single copper chest of another stage adopts its block, keeping the half's
// own type. Only a setblock-made dangling half reaches this.
// vanilla: CopperChestBlock.updateShape
func TestACopperChestHalfAdoptsADanglingSingle(t *testing.T) {
	w, p := newPhysicsWorld(t)
	state := func(name, typ string) uint16 {
		s, _ := registry.FindState(name, map[string]string{"facing": "north", "type": typ, "waterlogged": "false"})
		return s
	}
	// Facing north, the left half at (0, 0)'s partner side is east.
	w.SetBlock(0, GroundLevel, 0, state("minecraft:weathered_copper_chest", "left"))
	setBlock(t, w, p, 1, GroundLevel, 0, state("minecraft:oxidized_copper_chest", "single"))
	if got, want := w.BlockAt(0, GroundLevel, 0), state("minecraft:oxidized_copper_chest", "left"); got != want {
		t.Errorf("the dangling half = %d, want %d (the single's block, still left)", got, want)
	}
}

// TestAMixedCopperChestPairFollowsTheChangedHalf: a single copper chest and a
// half of another stage set beside it pair, and the half receiving the update
// adopts the changed half's block, so the oxidized half carries the exposed
// one along. vanilla: CopperChestBlock.updateShape
func TestAMixedCopperChestPairFollowsTheChangedHalf(t *testing.T) {
	w, p := newPhysicsWorld(t)
	state := func(name, typ string) uint16 {
		s, _ := registry.FindState(name, map[string]string{"facing": "north", "type": typ, "waterlogged": "false"})
		return s
	}
	// Facing north, the left half at (-1, 0)'s partner stands east, where the
	// exposed single stands; the set half is the more oxidized of the two.
	w.SetBlock(0, GroundLevel, 0, state("minecraft:exposed_copper_chest", "single"))
	setBlock(t, w, p, -1, GroundLevel, 0, state("minecraft:oxidized_copper_chest", "left"))
	if got, want := w.BlockAt(-1, GroundLevel, 0), state("minecraft:oxidized_copper_chest", "left"); got != want {
		t.Errorf("the set half = %d, want %d (oxidized left)", got, want)
	}
	if got, want := w.BlockAt(0, GroundLevel, 0), state("minecraft:oxidized_copper_chest", "right"); got != want {
		t.Errorf("the single = %d, want %d (oxidized right)", got, want)
	}
}

// TestAWaxingCopperChestHalfCarriesItsPartner: waxing one half of an exposed
// pair leaves the other half waxed too. vanilla: CopperChestBlock.updateShape
func TestAWaxingCopperChestHalfCarriesItsPartner(t *testing.T) {
	w, p := newPhysicsWorld(t)
	state := func(name, typ string) uint16 {
		s, _ := registry.FindState(name, map[string]string{"facing": "north", "type": typ, "waterlogged": "false"})
		return s
	}
	w.SetBlock(0, GroundLevel, 0, state("minecraft:exposed_copper_chest", "left"))
	w.SetBlock(1, GroundLevel, 0, state("minecraft:exposed_copper_chest", "right"))
	setBlock(t, w, p, 0, GroundLevel, 0, state("minecraft:waxed_exposed_copper_chest", "left"))
	if got, want := w.BlockAt(1, GroundLevel, 0), state("minecraft:waxed_exposed_copper_chest", "right"); got != want {
		t.Errorf("the partner = %d, want %d (waxed exposed right)", got, want)
	}
}

// TestAWeatheringCopperChestHalfCarriesItsPartner: a weathering step on one
// half of a pair makes the other half adopt the weathered block.
// vanilla: CopperChestBlock.updateShape
func TestAWeatheringCopperChestHalfCarriesItsPartner(t *testing.T) {
	w, p := newPhysicsWorld(t)
	state := func(name, typ string) uint16 {
		s, _ := registry.FindState(name, map[string]string{"facing": "north", "type": typ, "waterlogged": "false"})
		return s
	}
	w.SetBlock(0, GroundLevel, 0, state("minecraft:exposed_copper_chest", "left"))
	w.SetBlock(1, GroundLevel, 0, state("minecraft:exposed_copper_chest", "right"))
	setBlock(t, w, p, 0, GroundLevel, 0, state("minecraft:weathered_copper_chest", "left"))
	if got, want := w.BlockAt(1, GroundLevel, 0), state("minecraft:weathered_copper_chest", "right"); got != want {
		t.Errorf("the partner = %d, want %d (weathered right)", got, want)
	}
}

// TestADoorHalfAdoptsItsPartnersBlock: the door shape update takes the
// partner's block along with its state. vanilla: DoorBlock.updateShape
func TestADoorHalfAdoptsItsPartnersBlock(t *testing.T) {
	w, p := newPhysicsWorld(t)
	door := func(name, half string) uint16 {
		s, _ := registry.FindState(name, map[string]string{"facing": "east", "half": half, "hinge": "right", "open": "true", "powered": "false"})
		return s
	}
	w.SetBlock(0, GroundLevel, 0, door("minecraft:copper_door", "lower"))
	w.SetBlock(0, GroundLevel+1, 0, door("minecraft:copper_door", "upper"))
	setBlock(t, w, p, 0, GroundLevel+1, 0, door("minecraft:waxed_copper_door", "upper"))
	if got, want := w.BlockAt(0, GroundLevel, 0), door("minecraft:waxed_copper_door", "lower"); got != want {
		t.Errorf("the lower half = %d, want %d (waxed)", got, want)
	}
}

// TestAnOpenTrapdoorStaysOpenThroughANeighbourChange: only a change of
// power moves a door, trapdoor or gate. vanilla: TrapDoorBlock.neighborChanged
func TestAnOpenTrapdoorStaysOpenThroughANeighbourChange(t *testing.T) {
	w, p := newPhysicsWorld(t)
	open, _ := registry.FindState("minecraft:oak_trapdoor", map[string]string{"facing": "north", "half": "bottom", "open": "true", "powered": "false", "waterlogged": "false"})
	w.SetBlock(0, GroundLevel, 0, open)
	setBlock(t, w, p, 1, GroundLevel, 0, registry.BlockStates["minecraft:stone"])
	if got := w.BlockAt(0, GroundLevel, 0); got != open {
		t.Errorf("the trapdoor after its neighbour changed = %d, want %d (still open)", got, open)
	}
}
