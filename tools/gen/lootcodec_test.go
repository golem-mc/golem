package main

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func fixtureLootCodec(files map[string]string) lootCodec {
	fsys := fstest.MapFS{}
	for name, raw := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(raw)}
	}
	return lootCodec{&vanilla.DataPack{FS: fsys}}
}

func TestLootCodecReadsTables(t *testing.T) {
	codec := fixtureLootCodec(map[string]string{
		"minecraft/predicate/tool/can_shear.json": `{"type":"minecraft:match_tool","predicate":{"items":"minecraft:shears"}}`,
		"example/item_modifier/count.json":        `[{"type":"minecraft:set_count","count":2},{"type":"minecraft:set_count","count":3,"add":true}]`,
	})
	current := `{"pools":[{"condition":{"type":"minecraft:survives_explosion"},"entries":[{"type":"minecraft:item","name":"minecraft:stone","condition":"minecraft:tool/can_shear","modifier":"example:count"}]}]}`
	r := &lootReader{dp: codec.dp, usedItems: map[string]bool{}}
	after := r.tableData(decode([]byte(current), "current"), "fixture")
	for _, want := range []string{"LootSurvivesExplosion{}", `Items: []string{"minecraft:shears"}`, "Value: 2.0", "Value: 3.0"} {
		if !strings.Contains(after, want) {
			t.Errorf("table lacks %s", want)
		}
	}
	if strings.Index(after, "Value: 2.0") > strings.Index(after, "Value: 3.0") {
		t.Error("modifier sequence reversed")
	}
}

func TestLootCodecResolvesNestedPredicateTagsWithoutFlatteningAnyOf(t *testing.T) {
	codec := fixtureLootCodec(map[string]string{
		"minecraft/predicate/player.json":     `{"type":"minecraft:killed_by_player"}`,
		"minecraft/tags/predicate/inner.json": `{"values":["minecraft:player"]}`,
		"minecraft/tags/predicate/outer.json": `{"values":["#minecraft:inner","minecraft:player"]}`,
	})
	c := codec.predicate(decodeValue([]byte(`{"type":"minecraft:any_of","terms":[{"type":"minecraft:all_of","terms":"#minecraft:outer"},{"type":"minecraft:inverted","term":"minecraft:player"}]}`), "test"), nil)
	r := &lootReader{}
	want := "LootAnyOf{Terms: []LootCondition{LootAllOf{Terms: []LootCondition{LootKilledByPlayer{}}}, LootInverted{Term: LootKilledByPlayer{}}}}"
	if got := r.condition(c, "test"); got != want {
		t.Errorf("condition = %s, want %s", got, want)
	}
}

func TestLootCodecRejectsUnrepresentableOrCyclicData(t *testing.T) {
	codec := fixtureLootCodec(map[string]string{
		"minecraft/predicate/a.json":          `{"type":"minecraft:inverted","term":"minecraft:b"}`,
		"minecraft/predicate/b.json":          `{"type":"minecraft:all_of","terms":["minecraft:a"]}`,
		"minecraft/tags/predicate/cycle.json": `{"values":["#minecraft:cycle"]}`,
		"minecraft/item_modifier/a.json":      `["minecraft:b"]`,
		"minecraft/item_modifier/b.json":      `{"type":"minecraft:sequence","functions":["minecraft:a"]}`,
	})
	for _, tt := range []struct {
		name, raw, want string
		read            func(any)
	}{
		{"predicate cycle", `"minecraft:a"`, "cyclic", func(v any) { codec.predicate(v, nil) }},
		{"tag cycle", `{"type":"minecraft:all_of","terms":"#minecraft:cycle"}`, "cyclic", func(v any) { codec.predicate(v, nil) }},
		{"modifier cycle", `"minecraft:a"`, "cyclic", func(v any) { codec.modifier(v, nil) }},
		{"missing predicate", `"minecraft:missing"`, "missing.json", func(v any) { codec.predicate(v, nil) }},
		{"invalid path", `"minecraft:../escape"`, "invalid", func(v any) { codec.predicate(v, nil) }},
		{"tag as holder", `"#minecraft:tag"`, "cannot be a tag", func(v any) { codec.predicate(v, nil) }},
		{"conditional sequence", `{"type":"minecraft:sequence","condition":{"type":"minecraft:killed_by_player"},"functions":[]}`, "conditional", func(v any) { codec.modifier(v, nil) }},
		{"filtered sequence", `{"type":"minecraft:filtered","on_fail":{"type":"minecraft:sequence","functions":[{"type":"minecraft:discard"},{"type":"minecraft:discard"}]}}`, "single item modifier", func(v any) { codec.modifier(v, nil) }},
		{"mixed condition fields", `{"condition":"minecraft:killed_by_player","type":"minecraft:survives_explosion"}`, "both", func(v any) { codec.predicate(v, nil) }},
		{"mixed modifier fields", `{"function":"minecraft:set_count","type":"minecraft:set_count","count":1}`, "both", func(v any) { codec.modifier(v, nil) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tryRead(func() { tt.read(decodeValue([]byte(tt.raw), tt.name)) })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %s", err, tt.want)
			}
		})
	}
}

