package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestReactKindOf(t *testing.T) {
	for name, want := range map[string]reactKind{
		"minecraft:redstone_wire":       reactWire,
		"minecraft:redstone_wall_torch": reactTorch,
		"minecraft:powered_rail":        reactRail,
		"minecraft:oak_button":          reactPlateOrButton,
		"minecraft:waxed_copper_bulb":   reactCopperBulb,
		"minecraft:sticky_piston":       reactPiston,
		"minecraft:piston_head":         reactPistonHead,
		"minecraft:oak_fence_gate":      reactDoorLike,
		"minecraft:dropper":             reactDispenser,
		"minecraft:stone":               reactNone,
	} {
		b, ok := registry.BlockByName(name)
		if !ok {
			t.Fatalf("no block %s", name)
		}
		if got := reactKindOf(b.DefaultState()); got != want {
			t.Errorf("%s: kind %d, want %d", name, got, want)
		}
	}
}
