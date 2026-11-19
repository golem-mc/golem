package datapack

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDecodeBrewingPotionSets(t *testing.T) {
	tag, ok := registry.PotionTagMembers("minecraft:tradeable")
	if !ok || len(tag) == 0 {
		t.Fatal("missing tradeable potion tag")
	}
	for _, tc := range []struct {
		predicate string
		names     []string
	}{
		{`"water"`, []string{"minecraft:water"}},
		{`["minecraft:water","minecraft:awkward"]`, []string{"minecraft:water", "minecraft:awkward"}},
		{`"#minecraft:tradeable"`, tag},
	} {
		t.Run(tc.predicate, func(t *testing.T) {
			set := decodeRecipe(t, fmt.Sprintf(`{"type":"brewing",
				"input":{"item":["minecraft:potion","minecraft:splash_potion"],"potion_contents":{"potions":%s}},
				"reagent":{"item":"#minecraft:planks"},
				"output":{"id":"minecraft:potion","components":{"minecraft:potion_contents":{"potion":"swiftness"}}}}`, tc.predicate))
			var got, want []int32
			for _, name := range tc.names {
				want = append(want, registry.MustPotion(name))
			}
			for _, r := range set.Brewing {
				got = append(got, r.Potion)
				if r.ID != "pk:r" || len(r.Input) != 2 || !slices.Contains(r.Input, item(t, "minecraft:splash_potion")) ||
					!slices.Contains(r.Reagent, item(t, "minecraft:oak_planks")) || r.ResultID != item(t, "minecraft:potion") ||
					r.ResultPotion != registry.MustPotion("minecraft:swiftness") {
					t.Errorf("unexpected brewing row %+v", r)
				}
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("input potion set = %v, want %v", got, want)
			}
		})
	}
	set := decodeRecipe(t, `{"type":"brewing","input":{"item":"minecraft:brick"},
		"reagent":{"item":"minecraft:stick"},"output":{"id":"minecraft:glass_bottle"}}`)
	if set.Empty() || len(set.Brewing) != 1 || set.Brewing[0].Potion != -1 || set.Brewing[0].ResultPotion != -1 {
		t.Fatalf("unconstrained recipe = %+v, want any input potion and no output potion", set)
	}
}

func TestDecodeBrewingRefusesUnmodelledFields(t *testing.T) {
	const valid = `{"type":"brewing","input":{"item":"minecraft:potion","potion_contents":{"potions":"minecraft:water"}},
		"reagent":{"item":"minecraft:sugar"},"output":{"id":"minecraft:potion","components":{"minecraft:potion_contents":{"potion":"minecraft:swiftness"}}}}`
	for _, tc := range []struct{ old, replacement, key string }{
		{`"potions":"minecraft:water"`, `"potions":["minecraft:water","minecraft:missing"]`, "unknown potion"},
		{`"potions":"minecraft:water"`, `"potions":"#pk:missing"`, "potion tag"},
		{`"potions":"minecraft:water"`, `"potions":[]`, "potions"},
		{`"potions":"minecraft:water"`, `"potions":"water","effects":{}`, "effects"},
		{`"item":"minecraft:sugar"`, `"item":"sugar","potion_contents":{"potions":"water"}`, "potion_contents"},
		{`"id":"minecraft:potion"`, `"id":"potion","count":2`, "count"},
		{`"potion":"minecraft:swiftness"`, `"potion":"minecraft:missing"`, "unknown potion"},
		{`"potion":"minecraft:swiftness"`, `"potion":"swiftness","custom_color":123`, "custom_color"},
		{`"minecraft:potion_contents":{"potion":"minecraft:swiftness"}`, `"minecraft:custom_name":"oops"`, "minecraft:custom_name"},
	} {
		t.Run(tc.key+tc.replacement, func(t *testing.T) {
			var set registry.PackRecipes
			err := DecodeRecipe("pk:r", []byte(strings.Replace(valid, tc.old, tc.replacement, 1)), &set)
			if err == nil || !strings.Contains(err.Error(), tc.key) || !set.Empty() {
				t.Errorf("error = %v, recipes = %+v; want refusal naming %s and nothing added", err, set, tc.key)
			}
		})
	}
}
