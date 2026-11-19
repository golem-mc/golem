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
