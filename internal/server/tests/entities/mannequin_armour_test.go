package entities

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

const diamondMannequinEquipment = `head:{id:"diamond_helmet"},chest:{id:"diamond_chestplate"},legs:{id:"diamond_leggings"},feet:{id:"diamond_boots"}`

// vanilla: LivingEntity.getDamageAfterArmorAbsorb, CombatRules.getDamageAfterAbsorb
func TestAMannequinsEquipmentReducesDamage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, equipment, damageType string
		amount                      string
		health                      float32
	}{
		{"leather", `head:{id:"leather_helmet"},feet:{id:"leather_boots"}`, "lava", "4", 16.064},
		{"diamond_toughness", diamondMannequinEquipment, "player_attack", "10", 17},
		{"iron_without_toughness", `head:{id:"iron_helmet"},chest:{id:"iron_chestplate"},legs:{id:"iron_leggings"},feet:{id:"iron_boots"}`, "arrow", "10", 14},
		{"fall_bypasses", diamondMannequinEquipment, "fall", "10", 10},
		{"generic_bypasses", diamondMannequinEquipment, "generic", "10", 10},
		{"on_fire_bypasses", diamondMannequinEquipment, "on_fire", "4", 16},
		{"no_equipment", "", "player_attack", "10", 10},
		{"wrong_slot", `head:{id:"diamond_boots"}`, "lava", "4", 16},
		{"held_modifiers", `offhand:{id:"stick",components:{"minecraft:attribute_modifiers":[{id:"test:armor",type:"armor",amount:3.9d,operation:"add_value",slot:"offhand"},{id:"test:multiple",type:"armor",amount:1.0d,operation:"add_multiplied_total",slot:"offhand"},{id:"test:toughness",type:"armor_toughness",amount:8.0d,operation:"add_value",slot:"offhand"}]}}`, "lava", "4", 16.96},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "ArmourWatcher")
			c.ChatCommand(`summon mannequin 1.5 64 1.5 {immovable:1b,equipment:{` + tc.equipment + `}}`)
			id := awaitMannequin(t, c, srv).id
			c.ChatCommand("damage @e[type=mannequin,limit=1] " + tc.amount + " " + tc.damageType)
			seen := readMannequinHurt(c, srv, id, 3, func(s *mannequinHurtSeen) bool { return s.healthSent })
			if !seen.healthSent || seen.health != tc.health {
				t.Errorf("health sent=%v value=%v, want %v", seen.healthSent, seen.health, tc.health)
			}
		})
	}
}

// The boots' custom modifiers replace their default armor modifier.
// Values recorded from the vanilla 26.3 client after three lava-type hits.
func TestAMannequinsArmourReducesRepeatedLavaDamage(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "LavaWatcher")
	c.ChatCommand(`summon mannequin 1.5 64 1.5 {immovable:1b,equipment:{head:{id:"leather_helmet",components:{"minecraft:enchantments":{"minecraft:respiration":3}}},feet:{id:"leather_boots",components:{"minecraft:attribute_modifiers":[{id:"test:unlit",type:"burning_time",amount:-1.0d,operation:"add_value",slot:"feet"}]}}}}`)
	id := awaitMannequin(t, c, srv).id
	for i, health := range []float32{16.032, 12.063999, 8.095999} {
		if i > 0 {
			servertest.RunTicks(t, srv, 11)
		}
		c.ChatCommand("damage @e[type=mannequin,limit=1] 4 lava")
		seen := readMannequinHurt(c, srv, id, 3, func(s *mannequinHurtSeen) bool { return s.healthSent })
		if !seen.healthSent || seen.health != health {
			t.Errorf("health sent=%v value=%v, want %v", seen.healthSent, seen.health, health)
		}
	}
}

