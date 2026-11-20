package datapack

import (
	"archive/zip"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestDecodeAdvancementMatchesTheGeneratedTree decodes the vanilla data
// pack's advancement JSON, when the reference server is at hand, and compares
// each to the one tools/gen advancements generated from it.
func TestDecodeAdvancementMatchesTheGeneratedTree(t *testing.T) {
	jar := "../../../reference-server/versions/" + protocol.GameVersion + "/server-" + protocol.GameVersion + ".jar"
	zr, err := zip.OpenReader(jar)
	if err != nil {
		t.Skip("no reference server jar at " + jar)
	}
	defer zr.Close()
	checked := 0
	for _, want := range registry.Advancements {
		f, err := zr.Open("data/minecraft/advancement/" + strings.TrimPrefix(want.ID, "minecraft:") + ".json")
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(f)
		f.Close()
		got, err := DecodeAdvancement(want.ID, data)
		if err != nil {
			t.Errorf("%s: %v", want.ID, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s decoded as\n%+v\nwant\n%+v", want.ID, got, want)
		}
		checked++
	}
	if checked < len(registry.Advancements)/2 {
		t.Errorf("compared %d of %d advancements", checked, len(registry.Advancements))
	}
}

func TestDecodeAdvancementRefusesWhatVanillaRefuses(t *testing.T) {
	const crit = `"criteria":{"a":{"trigger":"minecraft:tick"}}`
	for name, body := range map[string]string{
		"no criteria":          `{"criteria":{}}`,
		"requirement unknown":  `{` + crit + `,"requirements":[["a","b"]]}`,
		"criterion unrequired": `{"criteria":{"a":{"trigger":"minecraft:tick"},"b":{"trigger":"minecraft:tick"}},"requirements":[["a"]]}`,
		"unknown trigger":      `{"criteria":{"a":{"trigger":"mod:x"}}}`,
		"spear count zero":     `{"criteria":{"a":{"trigger":"minecraft:spear_mobs","conditions":{"count":0}}}}`,
		"invalid trigger id":   `{"criteria":{"a":{"trigger":"Not A Trigger"}}}`,
		"invalid parent id":    `{"parent":"Bad Id",` + crit + `}`,
		"unknown item":         `{` + crit + `,"display":{"icon":{"id":"minecraft:nope"},"title":"t","description":"d"}}`,
		"formatted title":      `{` + crit + `,"display":{"icon":{"id":"minecraft:stick"},"title":{"text":"t","bold":true},"description":"d"}}`,
	} {
		if _, err := DecodeAdvancement("pk:x", []byte(body)); err == nil {
			t.Errorf("%s: decoded, want an error", name)
		}
	}
}

func TestDecodeAdvancementReadsLootAndFunctionRewards(t *testing.T) {
	const crit = `"criteria":{"a":{"trigger":"minecraft:tick"}}`
	for body, want := range map[string][]string{
		`{` + crit + `,"rewards":{"loot":"l","function":"x:y"}}`:         {"minecraft:l"},
		`{` + crit + `,"rewards":{"loot":["x:l","m"],"function":"x:y"}}`: {"x:l", "minecraft:m"},
	} {
		adv, err := DecodeAdvancement("pk:x", []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(adv.Loot, want) || adv.Function != "x:y" {
			t.Errorf("loot %v function %q, want %v and x:y", adv.Loot, adv.Function, want)
		}
	}
}

func TestDecodeAdvancementReadsLiteralTextAndUnqualifiedNames(t *testing.T) {
	adv, err := DecodeAdvancement("pk:x", []byte(`{"parent":"pk:root","display":{"icon":{"id":"stick","count":2},
		"title":"Plain","description":{"text":"Words"},"frame":"goal","hidden":true},"rewards":{"experience":7},
		"criteria":{"b":{"trigger":"minecraft:changed_dimension","conditions":{"to":"the_nether"}},
		"a":{"trigger":"minecraft:inventory_changed","conditions":{"items":[{"items":["stick","minecraft:dirt"]}]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := adv.Display
	if adv.Parent != "pk:root" || adv.Experience != 7 || d == nil || d.Title != "Plain" || !d.TitleLiteral ||
		d.Description != "Words" || !d.DescriptionLiteral || d.Frame != 2 || !d.Hidden || d.IconCount != 2 || !d.ShowToast {
		t.Errorf("decoded %+v display %+v", adv, d)
	}
	stick, _ := registry.ItemID("minecraft:stick")
	dirt, _ := registry.ItemID("minecraft:dirt")
	want := []int32{min(stick, dirt), max(stick, dirt)}
	if len(adv.Criteria) != 2 || adv.Criteria[0].Name != "a" || !reflect.DeepEqual(adv.Criteria[0].Items, [][]int32{want}) ||
		adv.Criteria[1].To != "minecraft:the_nether" {
		t.Errorf("criteria %+v", adv.Criteria)
	}
	if !reflect.DeepEqual(adv.Requirements, [][]string{{"a"}, {"b"}}) {
		t.Errorf("requirements %v, want one group per criterion", adv.Requirements)
	}
}

func TestDecodeAdvancementIgnoresWhatVanillaIgnoresAndNeverFiresWhatItCannotTest(t *testing.T) {
	adv, err := DecodeAdvancement("pk:x", []byte(`{"extra":1,"display":{"icon":{"id":"minecraft:stick","extra":1},"title":"t","description":"d","extra":1},
		"criteria":{"a":{"trigger":"minecraft:inventory_changed","extra":1,"conditions":{"player":[],"slots":{},"items":[{"items":"minecraft:stick"}]}},
		"b":{"trigger":"minecraft:inventory_changed","conditions":{"items":[{"items":"minecraft:stick","count":2}]}},
		"c":{"trigger":"minecraft:consume_item","conditions":{"player":[]}},
		"d":{"trigger":"minecraft:consume_item","conditions":{"item":{"items":"minecraft:stick"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for i, wantUnmodelled := range []bool{false, true, false, false} {
		if got := adv.Criteria[i].Unmodelled; got != wantUnmodelled {
			t.Errorf("criterion %s unmodelled = %v, want %v", adv.Criteria[i].Name, got, wantUnmodelled)
		}
	}
}

func TestStructureLocationPredicatesDecode(t *testing.T) {
	for _, tc := range []struct {
		name, location string
		want           []string
		unmodelled     bool
	}{
		{"single", `"structures":"fortress"`, []string{"minecraft:fortress"}, false},
		{"list", `"structures":["fortress","bastion_remnant"]`, []string{"minecraft:fortress", "minecraft:bastion_remnant"}, false},
		{"empty", `"structures":[]`, []string{}, false},
		{"tag", `"structures":"#minecraft:eye_of_ender_located"`, []string{"minecraft:stronghold"}, false},
		{"combined", `"structures":"fortress","biomes":"plains"`, []string{"minecraft:fortress"}, false},
		{"unsupported", `"structures":"fortress","light":{"light":7}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"criteria":{"here":{"trigger":"minecraft:location","conditions":{"player":[{
				"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:location":{` + tc.location + `}}}]}}}}`
			adv, err := DecodeAdvancement("pk:here", []byte(body))
			if err != nil {
				t.Fatal(err)
			}
			got := adv.Criteria[0]
			if !reflect.DeepEqual(got.Structures, tc.want) || got.Unmodelled != tc.unmodelled {
				t.Errorf("decoded %+v, want structures %v, unmodelled %v", got, tc.want, tc.unmodelled)
			}
		})
	}
}

// TestDecodeAdvancementReads263Conditions reads the shapes 26.3's own
// advancements are written in: a lone context condition keyed by "type", a
// conjunction of them, match_block, "#" tag predicates and the plural
// holder-set fields.
func TestDecodeAdvancementReads263Conditions(t *testing.T) {
	adv, err := DecodeAdvancement("pk:x", []byte(`{"criteria":{
		"a":{"trigger":"location","conditions":{"player":{"type":"entity_properties","entity":"this",
			"predicate":{"minecraft:location":{"structures":"minecraft:stronghold"}}}}},
		"b":{"trigger":"item_used_on_block","conditions":{"location":{"type":"minecraft:all_of","terms":[
			{"type":"minecraft:location_check","predicate":{"block":{"blocks":"#minecraft:beehives"},"smokey":true}},
			{"type":"minecraft:match_tool","predicate":{"items":"minecraft:glass_bottle"}}]}}},
		"c":{"trigger":"placed_block","conditions":{"location":{"type":"minecraft:match_block","blocks":"minecraft:creaking_heart",
			"state":{"creaking_heart_state":"awake"}}}},
		"d":{"trigger":"player_killed_entity","conditions":{"entity":{"type":"minecraft:entity_properties","entity":"this",
			"predicate":{"minecraft:distance":{"horizontal":{"min":50.0}},"minecraft:entity_type":"minecraft:skeleton"}},
			"killing_blow":{"tags":[{"expected":true,"id":"#minecraft:is_projectile"}]}}},
		"e":{"trigger":"recipe_crafted","conditions":{"recipes":"minecraft:crafter","ingredients":[{"items":"minecraft:stick"}]}},
		"f":{"trigger":"player_generates_container_loot","conditions":{"loot_tables":"minecraft:archaeology/desert_well"}},
		"g":{"trigger":"using_item","conditions":{"item":{"items":"minecraft:spyglass"},"player":{"type":"minecraft:entity_properties",
			"entity":"this","predicate":{"minecraft:type_specific/player":{"looking_at":{"minecraft:entity_type":"minecraft:ghast"}}}}}},
		"h":{"trigger":"bee_nest_destroyed","conditions":{"blocks":"minecraft:bee_nest","num_bees_inside":3,
			"item":{"predicates":{"minecraft:enchantments":[{"enchantments":"minecraft:silk_touch","levels":{"min":1}}]}}}},
		"i":{"trigger":"enter_block","conditions":{"blocks":"minecraft:end_gateway"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range adv.Criteria {
		if c.Unmodelled {
			t.Errorf("criterion %s is unmodelled: %+v", c.Name, c)
		}
	}
	c := adv.Criteria
	if !slices.Equal(c[0].Structures, []string{"minecraft:stronghold"}) {
		t.Errorf("a's structures = %v, want the stronghold", c[0].Structures)
	}
	if len(c[1].Where) != 1 || !c[1].Where[0].Smokey || len(c[1].Items) != 1 {
		t.Errorf("b = %+v, want a smoked beehive and a glass bottle", c[1])
	}
	if len(c[2].Where) != 1 || c[2].Where[0].State["creaking_heart_state"] != "awake" {
		t.Errorf("c's where = %+v, want an awake creaking heart", c[2].Where)
	}
	if d := c[3]; d.Entity != "minecraft:skeleton" || !d.Projectile || d.MinDistance != 50 {
		t.Errorf("d = %+v, want a skeleton shot from 50 blocks", d)
	}
	if c[4].RecipeID != "minecraft:crafter" || len(c[4].Items) != 1 || c[5].LootTable != "minecraft:archaeology/desert_well" {
		t.Errorf("e, f = %+v, %+v, want the crafter recipe and the desert well loot", c[4], c[5])
	}
	if c[6].LookingAt != "minecraft:ghast" || len(c[6].Items) != 1 {
		t.Errorf("g = %+v, want a spyglass at a ghast", c[6])
	}
	if h := c[7]; h.Block != "minecraft:bee_nest" || h.Bees != 3 || h.Enchantment != "minecraft:silk_touch" {
		t.Errorf("h = %+v, want a silk-touched bee nest with 3 bees", h)
	}
	if len(c[8].Where) != 1 || !slices.Equal(c[8].Where[0].Blocks, []string{"minecraft:end_gateway"}) {
		t.Errorf("i's where = %+v, want the end gateway", c[8].Where)
	}

	// match_block's blocks is optional (BlockPredicate.MAP_CODEC); a state alone is not modelled.
	adv, err = DecodeAdvancement("pk:y", []byte(`{"criteria":{"a":{"trigger":"placed_block",
		"conditions":{"location":{"type":"minecraft:match_block","state":{"lit":"true"}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !adv.Criteria[0].Unmodelled {
		t.Errorf("a match_block without blocks = %+v, want unmodelled", adv.Criteria[0])
	}
}

func TestDecodeAdvancementKeepsEntityConditionsAsLootConditions(t *testing.T) {
	adv, err := DecodeAdvancement("pk:x", []byte(`{"criteria":{
		"tame":{"trigger":"minecraft:tame_animal","conditions":{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Tags:[\"a\"]}"}}]}},
		"bred":{"trigger":"minecraft:bred_animals","conditions":{"child":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_baby":true}}}]}},
		"cure":{"trigger":"minecraft:cured_zombie_villager","conditions":{"villager":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:villager"}}]}},
		"fx":{"trigger":"minecraft:effects_changed","conditions":{"effects":{"minecraft:speed":{"amplifier":{"min":1}}}}},
		"arrow":{"trigger":"minecraft:killed_by_arrow","conditions":{"victims":[[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Tags:[\"a\"]}"}}]]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]registry.AdvancementCriterion{}
	for _, c := range adv.Criteria {
		if c.Unmodelled {
			t.Errorf("criterion %s is unmodelled", c.Name)
		}
		by[c.Name] = c
	}
	if len(by["tame"].Contexts["entity"]) != 1 || len(by["bred"].Contexts["child"]) != 1 || len(by["cure"].Contexts["villager"]) != 1 {
		t.Errorf("entity conditions not kept: %+v %+v %+v", by["tame"].Contexts, by["bred"].Contexts, by["cure"].Contexts)
	}
	if by["fx"].EffectStates["minecraft:speed"].AmplifierMin == nil || len(by["arrow"].VictimConds) != 1 {
		t.Errorf("effect or victims conditions not kept: %+v %+v", by["fx"].EffectStates, by["arrow"].VictimConds)
	}
}
