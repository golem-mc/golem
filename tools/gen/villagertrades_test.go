package main

import (
	"strings"
	"testing"
)

func TestTradeModifiersGenerateEnchantmentsCostsAndFilters(t *testing.T) {
	trade := decode([]byte(`{"given_item_modifiers":[
  {"function":"minecraft:enchant_randomly","options":["minecraft:mending"],"only_compatible":false,"include_additional_cost_component":true},
  {"function":"minecraft:filtered","item_filter":{"items":"minecraft:enchanted_book","predicates":{"minecraft:stored_enchantments":[{}]}},"on_fail":{"function":"minecraft:discard"}}
 ]}`), "trade")
	got, ok := tradeModifiers(&lootReader{}, trade.list("given_item_modifiers"), "trade")
	if !ok || len(got) != 2 {
		t.Fatalf("generated %v, supported %v", got, ok)
	}
	for _, want := range []string{"IncludeAdditionalCost: true", "OnlyCompatible: false", "StoredEnchantments: []LootEnchantments{", "OnFail: LootDiscard{"} {
		if !strings.Contains(strings.Join(got, ""), want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}

func TestTradeModifiersGenerateMapResultFilters(t *testing.T) {
	filter := decode([]byte(`{"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:map_id":{}}},"on_fail":{"function":"minecraft:discard"}}`), "trade")
	got, ok := tradeModifiers(&lootReader{}, []any{map[string]any(filter)}, "trade")
	if !ok || len(got) != 1 || !strings.Contains(got[0], "HasMapID: true") {
		t.Fatalf("generated %v, supported %v", got, ok)
	}
}

func TestTradeModifiersRejectUnmodelledShapes(t *testing.T) {
	for _, modifier := range []string{
		`{"function":"minecraft:unknown"}`,
		`{"function":"minecraft:set_random_dyes","number_of_dyes":1,"unknown":true}`,
		`{"function":"minecraft:set_random_dyes","number_of_dyes":{"type":"minecraft:sum","summands":[],"unknown":true}}`,
		`{"function":"minecraft:set_random_potion","options":[],"unknown":true}`,
		`{"function":"minecraft:set_stew_effect","effects":[{"type":"minecraft:saturation","duration":7,"unknown":true}]}`,
		`{"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:dyed_color":{"color":1}}},"on_fail":{"function":"minecraft:discard"}}`,
		`{"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:stored_enchantments":{}}},"on_fail":{"function":"minecraft:discard"}}`,
		`{"function":"minecraft:enchant_randomly","conditions":[]}`,
		`{"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:unknown":{}}},"on_fail":{"function":"minecraft:discard"}}`,
		`{"function":"minecraft:filtered","item_filter":{},"on_fail":{"function":"minecraft:set_count","count":2}}`,
	} {
		t.Run(modifier, func(t *testing.T) {
			defer func() {
				if _, ok := recover().(genError); !ok {
					t.Fatal("expected a generator refusal")
				}
			}()
			tradeModifiers(&lootReader{}, []any{map[string]any(decode([]byte(modifier), "trade"))}, "trade")
		})
	}
}

func TestTradeModifiersGenerateDyesStewAndPotions(t *testing.T) {
	trade := decode([]byte(`{"given_item_modifiers":[
  {"function":"minecraft:set_random_dyes","number_of_dyes":{"type":"minecraft:sum","summands":[1,{"type":"minecraft:binomial","n":2,"p":0.75}]}},
  {"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:dyed_color":{}}},"on_fail":{"function":"minecraft:discard"}},
  {"function":"minecraft:set_stew_effect","effects":[{"type":"minecraft:saturation","duration":7}]},
  {"function":"minecraft:set_random_potion","options":["minecraft:healing"]},
  {"function":"minecraft:set_potion","id":"minecraft:long_invisibility"}
 ]}`), "trade")
	got, ok := tradeModifiers(&lootReader{}, trade.list("given_item_modifiers"), "trade")
	if !ok || len(got) != 5 {
		t.Fatalf("generated %v, supported %v", got, ok)
	}
	for _, want := range []string{"LootSum{Summands:", "LootBinomial{", "HasDyedColor: true", "LootSetStewEffect{", "LootSetRandomPotion{Options: []string{\"minecraft:healing\"}", "LootSetPotion{Potion: \"minecraft:long_invisibility\""} {
		if !strings.Contains(strings.Join(got, ""), want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}
