package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestEnchantmentsCarryWindBurstAndRejectOtherExplosions(t *testing.T) {
	const raw = `{"type":"minecraft:explode","radius":3.5,"knockback_multiplier":{"type":"minecraft:lookup","values":[1.2,1.75,2.2],"fallback":{"type":"minecraft:linear","base":1.5,"per_level_above_first":0.35}},"block_interaction":"trigger","immune_blocks":"#minecraft:blocks_wind_charge_explosions","small_particle":{"type":"minecraft:gust_emitter_small"},"large_particle":{"type":"minecraft:gust_emitter_large"},"sound":"minecraft:entity.wind_charge.wind_burst"}`
	w := &enchantmentWriter{}
	got, ok := w.entityEffect(decode([]byte(raw), "wind burst"), "minecraft:post_attack")
	if !ok || len(w.skippedEffects) != 0 {
		t.Fatalf("wind burst omitted: %v", w.skippedEffects)
	}
	for _, want := range []string{"Explode: &EnchExplode", "Constant: 3.5", "Values: []float32{1.2, 1.75, 2.2}", "Base: 1.5, PerLevel: 0.35", `BlockInteraction: "trigger"`, `ImmuneBlocks: "minecraft:blocks_wind_charge_explosions"`} {
		if !strings.Contains(got, want) {
			t.Errorf("explosion lacks %s: %s", want, got)
		}
	}
	for key, value := range map[string]any{
		"damage_type": "minecraft:explosion", "attribute_to_user": true, "create_fire": true,
		"offset": []any{0.0, 1.0, 0.0}, "block_interaction": "block", "immune_blocks": "#minecraft:another_tag",
		"sound": "minecraft:entity.generic.explode", "small_particle": map[string]any{"type": "minecraft:explosion"},
	} {
		t.Run(key, func(t *testing.T) {
			e := decode([]byte(raw), "wind burst")
			e[key] = value
			if err := tryRead(func() { w.entityEffect(e, "minecraft:post_attack") }); err == nil {
				t.Fatal("unmodelled explosion accepted")
			}
		})
	}
}

func TestEnchantmentsCarryWindBurstRequirements(t *testing.T) {
	const raw = `{"condition":"minecraft:entity_properties","entity":"direct_attacker","predicate":{"minecraft:flags":{"is_flying":false},"minecraft:movement":{"fall_distance":{"min":1.5}}}}`
	got := enchCondition(decode([]byte(raw), "wind burst requirements"))
	if !strings.Contains(got, "IsFlying: new(false)") || !strings.Contains(got, "MinFallDistance: new(float64(1.5))") {
		t.Fatalf("requirements = %s", got)
	}
	for _, bad := range []string{
		strings.Replace(raw, `"direct_attacker"`, `"attacker"`, 1),
		strings.Replace(raw, `"is_flying"`, `"is_swimming"`, 1),
		strings.Replace(raw, `"min":1.5`, `"min":1.5,"max":4`, 1),
	} {
		if err := tryRead(func() { enchCondition(decode([]byte(bad), "requirements")) }); err == nil {
			t.Errorf("unmodelled requirements accepted: %s", bad)
		}
	}
}

func TestEnchantmentsCarryDamageHolderSets(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{`"minecraft:lava"`, `&DamageTypeSet{Types: []DamageType{DamageTypeLava}}`},
		{`["minecraft:lava","minecraft:fall"]`, `&DamageTypeSet{Types: []DamageType{DamageTypeLava, DamageTypeFall}}`},
		{`[]`, `&DamageTypeSet{Types: []DamageType{}}`},
		{`"#minecraft:is_fire"`, `&DamageTypeSet{Tag: DamageTypeTagIDIsFire, IsTag: true}`},
	} {
		for _, expected := range []bool{true, false} {
			raw := fmt.Sprintf(`{"condition":"minecraft:damage_source_properties","predicate":{"tags":[{"id":%s,"expected":%t}]}}`, tc.id, expected)
			got := enchCondition(decode([]byte(raw), "requirements"))
			want := fmt.Sprintf("{Types: %s, Expected: %t}", tc.want, expected)
			if !strings.Contains(got, want) {
				t.Errorf("%s: %s, want %s", tc.id, got, want)
			}
		}
	}
}

func TestEnchantmentsCarryBodyConditions(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_flying":false}}}`,
			"&EnchCondition{Kind: EnchCondBody, Entity: \"this\", Body: &LootEntityPredicate{Flying: lootBool(false)}}"},
		{`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:periodic_tick":5,"minecraft:movement":{"horizontal_speed":{"min":0.5}},"minecraft:movement_affected_by":{"block":{"blocks":"#minecraft:soul_speed_blocks"}}}}`,
			`Period: 5, Movement: &LootMovement{HorizontalSpeed: AdvancementRange{Min: 0.5, HasMin: true}}, MovementAffectedBy: &LootLocationCheck{Block: "#minecraft:soul_speed_blocks"}`},
		{`{"condition":"minecraft:enchantment_active_check","active":false}`, "&EnchCondition{Kind: EnchCondActive, Active: false}"},
		// The user-state flags stay with the entity state.
		{`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_in_water":true}}}`, "EnchCondEntityState"},
	} {
		if got := enchCondition(decode([]byte(tc.raw), "requirements")); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %s, want %s", tc.raw, got, tc.want)
		}
	}
	bad := `{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_flying":false,"is_swimming":true}}}`
	if err := tryRead(func() { enchCondition(decode([]byte(bad), "requirements")) }); err == nil {
		t.Error("an unmodelled flag was accepted")
	}
}

func TestEnchantmentsExpandAttributeGroupsAndKeepTheirIndex(t *testing.T) {
	const raw = `[{"effect":{"type":"minecraft:all_of","effects":[
		{"type":"minecraft:attribute","amount":1,"attribute":"minecraft:movement_speed","id":"a:b","operation":"add_value"},
		{"type":"minecraft:attribute","amount":2,"attribute":"minecraft:movement_efficiency","id":"a:b","operation":"add_value"}]}},
		{"effect":{"type":"minecraft:change_item_damage","amount":1}}]`
	w := &enchantmentWriter{}
	got := w.locatedEntries(decodeValue([]byte(raw), "located").([]any), "minecraft:location_changed")
	for _, want := range []string{`Attribute: &EnchAttribute{Attribute: "minecraft:movement_speed"`, `Attribute: &EnchAttribute{Attribute: "minecraft:movement_efficiency"`,
		"Requires: nil, Group: 0}", "ChangeItemDamage: &EnchLevelValue{Kind: EnchValueConstant, Constant: 1}}, Requires: nil, Group: 1}"} {
		if !strings.Contains(got, want) {
			t.Errorf("entries lack %s: %s", want, got)
		}
	}
	if strings.Count(got, "Group: 0") != 2 {
		t.Errorf("the all_of's attributes do not share a group: %s", got)
	}
}

func TestEnchantmentsCarryUniformSoundPitch(t *testing.T) {
	w := &enchantmentWriter{sounds: soundHolders{"minecraft:particle.soul_escape": "SoundParticleSoulEscape"}}
	got, _ := w.entityEffect(decode([]byte(`{"type":"minecraft:play_sound","sound":"minecraft:particle.soul_escape","volume":0.6,"pitch":{"type":"minecraft:uniform","min_inclusive":0.6,"max_exclusive":1}}`), "sound"), "x")
	if !strings.Contains(got, "Pitch: 0.6, PitchMax: 1}") {
		t.Errorf("pitch = %s", got)
	}
}
