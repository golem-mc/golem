package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func smithingStack(t *testing.T, name string) protocol.ItemStack {
	t.Helper()
	item, ok := ItemByName(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return protocol.ItemStack{ItemID: int32(item), Count: 1}
}

// TestSmithingKeepsOnlyRemovalsTheResultHasDefaultsFor upgrades a diamond
// pickaxe that removes max_damage and food: the netherite pickaxe has a
// max_damage to remove but no food.
func TestSmithingKeepsOnlyRemovalsTheResultHasDefaultsFor(t *testing.T) {
	t.Parallel()
	base := smithingStack(t, "minecraft:diamond_pickaxe")
	if !base.Remove(protocol.DataComponentMaxDamage) || !base.Remove(protocol.DataComponentFood) {
		t.Fatal("components not removable")
	}
	result := SmithingResult(smithingStack(t, "minecraft:netherite_upgrade_smithing_template"), base,
		smithingStack(t, "minecraft:netherite_ingot"))
	if result.Empty() {
		t.Fatal("no upgrade result")
	}
	if !result.Removes(protocol.DataComponentMaxDamage) || result.Removes(protocol.DataComponentFood) {
		t.Errorf("result removes %v, want only max_damage", result.RemovedNames())
	}
}

// TestResultFromDropsRemovalsOfAbsentDefaults turns a pickaxe removing
// max_damage into a stick, which has no max_damage.
func TestResultFromDropsRemovalsOfAbsentDefaults(t *testing.T) {
	t.Parallel()
	base := smithingStack(t, "minecraft:diamond_pickaxe")
	base.Remove(protocol.DataComponentMaxDamage)
	stick := smithingStack(t, "minecraft:stick")
	if got := resultFrom("none", base, stick.ItemID, 1); got.Removes(protocol.DataComponentMaxDamage) {
		t.Errorf("the stick removes %v", got.RemovedNames())
	}
}
