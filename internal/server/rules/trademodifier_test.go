package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestTradeEnchantmentsChargeOnlyInATradeContext(t *testing.T) {
	book, _ := registry.ItemID("minecraft:book")
	mending, _ := registry.EnchantmentID("minecraft:mending")
	fn := registry.LootEnchantRandomly{Options: []string{"minecraft:mending"}, IncludeAdditionalCost: true}
	for _, enabled := range []bool{false, true} {
		fn.IncludeAdditionalCost = enabled
		rng := rand.New(rand.NewSource(7))
		rng.Int63() // consume the choice of the only enchantment
		want := int32(0)
		if enabled {
			want = 2 + int32(rng.Intn(15)) + 3
		}
		got, cost := ApplyTradeModifiers([]registry.LootFunction{fn}, protocol.ItemStack{ItemID: book, Count: 1}, rand.New(rand.NewSource(7)), nil)
		if cost != want || got.StoredEnchantments[mending] != 1 {
			t.Fatalf("enabled %v: cost %d, enchantments %v; want %d and mending I", enabled, cost, got.StoredEnchantments, want)
		}
		outside := ApplyItemModifier(fn, protocol.ItemStack{ItemID: book, Count: 1}, rand.New(rand.NewSource(7)), nil)
		if outside.StoredEnchantments[mending] != 1 {
			t.Fatal("enchantment flag changed a non-trade modifier")
		}
	}
}

func TestTradeEquipmentChargesTheRolledLevelsAndFiltersFailures(t *testing.T) {
	sword, _ := registry.ItemID("minecraft:diamond_sword")
	fn := registry.LootEnchantWithLevels{Levels: registry.LootConstant{Value: 19},
		Options: []string{"minecraft:sharpness"}, IncludeAdditionalCost: true}
	filter := registry.LootFiltered{Predicate: registry.LootItemPredicate{Items: []string{"minecraft:diamond_sword"},
		Enchantments: []registry.LootEnchantments{{}}}, OnFail: registry.LootDiscard{}}
	for _, options := range [][]string{fn.Options, {}} {
		fn.Options = options
		got, cost := ApplyTradeModifiers([]registry.LootFunction{fn, filter}, protocol.ItemStack{ItemID: sword, Count: 1}, rand.New(rand.NewSource(4)), nil)
		if cost != 19 {
			t.Errorf("cost %d, want 19", cost)
		}
		if (len(options) == 0) != got.Empty() {
			t.Errorf("options %v: result %+v", options, got)
		}
	}
}

func TestTradeFilterReadsStoredEnchantmentsAndItsConditions(t *testing.T) {
	book, _ := registry.ItemID("minecraft:enchanted_book")
	mending, _ := registry.EnchantmentID("minecraft:mending")
	filter := registry.LootFiltered{Predicate: registry.LootItemPredicate{
		StoredEnchantments: []registry.LootEnchantments{{Enchantments: []string{"minecraft:mending"}, Min: 1, Max: 1}},
	}, OnFail: registry.LootDiscard{}}
	for _, stored := range []bool{false, true} {
		stack := protocol.ItemStack{ItemID: book, Count: 1, Enchantments: map[int32]int32{mending: 1}}
		if stored {
			stack.StoredEnchantments = stack.Enchantments
		}
		got, _ := ApplyTradeModifiers([]registry.LootFunction{filter}, stack, rand.New(rand.NewSource(1)), nil)
		if got.Empty() == stored {
			t.Errorf("stored %v: result %+v", stored, got)
		}
	}
	filter.Conditions = []registry.LootCondition{registry.LootRandomChance{Chance: registry.LootConstant{}}}
	got, _ := ApplyTradeModifiers([]registry.LootFunction{filter}, protocol.ItemStack{ItemID: book, Count: 1}, rand.New(rand.NewSource(1)), nil)
	if got.Empty() {
		t.Fatal("failed condition ran the filter")
	}
}
