package combat

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
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

// A mob wearing a pack enchantment runs its tick entity effects too.
// vanilla: EnchantmentHelper.tickEffects
func TestPackTickEffectGlowsTheMob(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/glow.json": glowBoots})
	srv, c, _ := servertest.JoinAs(t, cfg, "Watcher")
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(zombie)
	servertest.HoldTicks(t, srv)
	srv.SetMobWorn(zombie, 0, servertest.EnchantedStack(t, "minecraft:iron_boots", "pk:glow", 1))
	servertest.RunTicks(t, srv, 3)
	if _, _, ok := srv.MobEffectOf(zombie, "minecraft:glowing"); !ok {
		t.Error("the zombie does not glow after three ticks in the boots")
	}
}

// Changing one worn slot runs only that slot's location_changed entries; the
// others wait for a block move.
// vanilla: LivingEntity.collectEquipmentChanges, onChangedBlock
func TestPackLocationChangedRunsOnlyTheChangedSlot(t *testing.T) {
	const name = "Dresser"
	cfg := servertest.BowTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/haste.json": hasteHelmet})
	srv, c, _ := servertest.JoinAs(t, cfg, name)
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.ArmorSlot, servertest.EnchantedStack(t, "minecraft:iron_helmet", "pk:haste", 1))
	servertest.RunTicks(t, srv, 2)
	before, _, ok := srv.PlayerEffect(name, "minecraft:haste")
	if !ok {
		t.Fatal("the helmet's location_changed effect did not run when it was put on")
	}
	servertest.RunTicks(t, srv, 20)
	srv.GiveSlot(name, player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:unbreaking", 1))
	servertest.RunTicks(t, srv, 2)
	after, _, ok := srv.PlayerEffect(name, "minecraft:haste")
	if !ok || after >= before {
		t.Errorf("haste duration %d after the boots, was %d: the helmet's entry ran again", after, before)
	}
}

// A zombie in boots with a pack location_changed movement_speed modifier gets
// the modifier on its attribute map, which its pace reads.
// vanilla: EnchantmentHelper.runLocationChangedEffects
func TestPackLocationChangedAttributeSpeedsTheMob(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/fast.json": fastBoots})
	srv, c, _ := servertest.JoinAs(t, cfg, "Watcher")
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(zombie)
	bare := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, 2.5, 4374)
	srv.SetMobNoAI(bare)
	servertest.HoldTicks(t, srv)
	srv.SetMobWorn(zombie, 0, servertest.EnchantedStack(t, "minecraft:iron_boots", "pk:fast", 1))
	servertest.RunTicks(t, srv, 3)
	if got, base := srv.MobPace(zombie), srv.MobPace(bare); got <= base {
		t.Errorf("pace %v with the fast boots, %v bare: the modifier is not read", got, base)
	}
	c.AwaitAttributes(zombie, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return hasSuffixModifier(e, "pack:fast") })
	srv.SetMobWorn(zombie, 0, servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:unbreaking", 1))
	servertest.RunTicks(t, srv, 3)
	c.AwaitAttributes(zombie, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !hasSuffixModifier(e, "pack:fast") })
}

// An armor stand in boots with a pack location_changed attribute effect holds
// the modifier on its attribute map and tells its watchers.
// vanilla: EnchantmentHelper.runLocationChangedEffects
func TestPackLocationChangedAttributeFollowsTheStandsBoots(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/fast.json": fastBoots})
	srv, c, _ := servertest.JoinAs(t, cfg, "Watcher")
	c.MoveToGround(.5, 64, .5, true)
	servertest.HoldTicks(t, srv)
	stand := srv.SpawnArmorStand(2.5, 64, .5)
	srv.SetArmorStandStack(stand, entity.StandFeet, servertest.EnchantedStack(t, "minecraft:iron_boots", "pk:fast", 1))
	servertest.RunTicks(t, srv, 3)
	c.AwaitAttributes(stand, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return hasSuffixModifier(e, "pack:fast") })
	srv.SetArmorStandStack(stand, entity.StandFeet, servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:unbreaking", 1))
	servertest.RunTicks(t, srv, 3)
	c.AwaitAttributes(stand, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool { return !hasSuffixModifier(e, "pack:fast") })
}
