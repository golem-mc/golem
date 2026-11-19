package advancement

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkInventoryChanged fires inventory_changed over the vanilla tree for
// a player holding a survival hotbar, after the first firing has awarded what
// it can: the steady cost of each inventory change.
func BenchmarkInventoryChanged(b *testing.B) {
	var held []int32
	for _, name := range []string{"minecraft:cobblestone", "minecraft:oak_log", "minecraft:oak_planks", "minecraft:stick",
		"minecraft:torch", "minecraft:iron_ingot", "minecraft:coal", "minecraft:bread", "minecraft:stone_pickaxe"} {
		id, ok := registry.ItemID(name)
		if !ok {
			b.Fatalf("no item %s", name)
		}
		held = append(held, id)
	}
	p := NewProgress(Vanilla())
	now := time.Now()
	p.InventoryChanged(Inventory{Held: held, Changed: held}, now)
	for b.Loop() {
		p.InventoryChanged(Inventory{Held: held, Changed: held}, now)
	}
}
