package datapack

import (
	"encoding/json"
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

func TestDecodeEnchantmentExponentialAndPredicateReference(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/enchantment/exponential.json")
	if err != nil {
		t.Fatal(err)
	}
	def, dropped, err := DecodeEnchantment("pk:exp", data)
	if err != nil || len(dropped) != 0 {
		t.Fatalf("exponential: %v, dropped %v", err, dropped)
	}
	all := def.Effects.FishingLuckBonus[0].Effect
	if len(all.Effects) != 2 || all.Effects[1].Kind != registry.EnchExponential || all.Effects[1].Exponent == nil {
		t.Errorf("exponential effect = %+v", all)
	}

	data, err = os.ReadFile("testdata/enchantment/predicate_ref.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, dropped, err = DecodeEnchantment("pk:ref", data); err != nil || len(dropped) == 0 {
		t.Errorf("without the pack's predicates the reference is dropped: %v, %v", err, dropped)
	}
	pred, err := os.ReadFile("testdata/enchantment/arrow_ammo_predicate.json")
	if err != nil {
		t.Fatal(err)
	}
	def, dropped, err = DecodeEnchantmentWith("pk:ref", data, func(name string) ([]byte, bool) {
		return pred, name == "pack:arrow_ammo"
	})
	if err != nil || len(dropped) != 0 {
		t.Fatalf("reference: %v, dropped %v", err, dropped)
	}
	req := def.Effects.ProjectilePiercing[0].Requires
	if req == nil || req.Kind != registry.EnchCondAllOf || len(req.Terms) != 1 || req.Terms[0].Kind != registry.EnchCondMatchTool {
		t.Errorf("resolved requirement = %+v", req)
	}
}

// TestDecodeEnchantmentLootRequirements keeps a requirement the enchantment
// codec has no variant for as a loot condition, and still drops one the loot
// codec refuses.
func TestDecodeEnchantmentLootRequirements(t *testing.T) {
	t.Parallel()
	effect := func(requirements string) []byte {
		return []byte(`{"description":{"text":"x"},"max_level":1,"weight":1,"anvil_cost":1,
		"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":9,"per_level_above_first":0},
		"slots":["mainhand"],"supported_items":"#minecraft:enchantable/weapon",
		"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":1},"requirements":` + requirements + `}]}}`)
	}
	for _, tc := range []struct {
		requirements string
		kept         bool
	}{
		{`{"type":"minecraft:entity_scores","entity":"this","scores":{"x":1}}`, true},
		{`{"type":"minecraft:entity_properties","entity":"attacker","predicate":{"minecraft:flags":{"is_sneaking":true}}}`, true},
		{`{"type":"minecraft:inverted","term":{"type":"minecraft:entity_scores","entity":"this","scores":{"x":1}}}`, true},
		{`{"type":"minecraft:location_check","predicate":{"dimension":"minecraft:the_end"}}`, true},
		{`{"type":"minecraft:no_such_condition"}`, false},
		{`{"condition":"minecraft:entity_scores","entity":"this","scores":{"x":1}}`, false},
	} {
		def, dropped, err := DecodeEnchantment("pk:loot", effect(tc.requirements))
		if err != nil {
			t.Fatal(err)
		}
		if kept := len(def.Effects.Damage) == 1 && len(dropped) == 0; kept != tc.kept {
			t.Errorf("%s: kept = %v, dropped %v", tc.requirements, kept, dropped)
		}
	}
}

func TestEnchantmentLegacyConditionKeyRefused(t *testing.T) {
	def, dropped, err := DecodeEnchantment("pk:legacy", []byte(`{"description":{"text":"x"},"max_level":1,"weight":1,"anvil_cost":1,
		"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":9,"per_level_above_first":0},
		"slots":["mainhand"],"supported_items":"#minecraft:enchantable/weapon",
		"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":1},
		"requirements":{"condition":"minecraft:entity_scores","entity":"this","scores":{"x":1}}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Effects.Damage) != 0 || len(dropped) != 1 || !strings.Contains(dropped[0], "No key type in MapLike[") {
		t.Fatalf("effects %v, dropped %v: want the entry refused with No key type", def.Effects.Damage, dropped)
	}
}