// The window compares raw hits; armor reduces only the excess of a stronger hit.
// vanilla: LivingEntity.hurtServer
func TestAMannequinArmourFollowsTheHurtWindow(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "WindowWatcher")
	c.ChatCommand(`summon mannequin 1.5 64 1.5 {immovable:1b,equipment:{` + diamondMannequinEquipment + `}}`)
	id := awaitMannequin(t, c, srv).id
	for _, hit := range []struct {
		amount string
		health float32
	}{{"4", 19.04}, {"10", 17.480001}} {
		c.ChatCommand("damage @e[type=mannequin,limit=1] " + hit.amount + " player_attack")
		seen := readMannequinHurt(c, srv, id, 2, func(s *mannequinHurtSeen) bool { return s.healthSent })
		if !seen.healthSent || seen.health != hit.health {
			t.Errorf("hit %s: health sent=%v value=%v, want %v", hit.amount, seen.healthSent, seen.health, hit.health)
		}
	}
	c.ChatCommand("damage @e[type=mannequin,limit=1] 10 player_attack")
	seen := readMannequinHurt(c, srv, id, 2, func(*mannequinHurtSeen) bool { return false })
	if seen.healthSent || seen.damageEvent {
		t.Errorf("equal hit inside window: health sent=%v damage event=%v", seen.healthSent, seen.damageEvent)
	}
	c.ChatCommand("item replace entity @e[type=mannequin,limit=1] armor.chest with air")
	mannequinEquipment(t, c, srv, id)
	servertest.RunTicks(t, srv, 11)
	c.ChatCommand("damage @e[type=mannequin,limit=1] 4 lava")
	seen = readMannequinHurt(c, srv, id, 3, func(s *mannequinHurtSeen) bool { return s.healthSent })
	if !seen.healthSent || math.Abs(float64(seen.health)-15.217143) > 0.00001 {
		t.Errorf("after removing chestplate: health sent=%v value=%v, want 15.217143", seen.healthSent, seen.health)
	}
}

// vanilla: LivingEntity.actuallyHurt applies armor before resistance and absorption.
func TestAMannequinArmourPrecedesResistanceAndAbsorption(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "ArmourEffects")
	c.ChatCommand(`summon mannequin 1.5 64 1.5 {immovable:1b,equipment:{` + diamondMannequinEquipment + `}}`)
	id := awaitMannequin(t, c, srv).id
	c.ChatCommand("effect give @e[type=mannequin,limit=1] resistance 60")
	c.ChatCommand("effect give @e[type=mannequin,limit=1] absorption 60")
	c.ChatCommand("damage @e[type=mannequin,limit=1] 20 player_attack")
	seen := readMannequinHurt(c, srv, id, 3, func(s *mannequinHurtSeen) bool { return s.healthSent })
	if !seen.healthSent || seen.health != 17.6 {
		t.Errorf("health sent=%v value=%v, want 17.6", seen.healthSent, seen.health)
	}
}

// vanilla: LivingEntity.getArmorValue, getDamageAfterArmorAbsorb
func TestAMannequinArmourUsesEffectiveAttributes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, attributes, helmet string
		health                   float32
	}{
		{"configured_armor", `{id:"armor",base:10d}`, "leather_helmet", 12.4},
		{"configured_toughness", `{id:"armor",base:10d},{id:"armor_toughness",base:6d}`, "diamond_helmet", 14.2},
		{"persistent_modifiers", `{id:"armor",base:5d,modifiers:[{id:"test:armor",amount:4d,operation:"add_value"}]},{id:"armor_toughness",base:4d,modifiers:[{id:"test:toughness",amount:1d,operation:"add_multiplied_total"}]}`, "leather_helmet", 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "EffectiveArmour")
			c.ChatCommand(`summon mannequin 1.5 64 1.5 {immovable:1b,attributes:[` + tc.attributes + `],equipment:{head:{id:"` + tc.helmet + `"}}}`)
			id := awaitMannequin(t, c, srv).id
			c.ChatCommand("damage @e[type=mannequin,limit=1] 10 player_attack")
			seen := readMannequinHurt(c, srv, id, 3, func(s *mannequinHurtSeen) bool { return s.healthSent })
			if !seen.healthSent || seen.health != tc.health {
				t.Errorf("health sent=%v value=%v, want %v", seen.healthSent, seen.health, tc.health)
			}
		})
	}
}
