package combat

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func lootDamageEnchant(requirements string) string {
	return `{"anvil_cost":1,"description":{"text":"Loot"},"max_level":2,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["mainhand"],"supported_items":"#minecraft:enchantable/weapon",
	"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":10},"requirements":` + requirements + `}]}}`
}

// TestPackEnchantmentLootRequirements tests an effect whose requirements are
// loot conditions the enchantment codec has no variant for: an attacker's
// sneaking flag and the NBT tags of the hurt body.
// vanilla: Enchantment.CONDITIONAL_CODEC, LootItemCondition.DIRECT_CODEC
func TestPackEnchantmentLootRequirements(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/pk/enchantment/sneaky.json": lootDamageEnchant(
			`{"condition":"minecraft:entity_properties","entity":"attacker","predicate":{"minecraft:flags":{"is_sneaking":true}}}`),
		"data/pk/enchantment/tagged.json": lootDamageEnchant(
			`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Tags:[\"a\"]}"}}`),
		"data/pk/enchantment/level.json": lootDamageEnchant(
			`{"condition":"minecraft:float_value_check","value":{"type":"minecraft:enchantment_level","amount":{"type":"minecraft:linear","base":1,"per_level_above_first":1}},"test":{"min":2}}`),
		"data/pk/enchantment/overworld.json": lootDamageEnchant(
			`{"condition":"minecraft:location_check","predicate":{"dimension":"minecraft:overworld"}}`),
		"data/pk/enchantment/nether.json": lootDamageEnchant(
			`{"condition":"minecraft:location_check","predicate":{"dimension":"minecraft:the_nether"}}`),
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Smith"
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.NightWorld(srv, c)
	zombieID := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, 0.5, 4374)
	srv.SetMobNoAI(zombieID)
	servertest.HoldTicks(t, srv)
	iron := servertest.MustItemID(t, "minecraft:iron_sword")
	base := 1 + float64(registry.AttackDamage(iron))
	hit := func(enchantment string, level ...int32) float64 {
		t.Helper()
		lv := int32(1)
		if len(level) > 0 {
			lv = level[0]
		}
		srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", enchantment, lv))
		srv.SetMobHealth(zombieID, 20)
		servertest.AwaitFullSwing(t, srv, name)
		c.Attack(zombieID)
		c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
		health, _ := srv.MobHealth(zombieID)
		// The zombie's armour of 2 takes 0.4 of every 25 points off the blow.
		return (20 - health) / (1 - 0.4/25)
	}
	check := func(what string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 0.05 {
			t.Errorf("%s: blow deals %v before armour, want %v", what, got, want)
		}
	}
	check("standing attacker", hit("pk:sneaky"), base)
	c.SendSneak(true)
	c.Settle()
	check("sneaking attacker", hit("pk:sneaky"), base+10)
	c.SendSneak(false)
	c.Settle()
	check("untagged victim", hit("pk:tagged"), base)
	c.ChatCommand("tag @e[type=zombie] add a")
	c.Settle()
	check("tagged victim", hit("pk:tagged"), base+10)
	check("enchantment level 1", hit("pk:level", 1), base)
	check("enchantment level 2", hit("pk:level", 2), base+10)
	check("dimension overworld", hit("pk:overworld"), base+10)
	check("dimension nether", hit("pk:nether"), base)
}
