package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestPackLootTableModifierShape checks that a table written in the 26.3
// shape is rolled as written: entry, pool and table modifiers given inline,
// as a list and by an item modifier's name, functions keyed by type, and
// conditions given inline and by a predicate's name. Read in the 26.2 shape,
// the alternatives would pick the diamond and no function would run.
// vanilla: LootTable.DIRECT_CODEC, LootPool.CODEC, LootPoolEntryContainer.commonFields
func TestPackLootTableModifierShape(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	writeFile(t, filepath.Join(data, "item_modifier", "plus_two.json"),
		`{"type":"minecraft:set_count","count":2,"add":true}`)
	writeFile(t, filepath.Join(data, "predicate", "always.json"),
		`{"type":"minecraft:random_chance","chance":1}`)
	writeFile(t, filepath.Join(data, "loot_table", "shaped.json"), `{
		"modifier":{"type":"minecraft:set_count","count":1,"add":true},
		"pools":[{"rolls":1,"condition":"pk:always","modifier":"pk:plus_two","entries":[
			{"type":"minecraft:alternatives","children":[
				{"type":"minecraft:item","name":"minecraft:diamond",
					"condition":{"type":"minecraft:random_chance","chance":0}},
				{"type":"minecraft:item","name":"minecraft:emerald","modifier":[
					{"type":"minecraft:set_count","count":3},
					{"type":"minecraft:set_count","count":50,
						"condition":{"type":"minecraft:inverted","term":"pk:always"}},
					"pk:plus_two"]}]}]}]}`)
	_, c := joinPacks(t, cfg)

	// 3, the inverted condition skips the 50, then +2 by name in the entry's
	// list, +2 by the pool's named modifier and +1 by the table's.
	expectGave(t, c, "loot give @s loot pk:shaped", "minecraft:emerald", 8)
}
