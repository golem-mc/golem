package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestCopyComponentsAbsentIncludeKeepsEnchantments checks that a
// copy_components with no include carries the tool's enchantments, and an
// entity source with no include carries its custom_name.
// vanilla: CopyComponentsFunction.run
func TestCopyComponentsAbsentIncludeKeepsEnchantments(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table")
	table := func(item, source string) string {
		return `{"type":"minecraft:block","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"` + item +
			`","functions":[{"function":"minecraft:copy_components","source":"` + source + `"}]}]}]}`
	}
	writeFile(t, filepath.Join(tables, "blocks", "coarse_dirt.json"), table("minecraft:coarse_dirt", "tool"))
	writeFile(t, filepath.Join(tables, "entities", "sheep.json"), table("minecraft:white_wool", "this"))
	srv, c := joinPacks(t, cfg)

	servertest.Setblock(t, c, 0, 64, 2, "minecraft:coarse_dirt")
	c.ChatCommand(`item replace entity @s hotbar.0 with minecraft:iron_shovel[minecraft:enchantments={"minecraft:efficiency":2}]`)
	c.Settle()
	c.BreakBlock(0, 64, 2)
	servertest.UntilTicks(t, srv, 40, "the drop carrying the tool's enchantments", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:coarse_dirt" {
				_, ok := registry.StackComponents(it.Stack)["minecraft:enchantments"]
				return ok
			}
		}
		return false
	})

	srv.RunLine(`summon minecraft:sheep 4.5 64 5.5 {NoAI:1b,CustomName:"Dolly"}`)
	c.Settle()
	srv.RunLine(`kill @e[type=minecraft:sheep]`)
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
