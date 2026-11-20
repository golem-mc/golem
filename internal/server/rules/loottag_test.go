package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Built-in enchant functions read their tag from the loaded tags, so a pack
// that replaces one changes every built-in table that names it.
func TestBuiltinEnchantFunctionsFollowLoadedTags(t *testing.T) {
	unbreaking, _ := registry.EnchantmentID("minecraft:unbreaking")
	featherFalling, _ := registry.EnchantmentID("minecraft:feather_falling")
	replace := func(tag, member string) {
		registry.ApplyPackTags(registry.PackTags{"minecraft:enchantment": {
			tag: {Replace: true, Entries: []registry.PackTagEntry{{ID: member, Required: true}}},
		}})
	}
	t.Cleanup(func() { registry.ApplyPackTags(nil) })

	replace("minecraft:on_random_loot", "minecraft:unbreaking")
	books := 0
	for seed := range int64(400) {
		ctx := LootContext{}
		for _, s := range RollLoot("minecraft:chests/simple_dungeon", &ctx, rand.New(rand.NewSource(seed))) {
			if len(s.StoredEnchantments) == 0 {
				continue
			}
			books++
			for id := range s.StoredEnchantments {
				if id != unbreaking {
					t.Fatalf("seed %d: enchanted book holds %d, want only unbreaking", seed, id)
				}
			}
		}
	}
	if books == 0 {
		t.Fatal("no enchanted book dropped in 400 rolls")
	}

	registry.ApplyPackTags(nil)
	replace("minecraft:on_traded_equipment", "minecraft:feather_falling")
	var trade registry.VillagerTrade
	for _, level := range []string{"armorer/level_4", "armorer/level_5"} {
		set, _ := registry.FindTradeSet(level)
		for _, tr := range set.Trades {
			if len(tr.Modifiers) > 0 && tr.Gives.Item == "minecraft:diamond_boots" {
				trade = tr
			}
		}
	}
	if len(trade.Modifiers) == 0 {
		t.Fatal("enchanted boots trade not found")
	}
	boots, _ := registry.ItemID("minecraft:diamond_boots")
	enchanted := 0
	for seed := range int64(20) {
		got, _ := ApplyTradeModifiers(trade.Modifiers, protocol.ItemStack{ItemID: boots, Count: 1}, rand.New(rand.NewSource(seed)), nil)
		for id := range got.Enchantments {
			enchanted++
			if id != featherFalling {
				t.Fatalf("seed %d: boots hold %d, want only featherFalling", seed, id)
			}
		}
	}
	if enchanted == 0 {
		t.Fatal("no enchanted boots rolled")
	}
}
