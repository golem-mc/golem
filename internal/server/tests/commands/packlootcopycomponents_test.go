package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// A copy_components include of a component no kind reads copies the block
// entity's own leftover component onto the drop.
// vanilla: CopyComponentsFunction.run, BlockEntity.collectComponents
func TestLootCopyComponentsCopiesABlockEntitysOwnComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table", "blocks", "barrel.json"),
		`{"type":"minecraft:block","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:barrel",`+
			`"functions":[{"function":"minecraft:copy_components","source":"block_entity","include":["minecraft:lore","minecraft:custom_data"]}]}]}]}`)
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Join("Testificate")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand("gamemode survival")
	servertest.Setblock(t, c, 0, 64, 2, `minecraft:barrel{components:{"minecraft:lore":["L2"],"minecraft:custom_data":{z:2b}}}`)
	c.ChatCommand(`item replace entity @s hotbar.0 with minecraft:netherite_axe[minecraft:enchantments={"minecraft:efficiency":5}]`)
	c.Settle()
	c.BreakBlock(0, 64, 2)
	var got bool
	servertest.UntilTicks(t, srv, 40, "the broken barrel's drop", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:barrel" {
				got = len(it.Stack.LoreOf()) == 1 && it.Stack.CustomData()["z"] != nil
				return true
			}
		}
		return false
	})
	if !got {
		t.Errorf("the dropped barrel lacks the lore and custom_data of its block entity")
	}
}
