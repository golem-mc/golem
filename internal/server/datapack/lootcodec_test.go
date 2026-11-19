package datapack

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LootItemFunctions.TYPED_CODEC, SequenceFunction.INLINE_CODEC
func TestDecodeItemModifierDiscriminators(t *testing.T) {
	t.Parallel()
	const fields = `:"minecraft:copy_custom_data","source":{"type":"minecraft:storage","source":"test:src"},"ops":[{"source":"x","target":"a.b","op":"replace"}]}`
	typed := `{"type"` + fields
	legacy := `{"function"` + fields
	for _, tc := range []struct {
		name, template string
	}{
		{"single", `%s`},
		{"list", `[%s]`},
		{"sequence", `{"type":"sequence","functions":[%s]}`},
		{"filtered pass", `{"type":"filtered","item_filter":{},"on_pass":%s}`},
		{"filtered fail", `{"type":"filtered","item_filter":{},"on_fail":%s}`},
		{"modify contents", `{"type":"modify_contents","component":"bundle_contents","modifier":%s}`},
		{"entry functions", `{"type":"set_contents","component":"container","entries":[{"type":"item","name":"stick","functions":[%s]}]}`},
		{"entry modifier", `{"type":"set_contents","component":"container","entries":[{"type":"group","children":[{"type":"item","name":"stick","modifier":%s}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := fmt.Sprintf(tc.template, typed)
			want, err := DecodeItemModifierValue(parseJSON([]byte(current)))
			if err != nil {
				t.Fatalf("current inline modifier: %v", err)
			}
			old := fmt.Sprintf(tc.template, legacy)
			if _, err := DecodeItemModifierValue(parseJSON([]byte(old))); err == nil || !strings.Contains(err.Error(), "No key type in MapLike[") {
				t.Errorf("legacy inline modifier error = %v, want missing type", err)
			}
			got, err := DecodeItemModifier([]byte(old))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("legacy pack modifier = %#v, %v; want %#v", got, err, want)
			}
		})
	}
	for _, value := range []string{
		`{"type":"discard","function":"unknown"}`,
		`{"type":"set_custom_data","tag":"{function:keep}"}`,
		`{"type":"set_components","components":{"custom_data":{"function":"keep"}}}`,
	} {
		if _, err := DecodeItemModifierValue(parseJSON([]byte(value))); err != nil {
			t.Errorf("%s: %v", value, err)
		}
	}
	if _, err := DecodeItemModifierValue(parseJSON([]byte(`{"type":5,"function":"discard"}`))); err == nil || err.Error() != "Not a string" {
		t.Errorf("non-string type with legacy fallback: %v", err)
	}
}

// lootTableSource finds the vanilla loot table JSON: a directory named by
// LOOT_TABLE_DIR, or the data pack in the reference server's jar.
func lootTableSource(t *testing.T) func(name string) ([]byte, error) {
	t.Helper()
	if dir := os.Getenv("LOOT_TABLE_DIR"); dir != "" {
		return func(name string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, strings.TrimPrefix(name, "minecraft:")+".json"))
		}
	}
	jar := "../../../reference-server/versions/" + protocol.GameVersion + "/server-" + protocol.GameVersion + ".jar"
	zr, err := zip.OpenReader(jar)
	if err != nil {
		t.Skip("no reference server jar at " + jar)
	}
	t.Cleanup(func() { zr.Close() })
	return func(name string) ([]byte, error) {
		f, err := zr.Open("data/minecraft/loot_table/" + strings.TrimPrefix(name, "minecraft:") + ".json")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}
}

// TestDecodeLootTableMatchesTheGeneratedTables decodes the vanilla data
// pack's JSON, when the reference server is at hand, and compares each table
// to the one tools/gen loot generated from it.
func TestDecodeLootTableMatchesTheGeneratedTables(t *testing.T) {
	read := lootTableSource(t)
	checked := 0
	for name, want := range registry.LootTables {
		data, err := read(name)
		if err != nil {
			continue
		}
		got, err := DecodeLootTable(data)
		if err != nil && strings.Contains(err.Error(), "inline loot table") {
			continue // the generator hoists inline tables into references
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		checked++
		if canon(reflect.ValueOf(got)) != canon(reflect.ValueOf(want)) {
			t.Errorf("%s decodes differently from the generated table", name)
		}
	}
	if checked == 0 {
		t.Fatal("no table was compared")
	}
	t.Logf("compared %d tables", checked)
}

// canon prints a value with nil and empty slices alike, so two tables that
// differ only in that compare equal. It also prints what the generator
// rewrites as the generator writes it: a predicate reference inlined,
// match_block as any_of over block_state_property, and a number at the
// float precision vanilla reads it with.
func canon(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return "nil"
		}
		return canon(v.Elem())
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = canon(v.Index(i))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case reflect.Map:
		var parts []string
		for _, k := range v.MapKeys() {
			parts = append(parts, fmt.Sprint(k)+":"+canon(v.MapIndex(k)))
		}
		slices.Sort(parts)
		return "{" + strings.Join(parts, ",") + "}"
	case reflect.Float64:
		return fmt.Sprint(float32(v.Float()))
	case reflect.Struct:
		switch c := v.Interface().(type) {
		case registry.LootReferenceCondition:
			if p, ok := registry.FindPredicate(c.Name); ok {
				return canon(reflect.ValueOf(p))
			}
		case registry.LootMatchBlock:
			if c.Blocks != nil {
				var terms []registry.LootCondition
				for _, b := range c.Blocks {
					terms = append(terms, registry.LootBlockState{Block: b, Properties: c.Properties})
				}
				return canon(reflect.ValueOf(registry.LootAnyOf{Terms: terms}))
			}
		}
		parts := make([]string, v.NumField())
		for i := range parts {
			parts[i] = canon(v.Field(i))
		}
		return v.Type().Name() + "(" + strings.Join(parts, ",") + ")"
	}
	return fmt.Sprint(v.Interface())
}

func TestDecodeLootTableRefusesWhatItDoesNotKnow(t *testing.T) {
	for name, body := range map[string]string{
		"syntax":         `{"pools":[`,
		"unknown item":   `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:nope"}]}]}`,
		"unknown entry":  `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:mystery"}]}]}`,
		"unknown cond":   `{"pools":[{"rolls":1,"entries":[],"conditions":[{"condition":"minecraft:time_check"}]}]}`,
		"refused func":   `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:stick","functions":[{"function":"minecraft:set_book_cover","generation":4}]}]}]}`,
		"unknown number": `{"pools":[{"rolls":{"type":"minecraft:score"},"entries":[]}]}`,
	} {
		if _, err := DecodeLootTable([]byte(body)); err == nil {
			t.Errorf("%s: decoded, want a refusal", name)
		}
	}
	table, err := DecodeLootTable([]byte(`{"pools":[{"rolls":2,"entries":[{"type":"minecraft:tag","name":"#minecraft:logs","expand":true}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	tag, ok := table.Pools[0].Entries[0].(registry.LootTag)
	if !ok || len(tag.Items) == 0 || !tag.Expand || tag.Weight != 1 {
		t.Errorf("tag entry = %#v, want the logs of the tag, expanded, weight 1", table.Pools[0].Entries[0])
	}
}

func ghastRecord(tag string) string {
	return `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:music_disc_tears"}],` +
		`"conditions":[{"condition":"minecraft:damage_source_properties","predicate":{"direct_entity":` +
		`{"minecraft:entity_type":"minecraft:fireball"},"tags":[{"id":"` + tag + `","expected":true}]}}]}]}`
}

// TestDecodeLootTableReadsTheGhastDamageSource checks the direct entity plus
// tag shape that vanilla's ghast table carries, without the extracted data,
// with the tag written as a holder set and as a bare tag key.
func TestDecodeLootTableReadsTheGhastDamageSource(t *testing.T) {
	for _, tag := range []string{"#minecraft:is_projectile", "minecraft:is_projectile"} {
		got, err := DecodeLootTable([]byte(ghastRecord(tag)))
		if err != nil {
			t.Fatal(err)
		}
		want := registry.LootDamageSource{DirectType: "minecraft:fireball", DamageTypes: &registry.DamageTypeSet{Tag: registry.DamageTypeTagIDIsProjectile, IsTag: true}}
		if conds := got.Pools[0].Conditions; len(conds) != 1 || !reflect.DeepEqual(conds[0], want) {
			t.Errorf("%s: conditions = %#v, want %#v", tag, conds, want)
		}
	}
}

func damageSourceRecord(id string) string {
	return `{"pools":[{"rolls":1,"entries":[],"conditions":[{"condition":"minecraft:damage_source_properties",` +
		`"predicate":{"tags":[{"id":` + id + `,"expected":true}]}}]}]}`
}

// TestDecodeLootTableReadsADamageSourceIDAsAHolderSet checks that 26.3's
// damage source tag id names one damage type, a list of them or a #tag, and
// that a bare name that is neither a damage type nor a tag is refused.
// vanilla: TagPredicate.codec
func TestDecodeLootTableReadsADamageSourceIDAsAHolderSet(t *testing.T) {
	for id, want := range map[string]registry.LootCondition{
		`"minecraft:in_fire"`:  registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeInFire}}},
		`"in_fire"`:            registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeInFire}}},
		`"#minecraft:is_fire"`: registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Tag: registry.DamageTypeTagIDIsFire, IsTag: true}},
		`"minecraft:is_fire"`:  registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Tag: registry.DamageTypeTagIDIsFire, IsTag: true}},
		`["minecraft:lava"]`:   registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeLava}}},
		`["minecraft:in_fire","minecraft:lava"]`: registry.LootAnyOf{Terms: []registry.LootCondition{
			registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeInFire}}}, registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeLava}}},
		}},
		`[]`: registry.LootAnyOf{Terms: []registry.LootCondition{}},
	} {
		got, err := DecodeLootTable([]byte(damageSourceRecord(id)))
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if conds := got.Pools[0].Conditions; len(conds) != 1 || !reflect.DeepEqual(conds[0], want) {
			t.Errorf("%s: conditions = %#v, want %#v", id, conds, want)
		}
	}
	for _, id := range []string{`"minecraft:no_such_damage"`, `["minecraft:in_fire","minecraft:no_such_damage"]`, `3`} {
		if _, err := DecodeLootTable([]byte(damageSourceRecord(id))); err == nil {
			t.Errorf("%s: decoded, want a refusal", id)
		}
	}
	got, err := DecodeLootTable([]byte(ghastRecord("minecraft:fireball")))
	if err != nil {
		t.Fatal(err)
	}
	want := registry.LootDamageSource{DirectType: "minecraft:fireball", DamageTypes: &registry.DamageTypeSet{Types: []registry.DamageType{registry.DamageTypeFireball}}}
	if conds := got.Pools[0].Conditions; len(conds) != 1 || !reflect.DeepEqual(conds[0], want) {
		t.Errorf("direct entity with one damage type: conditions = %#v, want %#v", conds, want)
	}
}

