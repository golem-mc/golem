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
	copyTable := func(item, source, component string) string {
		return `{"type":"minecraft:block","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"` + item +
			`","functions":[{"function":"minecraft:copy_components","source":"` + source +
			`","include":["` + component + `"]}]}]}]}`
	}
	writeFile(t, filepath.Join(tables, "blocks", "dirt.json"), copyTable("minecraft:dirt", "tool", "minecraft:custom_data"))
	writeFile(t, filepath.Join(tables, "entities", "sheep.json"), copyTable("minecraft:white_wool", "this", "minecraft:custom_name"))
	srv, c := joinPacks(t, cfg)

	servertest.Setblock(t, c, 0, 64, 2, "minecraft:dirt")
	c.ChatCommand(`item replace entity @s hotbar.0 with minecraft:iron_shovel[minecraft:custom_data={tag:1b}]`)
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
