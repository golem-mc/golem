package entities

import (
	"testing"
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
