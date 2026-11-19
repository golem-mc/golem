package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestFuelBurnTimeMatchesVanilla checks a sample of the fuel table against
// FuelValues.vanillaBurnTimes with its base unit of 200, transcribed by hand
// from the decompiled server, so a generator that misreads the chain
// shows up here.
func TestFuelBurnTimeMatchesVanilla(t *testing.T) {
	for name, want := range map[string]int32{
		"minecraft:lava_bucket":        20000, // baseUnit * 100
		"minecraft:coal_block":         16000, // baseUnit * 8 * 10
		"minecraft:blaze_rod":          2400,  // baseUnit * 12
		"minecraft:coal":               1600,
		"minecraft:charcoal":           1600,
		"minecraft:oak_log":            300, // #logs, baseUnit * 3 / 2
		"minecraft:stripped_birch_log": 300,
		"minecraft:bamboo_block":       300, // #bamboo_blocks
		"minecraft:oak_planks":         300,
		"minecraft:oak_slab":           150, // #wooden_slabs, baseUnit * 3 / 4
		"minecraft:bamboo_mosaic_slab": 150,
		"minecraft:oak_shelf":          300,
		"minecraft:crafting_table":     300,
		"minecraft:oak_sign":           200, // #signs, baseUnit
		"minecraft:oak_hanging_sign":   800, // #hanging_signs, baseUnit * 4
		"minecraft:wooden_pickaxe":     200,
		"minecraft:wooden_spear":       200,
		"minecraft:oak_door":           200,
		"minecraft:oak_boat":           1200, // #boats, baseUnit * 6
		"minecraft:white_wool":         100,  // #wool, baseUnit / 2
		"minecraft:stick":              100,
		"minecraft:oak_sapling":        100,
		"minecraft:bowl":               100,
		"minecraft:white_carpet":       67,   // 1 + baseUnit / 3
		"minecraft:dried_kelp_block":   4001, // 1 + baseUnit * 20
		"minecraft:bamboo":             50,   // baseUnit / 4
		"minecraft:scaffolding":        50,
		"minecraft:leaf_litter":        100,
		// remove(#non_flammable_wood) takes the nether woods back out.
		"minecraft:crimson_planks": 0,
		"minecraft:warped_stem":    0,
		"minecraft:crimson_slab":   0,
		// Not in the chain at all.
		"minecraft:stone":  0,
		"minecraft:bucket": 0,
	} {
		stack := protocol.ItemStack{ItemID: itemID(t, name), Count: 1}
		if got := registry.FuelBurnTicks(stack.ItemID); got != want {
			t.Errorf("FuelBurnTicks(%s) = %d, want %d", name, got, want)
		}
	}
	if IsCookingFuel(protocol.ItemStack{}) {
		t.Error("an empty stack is fuel")
	}
}
