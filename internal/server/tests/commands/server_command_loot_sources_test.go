package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// placeBlockNextToBarrel puts the block at 2 64 4 and waits for its update.
func placeBlockNextToBarrel(t *testing.T, c *servertest.Client, name string) {
	t.Helper()
	c.ChatCommand("setblock 2 64 4 " + name)
	state, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("the registry has no %s", name)
	}
	servertest.AwaitBlockUpdate(t, c, 2, 64, 4, state)
	servertest.DrainBlockUpdates(t, c, 8)
}

// tableText is the table name the with-table feedback carries.
func tableText(name string) protocol.Text { return protocol.Text{Text: name} }

// TestLootMineHonoursTheTool checks the mine source: stone drops cobblestone
// to a bare hand and itself to a silk touch pickaxe, and the feedback names
// the block's table.
// vanilla: LootCommand.dropBlockLoot
func TestLootMineHonoursTheTool(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	placeBlockNextToBarrel(t, c, "minecraft:stone")

	c.ChatCommand("loot insert 2 64 3 mine 2 64 4")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.single_with_table",
		servertest.ItemNumber(1), lootStackText(t, "minecraft:cobblestone", 1), tableText("minecraft:blocks/stone"))
	servertest.UntilTicks(t, srv, 200, "cobblestone in the barrel", func() bool {
		return barrelItems(srv, "minecraft:cobblestone") == 1
	})

	c.ChatCommand(`loot insert 2 64 3 mine 2 64 4 minecraft:diamond_pickaxe[enchantments={"minecraft:silk_touch":1}]`)
	servertest.UntilTicks(t, srv, 200, "stone in the barrel", func() bool {
		return barrelItems(srv, "minecraft:stone") == 1
	})
	if got := barrelItems(srv, "minecraft:cobblestone"); got != 1 {
		t.Errorf("barrel holds %d cobblestone, want the silk touch roll to add none", got)
	}
	c.Disconnect()
}

// TestLootMineWithoutALootTable checks that a block that drops through no
// table is refused by name.
// vanilla: LootCommand.dropBlockLoot
func TestLootMineWithoutALootTable(t *testing.T) {
	t.Parallel()
	_, c := dialItemBarrel(t)

	c.ChatCommand("loot give @s mine 2 90 3")
	awaitItemFailure(t, c, "commands.drop.no_loot_table.block", protocol.Text{Translate: "block.minecraft.air"})
	c.Disconnect()
}

// TestLootMineHeldTool checks the mainhand tool of the runner: a silk touch
// pickaxe in hand keeps the stone whole.
// vanilla: LootCommand.getSourceHandItem
func TestLootMineHeldTool(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	placeBlockNextToBarrel(t, c, "minecraft:stone")

	c.ChatCommand(`item replace entity @s weapon.mainhand with minecraft:diamond_pickaxe[enchantments={"minecraft:silk_touch":1}]`)
	c.ChatCommand("loot insert 2 64 3 mine 2 64 4 mainhand")
	servertest.UntilTicks(t, srv, 200, "stone in the barrel", func() bool {
		return barrelItems(srv, "minecraft:stone") == 1
	})
	c.ChatCommand("loot insert 2 64 3 mine 2 64 4 offhand")
	servertest.UntilTicks(t, srv, 200, "cobblestone from the empty off hand", func() bool {
		return barrelItems(srv, "minecraft:cobblestone") == 1
	})
	c.Disconnect()
}

// TestLootFishRollsTheTable checks the fish source: a table is rolled with
// the tool and the position, and the fishing table gives a catch.
// vanilla: LootCommand.dropFishingLoot
func TestLootFishRollsTheTable(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand("loot insert 2 64 3 fish minecraft:blocks/diamond_block 2 64 3")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.single",
		servertest.ItemNumber(1), lootStackText(t, "minecraft:diamond_block", 1))

	c.ChatCommand("loot insert 2 64 3 fish minecraft:gameplay/fishing 2 64 3 minecraft:fishing_rod")
	servertest.UntilTicks(t, srv, 200, "a catch in the barrel", func() bool {
		var n int
		for _, s := range srv.InventoryAt(2, 64, 3) {
			if !s.Empty() && s.ItemID != servertest.MustItem("minecraft:diamond_block") {
				n++
			}
		}
		return n > 0
	})
	c.Disconnect()
}

