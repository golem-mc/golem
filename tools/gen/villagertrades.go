package main

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "villagertrades",
		doc:  "registry/villagertrades_gen.go: every trade set, with the trades it draws from",
		run:  runVillagerTrades,
	})
}

// Inputs: the data pack's trade_set files, the villager_trade tags they name
// and the villager_trade files those list, in tag order (the draw indexes the
// list). A trade is read as plain data: wants, an optional second want, gives,
// max uses, xp, the reputation discount and the villager variants it is
// restricted to. Number providers must be constants. Given-item modifiers
// and wanted-item components outside the model are marked Unsupported so
// the draw still counts them. Enchantment, dye, stew and potion modifiers and their result filters
// and explorer maps are evaluated. Any other key,
// or a predicate other than a villager variant list, stops the generation.

var vtKeys = []string{
	"wants", "additional_wants", "gives", "max_uses", "reputation_discount", "xp",
	"merchant_predicate", "given_item_modifiers", "given_item_modifier", "double_trade_price_enchantments",
}

func runVillagerTrades(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	items := registries["minecraft:item"].Entries
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	readTag := func(dir, name string) []any {
		file := path.Join("minecraft/tags", dir, strings.TrimPrefix(name, "minecraft:")+".json")
		b, err := dpRead(dp, file)
		if err != nil {
			fail("%v", err)
		}
		return decode(b, file).list("values")
	}
	cost := func(o obj, what string, defCount int) string {
		id := o.str("id")
		if _, ok := items.Get(id); !ok {
			fail("%s: no such item %s", what, id)
		}
		out := "TradeCost{Item: " + gogen.Quote(id) + ", Count: " + gogen.Int(o.intOr("count", defCount))
		if isWater(o) {
			out += ", Water: true"
		}
		return out + "}"
	}
	// modelled is whether a cost's component predicate is one the server
	// evaluates: none, or the plain water potion.
	modelled := func(o obj) bool { return !o.has("components") || isWater(o) }
	lr := &lootReader{dp: dp}
	readTrade := func(id string) string {
		file := path.Join("minecraft/villager_trade", strings.TrimPrefix(id, "minecraft:")+".json")
		b, err := dpRead(dp, file)
		if err != nil {
			fail("%v", err)
		}
		t := decode(b, file)
		for k := range t {
			if !slices.Contains(vtKeys, k) {
				fail("%s: key %q is not modelled", file, k)
			}
		}
		for _, k := range []string{"max_uses", "reputation_discount", "xp"} {
			if t.has(k) {
				if _, ok := asNum(t[k]); !ok {
					fail("%s: %s is not a constant", file, k)
				}
			}
		}
		wants, _ := t.obj("wants")
		gives, _ := t.obj("gives")
		unsupported := !modelled(wants) || gives.has("components")
		modifiers, supported := tradeModifiers(lr, (lootCodec{dp}).modifiers(t, "given_item_modifier"), file)
		unsupported = unsupported || !supported
		extra, hasExtra := t.obj("additional_wants")
		unsupported = unsupported || hasExtra && !modelled(extra)
		out := "{Wants: " + cost(wants, file, 1) + ", "
		if hasExtra {
			out += "Extra: " + cost(extra, file, 1) + ", "
		}
		out += "Gives: " + cost(gives, file, 1) +
			", MaxUses: " + gogen.Int(t.intOr("max_uses", 4)) +
			", XP: " + gogen.Int(t.intOr("xp", 1)) +
			", Discount: " + gogen.Float(t.numOr("reputation_discount", 0))
		if pred := (lootCodec{dp}).optionalPredicate(t, "merchant_predicate"); pred != nil {
			out += ", Variants: []string{" + strings.Join(variantsOf(pred, file), ", ") + "}"
		}
		if supported && len(modifiers) > 0 {
			out += ", Modifiers: []LootFunction{" + strings.Join(modifiers, ", ") + "}"
		}
		if v, ok := t["double_trade_price_enchantments"]; ok {
			out += ", DoublePrice: []string{" + quotedList(lr.nameList("enchantment", v)) + "}"
		}
		if unsupported {
			out += ", Unsupported: true"
		}
		return out + "}"
	}

	files, err := vanilla.Files(dp, "minecraft/trade_set", ".json")
	if err != nil {
		return err
	}
	var sets, tagsBody strings.Builder
	tags := map[string][]string{}
	tradeIDs := map[string]bool{}
	// readTagOnce records the tag and the tags it nests, with their entries as
	// the file lists them, so a pack's change to one reaches the sets.
	var readTagOnce func(name string, seen []string)
	readTagOnce = func(name string, seen []string) {
		if slices.Contains(seen, name) {
			fail("trade tag %s contains itself", name)
		}
		if _, ok := tags[name]; ok {
			return
		}
		values := []string{}
		for _, v := range readTag("villager_trade", name) {
			value := asStr(v, "trade tag "+name)
			values = append(values, value)
			if nested, ok := strings.CutPrefix(value, "#"); ok {
				readTagOnce(nested, append(slices.Clone(seen), name))
			} else {
				tradeIDs[value] = true
			}
		}
		tags[name] = values
	}
	for _, name := range files {
		file := path.Join("minecraft/trade_set", name)
		set := decode(mustRead(dp, file), file)
		amount, ok := asNum(set["amount"])
		if !ok {
			fail("%s: amount is not a constant", file)
		}
		tag, ok := set["trades"].(string)
		if !ok || !strings.HasPrefix(tag, "#") {
			fail("%s: trades is not a tag", file)
		}
		readTagOnce(tag[1:], nil)
		fmt.Fprintf(&sets, "\t%s: {Amount: %d, Duplicates: %t, Tag: %s},\n", gogen.Quote(strings.TrimSuffix(name, ".json")), int(amount), set.boolOr("allow_duplicates", false), gogen.Quote(tag[1:]))
	}
	for _, name := range slices.Sorted(maps.Keys(tags)) {
		fmt.Fprintf(&tagsBody, "\t%s: {%s},\n", gogen.Quote(name), quotedList(tags[name]))
	}
	var body strings.Builder
	body.WriteString(`// builtinTrades is the data pack's villager_trade registry, by id.
var builtinTrades = map[string]VillagerTrade{
`)
	for _, id := range slices.Sorted(maps.Keys(tradeIDs)) {
		fmt.Fprintf(&body, "\t%s: %s,\n", gogen.Quote(id), readTrade(id))
	}
	body.WriteString("}\n\n// builtinTradeTags is the villager_trade tags the sets draw from, as the files list them.\nvar builtinTradeTags = map[string][]string{\n")
	body.WriteString(tagsBody.String())
	body.WriteString(`}

// builtinTradeSets is the data pack's trade_set registry, by name under
// minecraft:trade_set without the extension.
var builtinTradeSets = map[string]tradeSetDef{
`)
	body.WriteString(sets.String())
	body.WriteString("}\n")
	if err := needAtLeast(len(files), 50, "trade sets"); err != nil {
		return err
	}
	e.logf("wrote %d trade sets, %d trades", len(files), len(tradeIDs))
	f := e.file("registry", "by tools/gen villagertrades from a vanilla {version} server")
	if strings.Contains(body.String(), "protocol.") {
		f.WriteString("import \"github.com/golem-mc/golem/internal/protocol\"\n\n")
	}
	f.WriteString(body.String())
	return e.emit("registry/villagertrades_gen.go", f)
}