func TestLootCodecRejectsAmbiguousConditionFields(t *testing.T) {
	codec := fixtureLootCodec(nil)
	err := tryRead(func() {
		codec.conditions(decode([]byte(`{"condition":{"type":"minecraft:killed_by_player"},"conditions":[]}`), "test"))
	})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("ambiguous fields accepted: %v", err)
	}
}

func TestLootMatchBlockReadsStateAndRejectsUnmodelledFields(t *testing.T) {
	codec := fixtureLootCodec(nil)
	r := &lootReader{}
	c := codec.predicate(decodeValue([]byte(`{"type":"minecraft:match_block","blocks":["minecraft:acacia_door","minecraft:oak_door"],"state":{"half":"lower"}}`), "test"), nil)
	got := r.condition(c, "test")
	for _, want := range []string{"LootAnyOf", `Block: "minecraft:acacia_door"`, `Block: "minecraft:oak_door"`, `Key: "half"`, `Value: "lower"`} {
		if !strings.Contains(got, want) {
			t.Errorf("condition lacks %s: %s", want, got)
		}
	}
	c["nbt"] = "{}"
	if err := tryRead(func() { r.condition(c, "test") }); err == nil {
		t.Error("unmodelled block entity predicate accepted")
	}
	entry := decode([]byte(`{"type":"minecraft:group","children":[],"modifier":{"type":"minecraft:set_count","count":2}}`), "test")
	if err := tryRead(func() { r.entry(entry, "test") }); err == nil {
		t.Error("modifier on a composite entry was silently dropped")
	}
}

func TestLootTagEntryReadsItemsHolderSet(t *testing.T) {
	codec := fixtureLootCodec(map[string]string{
		"minecraft/tags/item/inner.json": `{"values":["minecraft:music_disc_13","minecraft:music_disc_cat","minecraft:music_disc_11"]}`,
		"minecraft/tags/item/outer.json": `{"values":["#minecraft:inner","minecraft:music_disc_wait","minecraft:music_disc_13"]}`,
	})
	read := func(field string) (string, map[string]bool) {
		r := &lootReader{dp: codec.dp, usedItems: map[string]bool{}}
		e := decode([]byte(`{"type":"minecraft:tag",`+field+`,"expand":true}`), "test")
		return r.entry(e, "test"), r.usedItems
	}
	got, used := read(`"items":"#minecraft:outer"`)
	if !strings.Contains(got, `Items: []Item{ItemMusicDisc11, ItemMusicDisc13, ItemMusicDisc13, ItemMusicDiscCat, ItemMusicDiscWait}, Expand: true`) {
		t.Errorf("tag members changed: %s", got)
	}
	if !used["minecraft:music_disc_wait"] {
		t.Errorf("tag members not recorded as used: %v", used)
	}
	for _, tt := range []struct{ items, want string }{
		{`["stone","minecraft:dirt","stone"]`, `Items: []Item{ItemStone, ItemDirt, ItemStone}`},
		{`"stone"`, `Items: []Item{ItemStone}`},
	} {
		got, _ := read(`"items":` + tt.items)
		if !strings.Contains(got, tt.want) {
			t.Errorf("%s: got %s, want %s", tt.items, got, tt.want)
		}
	}
}