// TestDecodeLootTableReadsThe263HolderSets checks the fields read as
// holder sets: a tag entry's items, with expand optional, and an
// exploration map's destination, beside the bare tag key.
// vanilla: TagEntry.MAP_CODEC, ExplorationMapFunction.MAP_CODEC
func TestDecodeLootTableReadsThe263HolderSets(t *testing.T) {
	table, err := DecodeLootTable([]byte(`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:tag","items":["minecraft:stick","minecraft:apple"]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := table.Pools[0].Entries[0].(registry.LootTag); !slices.Equal(got.Items, []registry.Item{registry.ItemStick, registry.ItemApple}) || got.Expand {
		t.Errorf("tag entry = %#v, want the two items, not expanded", got)
	}
	for destination, want := range map[string][]string{
		`"#minecraft:mineshaft"`:       {"minecraft:mineshaft", "minecraft:mineshaft_mesa"},
		`"minecraft:mineshaft"`:        {"minecraft:mineshaft"},
		`"minecraft:on_treasure_maps"`: {"minecraft:buried_treasure"},
		`["minecraft:monument"]`:       {"minecraft:monument"},
	} {
		fn, err := DecodeItemModifier([]byte(`{"function":"minecraft:exploration_map","destination":` + destination + `}`))
		if err != nil {
			t.Fatalf("%s: %v", destination, err)
		}
		if got := fn.(registry.LootExplorationMap).Destination; !slices.Equal(got, want) {
			t.Errorf("destination %s = %v, want %v", destination, got, want)
		}
	}
}

// TestDecodeItemPredicateReadsMapIDPresence checks the any-value component
// predicate the treasure-map chests filter with. vanilla: AnyValue
func TestDecodeItemPredicateReadsMapIDPresence(t *testing.T) {
	fn, err := DecodeItemModifier([]byte(`{"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:map_id":{}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := fn.(registry.LootFiltered).Predicate; !p.HasMapID || p.HasDyedColor {
		t.Errorf("predicate = %#v, want map id presence only", p)
	}
}

// TestDecodeLootTableRequiresRollsAndExpand checks the fields vanilla's
// codecs do not default, and that a tag no pack defines holds nothing.
// vanilla: LootPool.CODEC, TagEntry.CODEC
func TestDecodeLootTableRequiresRollsAndExpand(t *testing.T) {
	if _, err := DecodeLootTable([]byte(`{"pools":[{"entries":[]}]}`)); err == nil {
		t.Error("a pool without rolls decoded")
	}
	tag := func(extra string) string {
		return `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:tag","name":"pk:nowhere"` + extra + `}]}]}`
	}
	if _, err := DecodeLootTable([]byte(tag(""))); err == nil {
		t.Error("a tag entry without expand decoded")
	}
	got, err := DecodeLootTable([]byte(tag(`,"expand":true`)))
	if err != nil {
		t.Fatalf("a tag entry naming a missing tag: %v", err)
	}
	if e, ok := got.Pools[0].Entries[0].(registry.LootTag); !ok || len(e.Items) != 0 || !e.Expand {
		t.Errorf("entry = %#v, want an empty expanding tag", got.Pools[0].Entries[0])
	}
}

// TestDecodeLootTableSetNameTargets checks that set_name reads its target:
// item_name sets the item name, the default is the custom name.
// vanilla: SetNameFunction.CODEC
func TestDecodeLootTableSetNameTargets(t *testing.T) {
	for target, want := range map[string]bool{`,"target":"item_name"`: true, `,"target":"custom_name"`: false, ``: false} {
		body := `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:stick","functions":` +
			`[{"function":"minecraft:set_name","name":{"text":"Hi"}` + target + `}]}]}]}`
		table, err := DecodeLootTable([]byte(body))
		if err != nil {
			t.Fatalf("%q: %v", target, err)
		}
		fns := table.Pools[0].Entries[0].(registry.LootItemEntry).Functions
		got, ok := fns[0].(registry.LootSetName)
		if !ok || got.ItemName != want {
			t.Errorf("%q: function = %#v, want ItemName %v", target, fns[0], want)
		}
	}
}

func TestDecodeTradeEnchantmentFunctionsAndStoredFilter(t *testing.T) {
	fn, err := DecodeItemModifier([]byte(`[
  {"function":"minecraft:enchant_randomly","options":["minecraft:mending"],"include_additional_cost_component":true},
  {"function":"minecraft:enchant_with_levels","levels":19,"include_additional_cost_component":true},
  {"function":"minecraft:filtered","item_filter":{"items":"minecraft:enchanted_book","predicates":{"minecraft:stored_enchantments":[{"enchantments":"minecraft:mending","levels":1}]}},"on_fail":{"function":"minecraft:discard"}}
 ]`))
	if err != nil {
		t.Fatal(err)
	}
	seq := fn.(registry.LootFunctionSequence).Functions
	if !seq[0].(registry.LootEnchantRandomly).IncludeAdditionalCost || !seq[1].(registry.LootEnchantWithLevels).IncludeAdditionalCost {
		t.Fatal("lost the price flags")
	}
	filter := seq[2].(registry.LootFiltered)
	if len(filter.Predicate.StoredEnchantments) != 1 || filter.Predicate.StoredEnchantments[0].Min != 1 {
		t.Fatalf("stored predicate %+v", filter.Predicate)
	}
	if _, ok := filter.OnFail.(registry.LootDiscard); !ok {
		t.Fatalf("on_fail %T", filter.OnFail)
	}
	if _, err := DecodeItemModifier([]byte(`{"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:unknown":{}}}}`)); err == nil {
		t.Fatal("accepted an unmodelled component predicate")
	}
}

func TestDecodeTradeComponentFunctionsAndDyeFilter(t *testing.T) {
	fn, err := DecodeItemModifier([]byte(`[
  {"function":"minecraft:set_random_dyes","number_of_dyes":{"type":"minecraft:add","inputs":[1,{"type":"minecraft:binomial","n":2,"p":0.75}]}},
  {"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:dyed_color":{}}},"on_fail":{"function":"minecraft:discard"}},
  {"function":"minecraft:set_random_potion","options":"#minecraft:tradeable"},
  {"function":"minecraft:set_random_potion","options":[]},
  {"function":"minecraft:set_random_potion"}
 ]`))
	if err != nil {
		t.Fatal(err)
	}
	seq := fn.(registry.LootFunctionSequence).Functions
	sum := seq[0].(registry.LootSetRandomDyes).Count.(registry.LootSum)
	if len(sum.Summands) != 2 || sum.Summands[1].(registry.LootBinomial).P.(registry.LootConstant).Value != 0.75 {
		t.Fatalf("sum %+v", sum)
	}
	if !seq[1].(registry.LootFiltered).Predicate.HasDyedColor {
		t.Fatal("lost colour presence predicate")
	}
	options := seq[2].(registry.LootSetRandomPotion).Options
	if len(options) != 41 || !slices.Contains(options, "minecraft:long_invisibility") || slices.Contains(options, "minecraft:water") {
		t.Fatalf("tradeable potions %v", options)
	}
	if seq[3].(registry.LootSetRandomPotion).Options == nil || seq[4].(registry.LootSetRandomPotion).Options != nil {
		t.Fatal("empty and absent potion options are different")
	}
	if _, err := DecodeItemModifier([]byte(`{"function":"minecraft:filtered","item_filter":{"predicates":{"minecraft:dyed_color":{"unknown":1}}}}`)); err == nil {
		t.Fatal("accepted an unmodelled dye predicate")
	}
}

// TestDecodeItemModifierComponentFunctions checks the component-editing
// functions decode, and their bad fields are refused.
// vanilla: SetLoreFunction, ListOperation.codec, ToggleTooltips
func TestDecodeItemModifierComponentFunctions(t *testing.T) {
	fn, err := DecodeItemModifier([]byte(`[
  {"function":"minecraft:set_lore","lore":["a",{"text":"b"}],"mode":"replace_section","offset":1,"size":2},
  {"function":"minecraft:set_custom_data","tag":"{a:1}"},
  {"function":"minecraft:toggle_tooltips","toggles":{"minecraft:enchantments":false}},
  {"function":"minecraft:copy_name","source":"block_entity"},
  {"function":"minecraft:set_attributes","modifiers":[{"id":"x:y","attribute":"minecraft:armor","operation":"add_value","amount":1,"slot":["head","chest"]}]}
 ]`))
	if err != nil {
		t.Fatal(err)
	}
	seq := fn.(registry.LootFunctionSequence).Functions
	lore := seq[0].(registry.LootSetLore)
	if len(lore.Lines) != 2 || lore.Operation != (registry.LootListOperation{Mode: "replace_section", Offset: 1, Size: 2}) {
		t.Errorf("set_lore = %#v", lore)
	}
	if seq[1].(registry.LootSetCustomData).Tag.Entries()["a"] == nil {
		t.Error("set_custom_data lost its tag")
	}
	if seq[2].(registry.LootToggleTooltips).Toggles["minecraft:enchantments"] {
		t.Error("toggle_tooltips lost its false")
	}
	if seq[3].(registry.LootCopyName).Source != "block_entity" {
		t.Error("copy_name lost its source")
	}
	if attrs := seq[4].(registry.LootSetAttributes); !attrs.Replace || len(attrs.Modifiers[0].Slots) != 2 {
		t.Errorf("set_attributes = %#v", attrs)
	}
	for _, bad := range []string{
		`{"function":"minecraft:set_lore","lore":["a"],"mode":"nope"}`,
		`{"function":"minecraft:set_lore","lore":["a"],"mode":"replace_section","size":300}`,
		`{"function":"minecraft:set_custom_data","tag":"{"}`,
		`{"function":"minecraft:copy_name","source":"nobody"}`,
		`{"function":"minecraft:set_attributes","modifiers":[{"id":"x:y","attribute":"minecraft:armor","operation":"add_value","amount":1,"slot":[]}]}`,
	} {
		if _, err := DecodeItemModifier([]byte(bad)); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}

func TestDecodePredicateMatchBlockKeepsAbsentBlocksNil(t *testing.T) {
	got, err := DecodePredicate([]byte(`{"type":"minecraft:match_block","state":{"half":"lower"}}`))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(registry.LootMatchBlock)
	if !ok || m.Blocks != nil || len(m.Properties) != 1 {
		t.Errorf("decoded %#v, want state-only match_block with nil Blocks", got)
	}
	got, err = DecodePredicate([]byte(`{"condition":"minecraft:match_block","blocks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := got.(registry.LootMatchBlock); !ok || m.Blocks == nil || len(m.Blocks) != 0 {
		t.Errorf("decoded %#v, want empty non-nil Blocks", got)
	}
}

func TestDecodePredicateLevelAndValueChecks(t *testing.T) {
	for _, in := range []string{
		`{"condition":"minecraft:time_check","clock":"overworld","period":24000,"value":{"min":0,"max":12000}}`,
		`{"condition":"minecraft:weather_check","raining":true}`,
		`{"condition":"minecraft:entity_scores","entity":"attacker","scores":{"a":1,"b":{"min":{"type":"minecraft:uniform","min":0,"max":2}}}}`,
		`{"condition":"minecraft:enchantment_active_check","active":false}`,
		`{"condition":"minecraft:environment_attribute_check","attribute":"minecraft:gameplay/sky_light_level","value":15}`,
		`{"condition":"minecraft:int_value_check","value":{"type":"minecraft:constant","value":3},"test":{"max":4}}`,
		`{"condition":"minecraft:float_value_check","value":0.5,"test":0.5}`,
		`{"condition":"minecraft:int_value_check","value":{"type":"minecraft:score","target":"this","score":"a"},"test":1}`,
	} {
		if _, err := DecodePredicate([]byte(in)); err != nil {
			t.Errorf("%s: %v", in, err)
		}
	}
	for in, want := range map[string]string{
		`{"condition":"minecraft:environment_attribute_check","attribute":"minecraft:gameplay/can_start_raid","value":true}`: "environment attribute minecraft:gameplay/can_start_raid is not supported by this server",
		`{"condition":"minecraft:entity_scores","entity":"nobody","scores":{}}`:                                              "Unknown entity target: nobody",
		`{"condition":"minecraft:int_value_check","value":{"type":"minecraft:nope"},"test":1}`:                               "Failed to parse either. First: Not a number; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:context_int_provider_type]: minecraft:nope",
		`{"condition":"minecraft:time_check","clock":"minecraft:overworld"}`:                                                 `No key value in MapLike[{clock:"minecraft:overworld",condition:"minecraft:time_check"}]`,
		`{"condition":"minecraft:time_check","clock":"minecraft:x","value":1}`:                                               "Failed to get element minecraft:x from registry minecraft:world_clock",
	} {
		if _, err := DecodePredicate([]byte(in)); err == nil || err.Error() != want {
			t.Errorf("%s: got %v, want %q", in, err, want)
		}
	}
}

// vanilla: RaiderPredicate.CODEC
func TestDecodeRaiderLootPredicateDefaultsAndFlags(t *testing.T) {
	for _, tc := range []struct {
		raw           string
		raid, captain bool
	}{
		{`{}`, false, false},
		{`{"is_captain":true}`, false, true},
		{`{"has_raid":true}`, true, false},
		{`{"has_raid":true,"is_captain":true}`, true, true},
		{`{"has_raid":false,"is_captain":false}`, false, false},
	} {
		cond, err := DecodePredicate([]byte(`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:type_specific/raider":` + tc.raw + `}}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		p := cond.(registry.LootEntityProperties).Predicate
		if p.HasRaid == nil || p.Captain == nil || *p.HasRaid != tc.raid || *p.Captain != tc.captain {
			t.Errorf("%s: predicate = %+v", tc.raw, p)
		}
	}
}

// TestDecodePredicateRefusesAnUnknownSlotRange checks that a slots predicate
// naming no slot range is refused at load.
// vanilla: SlotRanges.CODEC
func TestDecodePredicateRefusesAnUnknownSlotRange(t *testing.T) {
	old := SlotRangeKnown
	t.Cleanup(func() { SlotRangeKnown = old })
	SlotRangeKnown = func(name string) bool { return name == "weapon.mainhand" }
	slots := func(name string) string {
		return `{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:slots":{"` + name + `":{"items":"minecraft:stick"}}}}`
	}
	if _, err := DecodePredicate([]byte(slots("weapon.mainhand"))); err != nil {
		t.Errorf("known slot range: %v", err)
	}
	if _, err := DecodePredicate([]byte(slots("nope.0"))); err == nil {
		t.Error("unknown slot range decoded, want a refusal")
	}
}
