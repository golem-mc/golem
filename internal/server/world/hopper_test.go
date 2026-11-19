package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestHopperMouthStates(t *testing.T) {
	for state := range uint16(registry.StateCount) {
		name, _, known := registry.BlockStateName(state)
		wantMouth := false
		switch registry.BlockOf(state).Type() {
		case registry.BlockTypeDecoratedPot, registry.BlockTypeShelf, registry.BlockTypeCopperChest, registry.BlockTypeWeatheringCopperChest:
			wantMouth = true
		}
		if got := ContainerMouthBlock(state); got != wantMouth {
			t.Fatalf("state %d (%s): mouth = %v, want %v", state, name, got, wantMouth)
		}
		for _, full := range []bool{false, true} {
			wantBlocked := full && (!known || name != "minecraft:bee_nest" && name != "minecraft:beehive")
			if got := HopperBlockedBy(state, full); got != wantBlocked {
				t.Fatalf("state %d (%s), full=%v: blocked = %v, want %v", state, name, full, got, wantBlocked)
			}
		}
	}
	// Invalid states retain the unknown block's conservative full-cell guard.
	if ContainerMouthBlock(^uint16(0)) || !HopperBlockedBy(^uint16(0), true) || HopperBlockedBy(^uint16(0), false) {
		t.Fatal("invalid state did not retain the mouth defaults")
	}
}