// TestLootInlineTable checks a table given as SNBT: the pool rolls twice
// and the function sets each stack, and a malformed one is refused.
// vanilla: ResourceOrIdArgument.lootTable
func TestLootInlineTable(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand(`loot insert 2 64 3 loot {pools:[{rolls:2,entries:[{type:"minecraft:item",name:"minecraft:diamond",modifier:{type:"minecraft:set_count",count:3}}]}]}`)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.multiple", servertest.ItemNumber(2))
	servertest.UntilTicks(t, srv, 200, "six diamonds in the barrel", func() bool {
		return barrelItems(srv, "minecraft:diamond") == 6
	})

	command := "loot give @s loot {pools:[{}]}"
	c.ChatCommand(command)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", "No key rolls in MapLike[{}]"),
		servertest.CommandErrorContextForTest(command, len(command)))
	c.Disconnect()
}

// TestLootInsertAsksCanPlaceItem checks that insert and replace leave out
// the slots a block entity refuses the stack: a furnace takes no output,
// and takes fuel only in its fuel slot.
// vanilla: LootCommand.distributeToContainer, LootCommand.blockReplace
func TestLootInsertAsksCanPlaceItem(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	placeBlockNextToBarrel(t, c, "minecraft:furnace")
	furnace := func() []protocol.ItemStack { return srv.InventoryAt(2, 64, 4) }

	c.ChatCommand("loot replace block 2 64 4 container.2 loot " + diamondBlockTable)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.multiple", servertest.ItemNumber(0))
	if got := furnace()[2]; !got.Empty() {
		t.Errorf("furnace output = %+v, want the replace to leave it", got)
	}

	c.ChatCommand("loot insert 2 64 4 loot " + diamondBlockTable)
	servertest.UntilTicks(t, srv, 200, "the diamond block in the furnace input", func() bool {
		return furnace()[0].ItemID == servertest.MustItem("minecraft:diamond_block")
	})
	c.ChatCommand("loot insert 2 64 4 loot minecraft:blocks/coal_block")
	servertest.UntilTicks(t, srv, 200, "the coal block in the furnace fuel", func() bool {
		return furnace()[1].ItemID == servertest.MustItem("minecraft:coal_block")
	})
	c.ChatCommand("loot insert 2 64 4 loot minecraft:blocks/stone")
	c.ChatCommand("loot insert 2 64 4 loot " + diamondBlockTable)
	servertest.UntilTicks(t, srv, 200, "the second diamond block merged", func() bool {
		return furnace()[0].Count == 2
	})
	if got := furnace()[1]; got.ItemID != servertest.MustItem("minecraft:coal_block") || got.Count != 1 {
		t.Errorf("furnace fuel = %+v, want the stone to leave the coal block alone", got)
	}
	if got := furnace()[2]; !got.Empty() {
		t.Errorf("furnace output = %+v, want the inserts to leave it", got)
	}
	c.Disconnect()
}

// TestLootInlineTableReferencesFishingSubTable checks that an inline
// loot_table entry can name a fishing sub-table and that the tables are
// offered to the loot table argument.
// vanilla: ResourceOrIdArgument.lootTable
func TestLootInlineTableReferencesFishingSubTable(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand(`loot insert 2 64 3 loot {pools:[{rolls:1,entries:[{type:"minecraft:loot_table",value:"minecraft:gameplay/fishing/fish"}]}]}`)
	servertest.UntilTicks(t, srv, 200, "a fish in the barrel", func() bool {
		return barrelItems(srv, "minecraft:cod")+barrelItems(srv, "minecraft:salmon")+
			barrelItems(srv, "minecraft:tropical_fish")+barrelItems(srv, "minecraft:pufferfish") == 1
	})
	c.Disconnect()
}
