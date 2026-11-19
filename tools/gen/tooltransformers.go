package main

import (
	"errors"
	"io/fs"
	"slices"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// transformerTables reads the data-driven replacements of AxeItem and ShovelItem.
// vanilla: BlockTransformer, RuleBasedBlockStateProvider, CopyPropertiesProvider
func transformerTables(e *env) (map[string]map[string]string, bool) {
	dp, err := e.ref.DataPack()
	if err != nil {
		fail("%v", err)
	}
	defer dp.Close()
	file := "minecraft/block_transformer/axe.json"
	if _, err := fs.Stat(dp.FS, file); errors.Is(err, fs.ErrNotExist) {
		return nil, false
	} else if err != nil {
		fail("%v", err)
	}
	blocks, err := e.ref.Blocks()
	if err != nil {
		fail("%v", err)
	}
	tables := map[string]map[string]string{"STRIP": {}, "SCRAPE": {}, "WAXOFF": {}, "FLATTEN": {}}
	readAxeTransformers(dp, blocks, decodeValue(mustRead(dp, file), file), tables)
	file = "minecraft/block_transformer/shovel.json"
	readShovelTransformers(dp, blocks, decodeValue(mustRead(dp, file), file), tables["FLATTEN"])
	return tables, true
}

func transformerEntries(raw any) []any {
	entries, ok := raw.([]any)
	if !ok || len(entries) == 0 {
		fail("expected a nonempty block transformer sequence: %v", raw)
	}
	return entries
}

func transformRules(entry obj) []any {
	provider := asObj(entry["block_state_provider"], "a transformation provider")
	if !keysAre(provider, "type", "rules") || provider.str("type") != "minecraft:rule_based" || len(provider.list("rules")) == 0 {
		fail("unsupported transformation provider: %v", provider)
	}
	return provider.list("rules")
}

func transformTarget(blocks vanilla.Blocks, raw any) string {
	state := asObj(raw, "a target block state")
	if !keysWithin(state, "id", "properties") {
		fail("unsupported target block state: %v", state)
	}
	name := state.str("id")
	block, ok := blocks.Get(name)
	if !ok {
		fail("unknown target block: %s", name)
	}
	var defaults map[string]string
	found := false
	for _, s := range block.States {
		if s.Default {
			defaults = s.Properties
			found = true
		}
	}
	if !found {
		fail("%s: no default state in the block report", name)
	}
	if props, ok := state.obj("properties"); ok {
		for property, value := range props {
			if defaults[property] != asStr(value, "a target state property") {
				fail("%s: transformation target is not the default state", name)
			}
		}
	}
	return name
}

func addTransformation(table map[string]string, blocks vanilla.Blocks, from, to string, copyProperties bool) {
	source, ok := blocks.Get(from)
	if !ok {
		fail("unknown transformation source: %s", from)
	}
	if _, duplicate := table[from]; duplicate {
		fail("multiple transformations for %s", from)
	}
	if copyProperties {
		target, _ := blocks.Get(to)
		if len(source.Properties) != len(target.Properties) {
			fail("%s -> %s: transformation properties differ", from, to)
		}
		for key, values := range source.Properties {
			if !jsonEqual(decodeValue(values, key), decodeValue(target.Properties[key], key)) {
				fail("%s -> %s: transformation property %s differs", from, to, key)
			}
		}
	}
	table[from] = to
}

func readAxeTransformers(dp *vanilla.DataPack, blocks vanilla.Blocks, raw any, tables map[string]map[string]string) {
	chests := blockTagMembers(dp, "#minecraft:copper_chests", nil)
	doors := blockTagMembers(dp, "#minecraft:doors", nil)
	lastRank := -1
	for _, raw := range transformerEntries(raw) {
		entry := asObj(raw, "an axe transformer")
		if !keysWithin(entry, "block_state_provider", "item_damage_per_use", "sound", "particle", "transform_type", "update_from_neighbors") || entry["item_damage_per_use"] != int64(1) {
			fail("unsupported axe transformer: %v", entry)
		}
		kind, particle, rank := "", "", 0
		switch entry.str("sound") {
		case "minecraft:item.axe.strip":
			kind = "STRIP"
		case "minecraft:item.axe.scrape":
			kind, particle, rank = "SCRAPE", "scrape", 1
		case "minecraft:item.axe.wax_off":
			kind, particle, rank = "WAXOFF", "wax_off", 2
		default:
			fail("unsupported axe sound: %v", entry)
		}
		if rank < lastRank || entry.strOr("particle", "") != particle || entry.strOr("transform_type", "normal") != "normal" && entry.strOr("transform_type", "normal") != "copper_chest" {
			fail("unsupported axe order, particle or transformation type: %v", entry)
		}
		lastRank = rank
		for _, raw := range transformRules(entry) {
			rule := asObj(raw, "an axe transformation rule")
			predicate := asObj(rule["if_true"], "an axe block predicate")
			then := asObj(rule["then"], "an axe state provider")
			if !keysAre(rule, "if_true", "then") || !keysAre(predicate, "type", "blocks") || predicate.str("type") != "minecraft:matching_blocks" ||
				!keysAre(then, "type", "source") || then.str("type") != "minecraft:copy_properties" {
				fail("unsupported axe rule: %v", rule)
			}
			from := predicate.str("blocks")
			to := transformTarget(blocks, then["source"])
			chest, door := slices.Contains(chests, from), slices.Contains(doors, from)
			transformType := "normal"
			if chest {
				transformType = "copper_chest"
			}
			if entry.strOr("transform_type", "normal") != transformType || entry.boolOr("update_from_neighbors", true) != (!chest && !door) {
				fail("%s: unsupported transformation side effects", from)
			}
			source, _ := blocks.Get(from)
			if kind == "STRIP" && (len(source.Properties) != 1 || source.Properties["axis"] == nil) {
				fail("%s: stripping needs more than the axis", from)
			}
			addTransformation(tables[kind], blocks, from, to, true)
		}
	}
}

func readShovelTransformers(dp *vanilla.DataPack, blocks vanilla.Blocks, raw any, table map[string]string) {
	entries := transformerEntries(raw)
	if len(entries) != 1 {
		fail("unsupported shovel transformer sequence: %v", raw)
	}
	entry := asObj(entries[0], "a shovel transformer")
	if !keysAre(entry, "block_state_provider", "disallowed_faces", "item_damage_per_use", "sound") ||
		!jsonEqual(entry["disallowed_faces"], []any{"down"}) || entry["item_damage_per_use"] != int64(1) || entry.str("sound") != "minecraft:item.shovel.flatten" {
		fail("unsupported shovel transformer: %v", entry)
	}
	rules := transformRules(entry)
	if len(rules) != 1 {
		fail("unsupported shovel rules: %v", rules)
	}
	rule := asObj(rules[0], "a shovel rule")
	expected := decode([]byte(`{"type":"minecraft:all_of","predicates":[{"type":"minecraft:matching_block_tag","tag":"minecraft:turns_into_dirt_path"},{"type":"minecraft:matching_block_tag","offset":[0,1,0],"tag":"minecraft:air"}]}`), "shovel predicate")
	if !keysAre(rule, "if_true", "then") || !jsonEqual(rule["if_true"], map[string]any(expected)) {
		fail("unsupported shovel predicate: %v", rule)
	}
	to := transformTarget(blocks, rule["then"])
	for _, from := range blockTagMembers(dp, "#minecraft:turns_into_dirt_path", nil) {
		addTransformation(table, blocks, from, to, false)
	}
}