func mustRead(dp *vanilla.DataPack, file string) []byte {
	b, err := dpRead(dp, file)
	if err != nil {
		fail("%v", err)
	}
	return b
}

// variantsOf reads a merchant predicate that tests the villager's variant.
func variantsOf(pred obj, file string) []string {
	if pred.str("condition") != "minecraft:entity_properties" || pred.str("entity") != "this" {
		fail("%s: merchant_predicate is not an entity_properties test of this", file)
	}
	inner, _ := pred.obj("predicate")
	preds, _ := inner.obj("minecraft:predicates")
	if len(inner) != 1 || len(preds) != 1 || !preds.has("minecraft:villager/variant") {
		fail("%s: merchant_predicate tests more than the villager variant", file)
	}
	var out []string
	switch v := preds["minecraft:villager/variant"].(type) {
	case string:
		out = []string{gogen.Quote(v)}
	case []any:
		for _, x := range v {
			out = append(out, gogen.Quote(asStr(x, file)))
		}
	default:
		fail("%s: villager variant is not a name or a list", file)
	}
	return out
}

// isWater is whether a cost's components are exactly minecraft:potion_contents
// naming the water potion, the one predicate any trade uses.
func isWater(o obj) bool {
	components, ok := o.obj("components")
	if !ok || len(components) != 1 {
		return false
	}
	contents, ok := components.obj("minecraft:potion_contents")
	return ok && len(contents) == 1 && contents.strOr("potion", "") == "minecraft:water"
}

// tradeModifiers checks every field before using the shared loot generator.
// Unknown shapes stop generation; explicitly unimplemented families skip the trade.
func tradeModifiers(r *lootReader, list []any, file string) ([]string, bool) {
	supported := true
	var out []string
	for _, v := range list {
		f := asObj(v, file)
		var keys []string
		switch f.str("function") {
		case "minecraft:enchant_randomly":
			keys = []string{"function", "options", "only_compatible", "include_additional_cost_component"}
		case "minecraft:enchant_with_levels":
			keys = []string{"function", "options", "levels", "include_additional_cost_component"}
		case "minecraft:filtered":
			keys = []string{"function", "item_filter", "on_fail"}
			failure, _ := f.obj("on_fail")
			if !keysWithin(failure, "function") || failure.strOr("function", "") != "minecraft:discard" {
				fail("%s: trade filter must discard on failure", file)
			}
		case "minecraft:set_stew_effect":
			keys = []string{"function", "effects"}
			for _, v := range f.listOr("effects") {
				e := asObj(v, file)
				if !keysWithin(e, "type", "duration") {
					fail("%s: unmodelled stew effect fields", file)
				}
			}
		case "minecraft:set_random_potion":
			keys = []string{"function", "options"}
		case "minecraft:set_random_dyes":
			keys = []string{"function", "number_of_dyes"}
		case "minecraft:set_potion":
			keys = []string{"function", "id"}
		case "minecraft:exploration_map":
			keys = []string{"function", "destination", "decoration", "zoom", "search_radius", "skip_existing_chunks"}
		case "minecraft:set_name":
			keys = []string{"function", "name", "target"}
		default:
			fail("%s: unknown trade modifier %s", file, f.str("function"))
		}
		if !keysWithin(f, keys...) {
			fail("%s: unmodelled trade modifier fields %v", file, sortedKeys(f))
		}
	}
	// Generate only after every modifier's shape has been checked.
	if supported {
		for _, v := range list {
			out = append(out, r.function(asObj(v, file), file))
		}
	}
	return out, supported
}
