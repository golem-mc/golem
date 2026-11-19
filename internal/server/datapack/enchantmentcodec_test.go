package datapack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestDecodeEnchantmentSoundHolders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want []protocol.SoundHolder
	}{
		{`"minecraft:item.trident.thunder"`, []protocol.SoundHolder{registry.SoundItemTridentThunder.Holder()}},
		{`["minecraft:entity.allay.ambient_with_item","example:custom","unqualified"]`, []protocol.SoundHolder{
			{ID: 0}, {ID: -1, Name: "example:custom"}, {ID: -1, Name: "unqualified"},
		}},
		{`[]`, nil},
	} {
		raw := fmt.Sprintf(`{"supported_items":"minecraft:trident","max_level":3,"effects":{"minecraft:post_piercing_attack":[{"effect":{"type":"minecraft:play_sound","sound":%s,"volume":1,"pitch":1}}]}}`, tc.raw)
		def, dropped, err := DecodeEnchantment("example:sound", []byte(raw))
		if err != nil || len(dropped) != 0 {
			t.Fatalf("decode: %v, dropped %v", err, dropped)
		}
		sound := def.Effects.PostPiercingAttack[0].Effect.PlaySound
		got := sound.Sounds
		if tc.raw[0] == '"' {
			got = []protocol.SoundHolder{sound.Sound}
		} else if sound.Sound != (protocol.SoundHolder{ID: -1}) {
			t.Errorf("list fallback = %+v, want empty inline", sound.Sound)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: holders = %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}

// sameDef compares two definitions as JSON with an empty list read as an
// absent one, since the generated table writes some lists out empty.
func sameDef(t *testing.T, got, want registry.EnchantmentDef) bool {
	t.Helper()
	enc := func(d registry.EnchantmentDef) string {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), "[]", "null")
	}
	return enc(got) == enc(want)
}

// TestDecodeEnchantmentMatchesGeneratedTable checks that the runtime decoder
// reads vanilla's files into the entries tools/gen wrote, and that a
// component the tree lacks is named, not fatal.
func TestDecodeEnchantmentMatchesGeneratedTable(t *testing.T) {
	for _, name := range []string{"sharpness", "fire_aspect", "binding_curse", "soul_speed", "frost_walker"} {
		data, err := os.ReadFile("testdata/enchantment/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		got, dropped, err := DecodeEnchantment("minecraft:"+name, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		def, ok := registry.EnchantmentEffectsByID(registry.MustID("minecraft:enchantment", "minecraft:"+name))
		if !ok {
			t.Fatalf("no table entry for %s", name)
		}
		if !sameDef(t, got, *def) {
			t.Errorf("%s decoded to %+v, want %+v", name, got, *def)
		}
		if len(dropped) != 0 {
			t.Errorf("%s dropped %v", name, dropped)
		}
	}
	_, dropped, err := DecodeEnchantment("pk:unknown", []byte(`{"supported_items":"minecraft:bow","max_level":1,"effects":{"pk:no_such_component":{}}}`))
	if err != nil || len(dropped) != 1 {
		t.Errorf("unknown component: dropped %v, error %v", dropped, err)
	}
}

func TestDecodeEnchantmentItemSets(t *testing.T) {
	def, dropped, err := DecodeEnchantment("pk:x", []byte(`{
		"supported_items":["minecraft:diamond_sword","iron_sword"],
		"primary_items":"minecraft:bow",
		"exclusive_set":["minecraft:sharpness","pk:other"],
		"max_level":2,
		"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":{"type":"minecraft:linear","base":5,"per_level_above_first":5}}}],
			"minecraft:tick":[],
			"minecraft:post_attack":[{"enchanted":"attacker","affected":"victim","effect":{"type":"minecraft:no_such_effect"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	sword, bow := registry.ItemDiamondSword, registry.ItemBow
	if !def.Supports(sword) || def.Supports(bow) || !def.PrimaryFor(bow) || def.PrimaryFor(sword) {
		t.Errorf("Supports/PrimaryFor wrong for %+v", def)
	}
	if !slices.Equal(def.ExclusiveList, []string{"minecraft:sharpness", "pk:other"}) {
		t.Errorf("ExclusiveList = %v", def.ExclusiveList)
	}
	if len(def.Effects.Damage) != 1 || def.Effects.PostAttack != nil {
		t.Errorf("effects = %+v", def.Effects)
	}
	if len(dropped) != 1 {
		t.Errorf("dropped = %v, want only the unsupported post_attack effect", dropped)
	}
}

func TestDecodeEnchantmentRequiredFields(t *testing.T) {
	for _, data := range []string{`{"max_level":1}`, `{"supported_items":"#minecraft:x"}`, `{"supported_items":"nope","max_level":1}`, `[`} {
		if _, _, err := DecodeEnchantment("pk:x", []byte(data)); err == nil {
			t.Errorf("DecodeEnchantment(%s) succeeded", data)
		}
	}
}

// TestDecodeEnchantmentLookupOfInts checks that a lookup's values may all be
// whole numbers, which the JSON converter reads as an int array.
func TestDecodeEnchantmentLookupOfInts(t *testing.T) {
	def, _, err := DecodeEnchantment("pk:x", []byte(`{
		"supported_items":"minecraft:diamond_sword",
		"max_level":3,
		"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":{"type":"minecraft:lookup","values":[1,2,4],"fallback":8}}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Effects.Damage) != 1 || !slices.Equal(def.Effects.Damage[0].Effect.Value.Values, []float32{1, 2, 4}) {
		t.Errorf("damage = %+v, want a lookup of 1, 2, 4", def.Effects.Damage)
	}
}

// vanilla: TagPredicate.codec
func TestDecodeEnchantmentDamageHolderSets(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want registry.DamageTypeSet
	}{
		{`"minecraft:lava"`, registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeLava}}},
		{`"lava"`, registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeLava}}},
		{`["minecraft:lava","fall"]`, registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeLava, registry.DamageTypeFall}}},
		{`[]`, registry.DamageTypeSet{Types: []registry.DamageType{}}},
		{`"#minecraft:is_fire"`, registry.DamageTypeSet{Tag: registry.DamageTypeTagIDIsFire, IsTag: true}},
		{`"minecraft:is_fire"`, registry.DamageTypeSet{Tag: registry.DamageTypeTagIDIsFire, IsTag: true}},
	} {
		for _, expected := range []bool{true, false} {
			raw := fmt.Sprintf(`{"supported_items":"minecraft:elytra","max_level":1,
    "effects":{"minecraft:damage_protection":[{"effect":{"type":"minecraft:add","value":10},
    "requirements":{"type":"minecraft:damage_source_properties","predicate":{"is_direct":false,
    "tags":[{"id":%s,"expected":%t},{"id":"#minecraft:bypasses_invulnerability","expected":false}]}}}]}}`, tc.id, expected)
			def, dropped, err := DecodeEnchantment("pk:holders", []byte(raw))
			if err != nil || len(dropped) != 0 {
				t.Fatalf("%s/%t: dropped %v, error %v", tc.id, expected, dropped, err)
			}
			c := def.Effects.DamageProtection[0].Requires
			if c.Direct != 0 || len(c.DamageTags) != 2 || !reflect.DeepEqual(c.DamageTags[0].Types, &tc.want) || c.DamageTags[0].Expected != expected {
				t.Errorf("%s/%t: condition %+v", tc.id, expected, c)
			}
		}
	}
}

