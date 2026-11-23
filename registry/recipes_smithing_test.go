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
// max_damage, damage and attack_animation into a stick, which has only an
// attack_animation default.
func TestResultFromDropsRemovalsOfAbsentDefaults(t *testing.T) {
	t.Parallel()
	base := smithingStack(t, "minecraft:diamond_pickaxe")
	for _, c := range []int32{protocol.DataComponentMaxDamage, protocol.DataComponentDamage, protocol.DataComponentAttackAnimation} {
		if !base.Remove(c) {
			t.Fatalf("component %d not removable", c)
		}
	}
	stick := smithingStack(t, "minecraft:stick")
	got := resultFrom("none", base, stick.ItemID, 1)
	if got.Removes(protocol.DataComponentMaxDamage) || got.Removes(protocol.DataComponentDamage) ||
		!got.Removes(protocol.DataComponentAttackAnimation) {
		t.Errorf("the stick removes %v, want only attack_animation", got.RemovedNames())
	}
}

// TestSmithingKeepsAttackAnimationRemoval: every item has an attack_animation
// default, whether or not its props carry one.
func TestSmithingKeepsAttackAnimationRemoval(t *testing.T) {
	t.Parallel()
	base := smithingStack(t, "minecraft:diamond_pickaxe")
	if !base.Remove(protocol.DataComponentAttackAnimation) {
		t.Fatal("component not removable")
	}
	result := SmithingResult(smithingStack(t, "minecraft:netherite_upgrade_smithing_template"), base,
		smithingStack(t, "minecraft:netherite_ingot"))
	if !result.Removes(protocol.DataComponentAttackAnimation) {
		t.Errorf("result removes %v, want attack_animation", result.RemovedNames())
	}
}
