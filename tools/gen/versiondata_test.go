package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestBiomeSpawnsReadAttributeCounts(t *testing.T) {
	w := newWorkspace(t)
	w.biomeRegistry("plains")
	file := "data/minecraft/worldgen/biome/plains.json"
	w.serverJar(map[string]string{file: `{"attributes":{"minecraft:gameplay/natural_mob_spawns":{"modifier":"overlay","argument":{"spawn_costs":{},"spawns_by_category":{"monster":[{"type":"minecraft:zombie","weight":95,"count":4},{"type":"minecraft:enderman","weight":10,"count":{"type":"minecraft:uniform","min_inclusive":1,"max_inclusive":4}}]}}}}}`})
	w.gen("biomespawns")
	before := w.read("internal/server/entity/spawning_gen.go")
	if !strings.Contains(before, `{registry.EntityTypeEnderman, 10, 1, 4}`) {
		t.Error("uniform spawn count was lost")
	}
}

func TestBiomeSpawnsRejectUnsupportedProviders(t *testing.T) {
	for _, count := range []string{
		`{"type":"minecraft:biased_to_bottom","min_inclusive":1,"max_inclusive":4}`,
		`{"type":"minecraft:uniform","min_inclusive":5,"max_inclusive":4}`,
		`{"type":"minecraft:uniform","min_inclusive":1,"max_inclusive":4,"extra":true}`,
		`1.5`, `0`, `2147483648`,
	} {
		raw := `{"attributes":{"minecraft:gameplay/natural_mob_spawns":{"modifier":"overlay","argument":{"spawns_by_category":{"monster":[{"type":"minecraft:zombie","weight":95,"count":` + count + `}]}}}}}`
		if err := tryRead(func() { biomeSpawners(decode([]byte(raw), "fixture"), "fixture") }); err == nil {
			t.Errorf("unsupported count accepted: %s", count)
		}
	}
	for _, raw := range []string{
		`{}`,
		`{"attributes":{"minecraft:gameplay/natural_mob_spawns":{"modifier":"replace","argument":{"spawns_by_category":{}}}}}`,
		`{"attributes":{"minecraft:gameplay/natural_mob_spawns":{"modifier":"overlay","argument":{}}}}`,
	} {
		if err := tryRead(func() { biomeSpawners(decode([]byte(raw), "fixture"), "fixture") }); err == nil {
			t.Errorf("unsupported settings accepted: %s", raw)
		}
	}
}

func TestVillagerTradesReadMapModifiers(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{"minecraft:item":{"entries":{"minecraft:emerald":{"protocol_id":0},"minecraft:stone":{"protocol_id":1}}}}`)
	files := map[string]string{
		"data/minecraft/tags/worldgen/structure/on_treasure_maps.json": `{"values":["minecraft:buried_treasure"]}`,
		"data/minecraft/tags/villager_trade/example.json":              `{"values":["minecraft:example"]}`,
	}
	for i := range 50 {
		files[fmt.Sprintf("data/minecraft/trade_set/example/%d.json", i)] = `{"amount":1,"trades":"#minecraft:example"}`
	}
	trade := "data/minecraft/villager_trade/example.json"
	const common = `"wants":{"id":"minecraft:emerald"},"gives":{"id":"minecraft:stone"}`
	files[trade] = `{` + common + `,"given_item_modifier":{"type":"minecraft:exploration_map"},"merchant_predicate":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:predicates":{"minecraft:villager/variant":"minecraft:plains"}}}}`
	w.serverJar(files)
	w.gen("villagertrades")
	w.wantContains("registry/villagertrades_gen.go", `Variants: []string{"minecraft:plains"}`, "LootExplorationMap{")
	if strings.Contains(w.read("registry/villagertrades_gen.go"), "Unsupported: true") {
		t.Fatal("map trade marked unsupported")
	}

	const enchant = `{"type":"minecraft:enchant_randomly","options":["minecraft:mending"],"only_compatible":false,"include_additional_cost_component":true}`
	const filter = `{"type":"minecraft:filtered","item_filter":{"items":"minecraft:enchanted_book","predicates":{"minecraft:stored_enchantments":[{}]}},"on_fail":{"type":"minecraft:discard"}}`
	const doublePrice = `,"double_trade_price_enchantments":["minecraft:mending"]`
	sequence := `{"type":"minecraft:sequence","functions":[` + enchant + `,` + filter + `]}`
	files["data/minecraft/item_modifier/example.json"] = sequence
	var before string
	for _, modifier := range []string{`[` + enchant + `,` + filter + `]`, sequence, `"minecraft:example"`} {
		files[trade] = `{` + common + `,"given_item_modifier":` + modifier + doublePrice + `}`
		w.serverJar(files)
		w.gen("villagertrades")
		got := w.read("registry/villagertrades_gen.go")
		if before == "" {
			before = got
			w.wantContains("registry/villagertrades_gen.go", "IncludeAdditionalCost: true", "OnFail: LootDiscard{", `DoublePrice: []string{"minecraft:mending"}`)
			if strings.Contains(before, "Unsupported: true") {
				t.Fatal("enchanted trade marked unsupported")
			}
		} else if got != before {
			t.Errorf("modifier %s generates a different trade", modifier)
		}
	}
}
