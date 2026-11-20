package mobs

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestPackTradedEquipmentTagLimitsEnchantedArmor checks that an armorer's
// built-in enchanted armor offers draw from the loaded on_traded_equipment
// tag: with efficiency and feather falling in it, only feather falling can
// apply to armor, and efficiency never shows.
// vanilla: EnchantWithLevelsFunction.run, RegistryCodecs.homogeneousList
func TestPackTradedEquipmentTagLimitsEnchantedArmor(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil); registry.ApplyPackTradeSets(nil) })
	featherFalling, _ := registry.EnchantmentID("minecraft:feather_falling")
	_, stacks := armorerOfferStacks(t, map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/minecraft/tags/enchantment/on_traded_equipment.json": `{"replace":true,"values":["minecraft:efficiency","minecraft:feather_falling"]}`,
		"data/minecraft/trade_set/armorer/level_1.json":            `{"amount":2,"trades":"#minecraft:armorer/level_4"}`,
	})
	enchanted := 0
	for _, s := range stacks {
		for id := range s.Enchantments {
			enchanted++
			if id != featherFalling {
				t.Fatalf("armor holds enchantment %d, want only feather falling", id)
			}
		}
	}
	if enchanted == 0 {
		t.Fatalf("no enchanted armor offered: %v", stacks)
	}
}
