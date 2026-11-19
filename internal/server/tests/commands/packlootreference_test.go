package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPackLootReferenceFunctionRunsANamedModifier checks that a data pack's
// loot table runs a pack item modifier named by a minecraft:reference
// function, a list modifier included, that a modify_contents names one by
// id, and that an unknown or self-referring name leaves the stack. The pack
// layer is process-wide, so this is not parallel with the other tests that
// load loot.
// vanilla: FunctionReference, LootItemFunctions.CODEC
func TestPackLootReferenceFunctionRunsANamedModifier(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	modifiers := filepath.Join(data, "item_modifier")
	writeFile(t, filepath.Join(modifiers, "six.json"),
		`[{"function":"minecraft:set_count","count":4},{"function":"minecraft:set_count","count":2,"add":true}]`)
	writeFile(t, filepath.Join(modifiers, "loop.json"),
		`[{"function":"minecraft:set_count","count":2,"add":true},{"function":"minecraft:reference","name":"pk:loop"}]`)
	tables := filepath.Join(data, "loot_table")
	for name, body := range map[string]string{
		"ref":     oneItemTable("minecraft:stone", `[{"function":"minecraft:reference","name":"pk:six"}]`),
		"missing": oneItemTable("minecraft:dirt", `[{"function":"minecraft:reference","name":"pk:absent"}]`),
		"loop":    oneItemTable("minecraft:cobblestone", `[{"function":"minecraft:reference","name":"pk:loop"}]`),
		"box": oneItemTable("minecraft:shulker_box",
			`[{"function":"minecraft:set_contents","component":"minecraft:container","entries":[{"type":"minecraft:item","name":"minecraft:stick"}]},`+
				`{"function":"minecraft:modify_contents","component":"minecraft:container","modifier":"pk:six"}]`),
	} {
		writeFile(t, filepath.Join(tables, name+".json"), body)
	}
	_, c := joinPacks(t, cfg)

	expectGave(t, c, "loot give @s loot pk:ref", "minecraft:stone", 6)
	expectGave(t, c, "loot give @s loot pk:missing", "minecraft:dirt", 1)
	// The inner reference to pk:loop is the loop and runs nothing.
	expectGave(t, c, "loot give @s loot pk:loop", "minecraft:cobblestone", 3)

	box := giveStack(t, c, "loot give @s loot pk:box")
	if len(box.Container) != 1 || box.Container[0].ItemID != servertest.MustItem("minecraft:stick") || box.Container[0].Count != 6 {
		t.Errorf("modify_contents naming pk:six left the box holding %+v, want six sticks", box.Container)
	}
}
