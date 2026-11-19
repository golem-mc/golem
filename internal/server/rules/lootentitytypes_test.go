package rules

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestCanBreakIsHeldByProjectilesOnly checks that can_break reads the saved
// field of a projectile kind and nothing else.
// vanilla: Projectile.applyImplicitComponent
func TestCanBreakIsHeldByProjectilesOnly(t *testing.T) {
	saved := func() nbt.Compound { return nbt.Compound{"can_break": nbt.String("x")} }
	for kind, want := range map[string]bool{
		"minecraft:snowball": true, "minecraft:arrow": true,
		"minecraft:area_effect_cloud": false, "minecraft:zombie": false,
	} {
		_, got := entityTagComponent(LootEntity{Type: kind, NBT: saved}, "minecraft:can_break")
		if got != want {
			t.Errorf("%s holds can_break = %v, want %v", kind, got, want)
		}
	}
}