func TestDecodeEnchantmentRejectsInvalidDamageHolderSets(t *testing.T) {
	for _, id := range []string{`"minecraft:no_such_damage"`, `["lava","minecraft:no_such_damage"]`, `["#minecraft:is_fire"]`, `[1]`, `3`, `{}`, `null`} {
		raw := fmt.Sprintf(`{"supported_items":"minecraft:elytra","max_level":1,
   "effects":{"minecraft:damage_protection":[{"effect":{"type":"minecraft:add","value":10},
   "requirements":{"type":"minecraft:damage_source_properties","predicate":{"tags":[{"id":%s,"expected":false}]}}}]}}`, id)
		def, dropped, err := DecodeEnchantment("pk:holders", []byte(raw))
		if err != nil || len(dropped) != 1 || len(def.Effects.DamageProtection) != 0 {
			t.Errorf("%s: effects %+v, dropped %v, error %v", id, def.Effects, dropped, err)
		}
	}
}

// vanilla: ExplodeEffect.CODEC
func TestDecodeExplodeFullShape(t *testing.T) {
	def, dropped, err := DecodeEnchantment("pk:x", []byte(`{"supported_items":"minecraft:bow","max_level":1,"effects":{"minecraft:projectile_spawned":[{"effect":{
		"type":"minecraft:explode","radius":2,"block_interaction":"tnt","immune_blocks":["minecraft:bedrock"],
		"attribute_to_user":true,"damage_type":"minecraft:explosion","offset":[0,1,0],"create_fire":true,
		"small_particle":{"type":"minecraft:explosion"},"large_particle":{"type":"minecraft:explosion_emitter"},
		"sound":"minecraft:entity.generic.explode"}}]}}`))
	if err != nil || len(dropped) > 0 {
		t.Fatalf("decode: %v, dropped %v", err, dropped)
	}
	x := def.Effects.ProjectileSpawned[0].Effect.Explode
	if x == nil || x.BlockInteraction != "tnt" || !x.AttributeToUser || !x.CreateFire || x.Offset != [3]float64{0, 1, 0} ||
		x.DamageType != "minecraft:explosion" || len(x.ImmuneList) != 1 || x.WindBurst() {
		t.Fatalf("explode decoded to %+v", x)
	}
}

// vanilla: BlockStateProvider.CODEC
func TestDecodeReplaceStateProviders(t *testing.T) {
	decode := func(provider string) error {
		def, dropped, err := DecodeEnchantment("pk:x", []byte(`{"supported_items":"minecraft:bow","max_level":1,"effects":{"minecraft:hit_block":[{"effect":{
			"type":"minecraft:replace_block","block_state":`+provider+`}}]}}`))
		if err == nil && (len(dropped) > 0 || def.Effects.HitBlock[0].Effect.Replace == nil) {
			err = errors.New("dropped")
		}
		return err
	}
	for _, typ := range []string{"minecraft:simple_state_provider", "minecraft:simple"} {
		if err := decode(`{"type":"` + typ + `","state":{"id":"minecraft:stone"}}`); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
	if err := decode(`{"type":"minecraft:weighted_state_provider","entries":[{"data":{"Name":"minecraft:stone"},"weight":1}]}`); err != nil {
		t.Errorf("weighted provider: %v", err)
	}
	if err := decode(`{"type":"minecraft:rule_based_state_provider","rules":[]}`); err == nil {
		t.Error("a provider that reads the world decoded")
	}
}
