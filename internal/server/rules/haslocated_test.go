package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestHasLocatedFollowsAPackReload gives protection a location_changed entry
// through a pack: HasLocated flips for a stack carrying it and the generation
// moves, and both go back when the pack is removed. The registry tables are
// process-wide, so the test is not parallel.
func TestHasLocatedFollowsAPackReload(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackEnchantments(nil) })
	id := registry.MustID("minecraft:enchantment", "minecraft:protection")
	def, ok := registry.EnchantmentEffectsByID(id)
	if !ok {
		t.Fatal("no protection definition")
	}
	var stack protocol.ItemStack
	stack.SetEnchantment(false, id, 1)
	if HasLocated(stack) {
		t.Fatal("protection has a located entry before the pack")
	}
	before := registry.EnchantmentGeneration()

	packed := *def
	packed.Effects.LocationChanged = []registry.EnchLocated{{}}
	registry.ApplyPackEnchantments(map[string]registry.EnchantmentDef{"minecraft:protection": packed})
	if !HasLocated(stack) {
		t.Error("HasLocated stayed false after a pack added a location_changed entry")
	}
	during := registry.EnchantmentGeneration()
	if during == before {
		t.Error("generation did not change on reload")
	}

	registry.ApplyPackEnchantments(nil)
	if HasLocated(stack) {
		t.Error("HasLocated stayed true after the pack was removed")
	}
	if registry.EnchantmentGeneration() == during {
		t.Error("generation did not change when the pack was removed")
	}
}
