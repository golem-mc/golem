package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// openerTable gives a diamond only when THIS is a player, an emerald per bonus
// roll (one per point of luck), and a gold ingot over a stick once luck has
// cut the stick's weight to zero.
const openerTable = `{"pools":[` +
	`{"rolls":1,"conditions":[{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:player"}}],` +
	`"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]},` +
	`{"rolls":0,"bonus_rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:emerald"}]},` +
	`{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:stick","quality":-1},` +
	`{"type":"minecraft:item","name":"minecraft:gold_ingot","quality":1}]}]}`

// lootCounts totals the stacks by item name.
func lootCounts(stacks []protocol.ItemStack) map[string]int32 {
	got := map[string]int32{}
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		name, _ := registry.ItemName(s.ItemID)
		got[name] += s.Count
	}
	return got
}

// wantOpenerLoot checks the openerTable roll of a player under Luck III.
func wantOpenerLoot(t *testing.T, what string, got map[string]int32) {
	t.Helper()
	if got["minecraft:diamond"] != 1 || got["minecraft:emerald"] != 3 || got["minecraft:gold_ingot"] != 1 || len(got) != 3 {
		t.Errorf("%s holds %v, want a diamond (THIS is the player), 3 emeralds (luck 3 bonus rolls) and a gold ingot (quality)", what, got)
	}
}

// TestContainerLootRollsForTheOpeningPlayer checks that a chest's pending
// table rolls with the player opening it as THIS and with that player's luck,
// and that a shulker box broken in survival rolls for its breaker before its
// drop copies the contents. The pack layer is process-wide, so the table is
// one no other test of the package rolls.
// vanilla: RandomizableContainer.unpackLootTable, LootPool.addRandomItems,
// UniformContainerBase.EntryBase.getWeight, ShulkerBoxBlock.playerWillDestroy
func TestContainerLootRollsForTheOpeningPlayer(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "loot_table", "opener.json"), openerTable)
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", c)
	defer w.Disconnect()
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	c.ChatCommand("effect give @s minecraft:luck 600 2")
	c.Settle()
	// The effect's luck modifier goes on with the player's next tick.
	servertest.RunTicks(t, srv, 1)
	servertest.Setblock(t, c, 2, 64, 0, `minecraft:chest{LootTable:"pk:opener"}`)
	w.UseBlock(2, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 2, 64, 0)
	wantOpenerLoot(t, "the opened chest", lootCounts(slots[:27]))
	w.CloseWindow()

	servertest.Setblock(t, c, 0, 64, 2, `minecraft:shulker_box{LootTable:"pk:opener"}`)
	c.ChatCommand(`item replace entity @s hotbar.0 with minecraft:netherite_pickaxe[minecraft:enchantments={"minecraft:efficiency":5}]`)
	c.Settle()
	c.BreakBlock(0, 64, 2)
	var box []protocol.ItemStack
	servertest.UntilTicks(t, srv, 40, "the broken shulker box's drop", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:shulker_box" {
				box = it.Stack.Container
				return true
			}
		}
		return false
	})
	wantOpenerLoot(t, "the broken shulker box", lootCounts(box))
}