func TestEnchantmentCodecReadsReferencedRequirements(t *testing.T) {
	codec := fixtureLootCodec(map[string]string{
		"minecraft/predicate/arrow.json": `{"type":"minecraft:entity_properties","entity":"direct_attacker","predicate":{"minecraft:entity_type":"minecraft:arrow"}}`,
	})
	entry := decode([]byte(`{"requirements":"minecraft:arrow"}`), "test")
	got := enchCondition(codec.optionalPredicate(entry, "requirements"))
	want := `&EnchCondition{Kind: EnchCondEntityProperties, Entity: "direct_attacker", EntityTypeTag: "", EntityTypeID: "minecraft:arrow"}`
	if got != want {
		t.Errorf("requirement = %s, want %s", got, want)
	}
	if got := enchCondition(codec.optionalPredicate(obj{}, "requirements")); got != "nil" {
		t.Errorf("absent requirement = %s", got)
	}
}

func TestAdvancementCodecReadsConditions(t *testing.T) {
	r := advancementReader{}
	for _, tt := range []struct{ trigger, raw string }{
		{"minecraft:location", `{"player":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:location":{"biomes":"minecraft:badlands"}}}}`},
		{"minecraft:location", `{"player":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:location":{"structures":"minecraft:fortress"}}}}`},
		{"minecraft:location", `{"player":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:location":{"biomes":"minecraft:badlands","structures":["minecraft:mineshaft","minecraft:mineshaft_mesa"]}}}}`},
		{"minecraft:recipe_crafted", `{"recipes":["minecraft:decorated_pot"]}`},
		{"minecraft:player_generates_container_loot", `{"loot_tables":"minecraft:archaeology/desert_pyramid"}`},
		{"minecraft:enter_block", `{"blocks":"minecraft:water"}`},
		{"minecraft:bee_nest_destroyed", `{"blocks":"minecraft:bee_nest","num_bees_inside":3}`},
		{"minecraft:item_used_on_block", `{"location":{"type":"minecraft:match_block","blocks":"minecraft:creaking_heart","state":{"creaking_heart_state":"awake"}}}`},
	} {
		t.Run(tt.trigger, func(t *testing.T) {
			got := r.criterion("test", "criterion", obj{"trigger": tt.trigger, "conditions": map[string]any(decode([]byte(tt.raw), "test"))})
			if got.unmodelled {
				t.Errorf("criterion left unmodelled: %+v", got)
			}
		})
	}
	value := decode([]byte(`{"trigger":"minecraft:location","conditions":{"player":{"type":"minecraft:all_of","terms":[{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:location":{"biomes":"minecraft:badlands"}}},{"type":"minecraft:killed_by_player"}]}}}`), "test")
	if c := r.criterion("test", "test", value); !c.unmodelled {
		t.Error("extra player condition silently dropped")
	}
	for _, raw := range []string{`{"recipes":["minecraft:a","minecraft:b"]}`, `{"recipes":"#minecraft:tag"}`} {
		if err := tryRead(func() { singleRegistryName(decode([]byte(raw), "test"), "recipes") }); err == nil {
			t.Errorf("unrepresentable recipe set accepted: %s", raw)
		}
	}
}

func TestLootGeneratorRaiderPredicateDefaultsAndFlags(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{}`, "Captain: lootBool(false), HasRaid: lootBool(false)"},
		{`{"is_captain":true}`, "Captain: lootBool(true), HasRaid: lootBool(false)"},
		{`{"has_raid":true}`, "Captain: lootBool(false), HasRaid: lootBool(true)"},
		{`{"is_captain":true,"has_raid":true}`, "Captain: lootBool(true), HasRaid: lootBool(true)"},
	} {
		r := &lootReader{}
		p := decode([]byte(`{"minecraft:type_specific/raider":`+tc.raw+`}`), "fixture")
		if got := r.entityPredicate(p, "fixture"); got != "LootEntityPredicate{"+tc.want+"}" {
			t.Errorf("%s: %s", tc.raw, got)
		}
	}
}
