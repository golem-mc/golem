package rules_test

import (
	"math/rand"
	"os"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ScaleExponentially.process: 3 * 2^level is 12 at level 2.
func TestPackExponentialEffectScalesByPower(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackEnchantments(nil) })
	data, err := os.ReadFile("../datapack/testdata/enchantment/exponential.json")
	if err != nil {
		t.Fatal(err)
	}
	def, dropped, err := datapack.DecodeEnchantment("minecraft:lure", data)
	if err != nil || len(dropped) != 0 {
		t.Fatalf("decode: %v, dropped %v", err, dropped)
	}
	registry.ApplyPackEnchantments(map[string]registry.EnchantmentDef{"minecraft:lure": def})
	id, _ := registry.EnchantmentID("minecraft:lure")
	rod := protocol.ItemStack{ItemID: int32(registry.ItemFishingRod), Count: 1, Enchantments: map[int32]int32{id: 2}}
	// base 2, exponent = level (linear 1, +1 per level above first) -> 3 * 2^2
	if got := rules.FishingLuck(rod); got != 12 {
		t.Errorf("exponential at level 2 = %d, want 12", got)
	}
}

// vanilla: LootItemRandomChanceWithEnchantedBonusCondition.test
func TestEnchantedChanceAcceptsAnyLevelValue(t *testing.T) {
	data, err := os.ReadFile("../datapack/testdata/loot/enchanted_lookup.json")
	if err != nil {
		t.Fatal(err)
	}
	table, err := datapack.DecodeLootTable(data)
	if err != nil {
		t.Fatal(err)
	}
	ctx := func(level int32) *rules.LootContext {
		return &rules.LootContext{Attacker: rules.LootEntity{Type: "minecraft:player", Mainhand: rules.LootTool{
			Item: "minecraft:iron_sword", Count: 1, Enchantments: map[string]int32{"minecraft:looting": level}}}}
	}
	rng := rand.New(rand.NewSource(5))
	for range 50 {
		// lookup [0.5, 1]: level 2 is chance 1, so the diamond always drops
		if len(rules.RollLootTable("pk:t", table, ctx(2), rng)) != 1 {
			t.Fatal("level 2 did not always pass a chance of 1")
		}
		// past the values the fallback 0 holds
		if len(rules.RollLootTable("pk:t", table, ctx(3), rng)) != 0 {
			t.Fatal("level 3 passed a chance of 0")
		}
	}
}

func TestInlineNestedLootTableRolls(t *testing.T) {
	data, err := os.ReadFile("../datapack/testdata/loot/inline_nested.json")
	if err != nil {
		t.Fatal(err)
	}
	table, err := datapack.DecodeLootTable(data)
	if err != nil {
		t.Fatal(err)
	}
	stacks := rules.RollLootTable("pk:t", table, &rules.LootContext{}, rand.New(rand.NewSource(1)))
	total := int32(0)
	for _, s := range stacks {
		total += s.Count
	}
	if total != 2 {
		t.Errorf("inline table dropped %d sticks, want 2", total)
	}
}
