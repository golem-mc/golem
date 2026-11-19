package combat

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

const fastBoots = `{"anvil_cost":1,"description":{"text":"Fast"},"max_level":1,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["feet"],"supported_items":"#minecraft:enchantable/foot_armor",
	"effects":{"minecraft:location_changed":[{"effect":{"type":"minecraft:attribute",
	"attribute":"minecraft:movement_speed","id":"pack:fast","amount":0.1,"operation":"add_value"}}]}}`

const glowBoots = `{"anvil_cost":1,"description":{"text":"Glow"},"max_level":1,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["feet"],"supported_items":"#minecraft:enchantable/foot_armor",
	"effects":{"minecraft:tick":[{"effect":{"type":"minecraft:apply_mob_effect","to_apply":"minecraft:glowing",
	"min_duration":2,"max_duration":2,"min_amplifier":0,"max_amplifier":0}}]}}`

const hasteHelmet = `{"anvil_cost":1,"description":{"text":"Haste"},"max_level":1,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["head"],"supported_items":"#minecraft:enchantable/head_armor",
	"effects":{"minecraft:location_changed":[{"effect":{"type":"minecraft:apply_mob_effect","to_apply":"minecraft:haste",
	"min_duration":10,"max_duration":10,"min_amplifier":0,"max_amplifier":0}}]}}`

func hasSuffixModifier(e servertest.AttrEntry, id string) bool {
	for _, m := range e.Modifiers {
		if strings.HasPrefix(m.Id, id) {
			return true
		}
	}
	return false
}

// A pack enchantment's location_changed attribute effect puts its modifier
// on the wearer after a block move and takes it off with the boots.
// vanilla: EnchantmentHelper.runLocationChangedEffects, stopLocationBasedEffects
func TestPackLocationChangedAttributeFollowsTheBoots(t *testing.T) {
	const name = "Runner"
	cfg := servertest.BowTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/fast.json": fastBoots})
	srv, c, id := servertest.JoinAs(t, cfg, name)
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", "pk:fast", 1))
	c.MoveToGround(1.5, 64, 0.5, true)
	servertest.RunTicks(t, srv, 2)
	on := func(e servertest.AttrEntry) bool { return hasSuffixModifier(e, "pack:fast") }
	e := c.AwaitAttributes(id, servertest.AttrMovementSpeed, on)[servertest.AttrMovementSpeed]
	for _, m := range e.Modifiers {
		if strings.HasPrefix(m.Id, "pack:fast") && float32(m.Amount) != 0.1 {
			t.Errorf("modifier %s amount = %v, want 0.1", m.Id, m.Amount)
		}
	}
	srv.GiveSlot(name, player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:unbreaking", 1))
	servertest.RunTicks(t, srv, 2)
	c.AwaitAttributes(id, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !on(e) })
}

// A pack enchantment's tick effect runs every tick on the wearer.
// vanilla: EnchantmentHelper.tickEffects
func TestPackTickEffectGlowsTheWearer(t *testing.T) {
	const name = "Glower"
	cfg := servertest.BowTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/glow.json": glowBoots})
	srv, _, _ := servertest.JoinAs(t, cfg, name)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", "pk:glow", 1))
	servertest.RunTicks(t, srv, 2)
	if _, _, ok := srv.PlayerEffect(name, "minecraft:glowing"); !ok {
		t.Error("the wearer does not glow two ticks after putting the boots on")
	}
}
