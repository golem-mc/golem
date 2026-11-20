package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestCopyComponentsFromToolAndEntity checks that a pack's copy_components
// reads the breaking tool's custom_data and a dying mob's custom_name.
// vanilla: CopyComponentsFunction.run, LootContextArg
func TestCopyComponentsFromToolAndEntity(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table")
	// dirt names its include; gravel omits it and excludes custom_data.
	copyTable := func(item, source, component string) string {
		return `{"type":"minecraft:block","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"` + item +
			`","functions":[{"function":"minecraft:copy_components","source":"` + source +
			`","include":["` + component + `"]}]}]}]}`
	}
	writeFile(t, filepath.Join(tables, "blocks", "dirt.json"), copyTable("minecraft:dirt", "tool", "minecraft:custom_data"))
	writeFile(t, filepath.Join(tables, "entities", "sheep.json"), copyTable("minecraft:white_wool", "this", "minecraft:custom_name"))
	writeFile(t, filepath.Join(tables, "blocks", "gravel.json"), `{"type":"minecraft:block","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:gravel","functions":[{"function":"minecraft:copy_components","source":"tool","exclude":["minecraft:custom_data"]}]}]}]}`)
	srv, c := joinPacks(t, cfg)

	servertest.Setblock(t, c, 0, 64, 2, "minecraft:dirt")
	c.ChatCommand(`item replace entity @s hotbar.0 with minecraft:iron_shovel[minecraft:custom_data={tag:1b},minecraft:custom_name='"Spade"']`)
	c.Settle()
	c.BreakBlock(0, 64, 2)
	servertest.UntilTicks(t, srv, 40, "the dirt drop carrying the tool's custom data", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:dirt" {
				_, ok := registry.StackComponents(it.Stack)["minecraft:custom_data"]
				return ok
			}
		}
		return false
	})

	// An absent include copies every component but the excluded ones.
	servertest.Setblock(t, c, 0, 64, 3, "minecraft:gravel")
	c.BreakBlock(0, 64, 3)
	servertest.UntilTicks(t, srv, 40, "the gravel drop with the name but not the custom data", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:gravel" {
				comps := registry.StackComponents(it.Stack)
				_, name := comps["minecraft:custom_name"]
				_, data := comps["minecraft:custom_data"]
				return name && !data
			}
		}
		return false
	})

	srv.RunLine(`summon minecraft:sheep 4.5 64 5.5 {NoAI:1b,CustomName:"Dolly"}`)
	c.Settle()
	for _, id := range srv.MobIDsOfKind("minecraft:sheep") {
		srv.KillMob(id)
	}
	servertest.UntilTicks(t, srv, 40, "the wool carrying the sheep's name", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:white_wool" {
				_, ok := registry.StackComponents(it.Stack)["minecraft:custom_name"]
				return ok
			}
		}
		return false
	})
}
