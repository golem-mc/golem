package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestAirAndTurtleEggStateVariants(t *testing.T) {
	for id := range 65536 {
		state := uint16(id)
		name, _, known := registry.BlockStateName(state)
		air := !known || name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air"
		if IsAirState(state) != air {
			t.Fatalf("IsAirState(%d, %s) = %v, want %v", state, name, IsAirState(state), air)
		}
		if got := IsKnownAirState(state); got != (known && air) {
			t.Fatalf("IsKnownAirState(%d, %s) = %v, want %v", state, name, got, known && air)
		}
		egg := name == "minecraft:turtle_egg"
		if IsTurtleEgg(state) != egg {
			t.Fatalf("IsTurtleEgg(%d, %s) = %v, want %v", state, name, IsTurtleEgg(state), egg)
		}
	}
}
