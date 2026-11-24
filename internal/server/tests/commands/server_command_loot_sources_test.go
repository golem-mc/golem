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

// vanilla: SequentialEntry.compose, AlternativesEntry.compose, LootPool.addRandomItem
func TestLootSequenceKeepsCandidatesBeforeFailure(t *testing.T) {
	t.Parallel()
	const emerald = `{type:"minecraft:item",name:"minecraft:emerald"}`
	const sequence = `{type:"minecraft:sequence",children:[` +
		`{type:"minecraft:item",name:"minecraft:stick"},` +
		`{type:"minecraft:item",name:"minecraft:diamond",condition:{type:"minecraft:random_chance",chance:0}},` +
		emerald + `]}`
	for _, tc := range []struct {
		name  string
		entry string
	}{
		{"sequence", sequence},
		// The fallback succeeds and has no weight in the pool's choice.
		{"successful fallback", `{type:"minecraft:alternatives",children:[` + sequence + `,{type:"minecraft:empty",weight:0}]}`},
		{"failed alternative stops outer sequence", `{type:"minecraft:sequence",children:[` +
			`{type:"minecraft:alternatives",children:[` + sequence + `]},` + emerald + `]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := dialItemBarrel(t)
			c.ChatCommand(`loot insert 2 64 3 loot {pools:[{rolls:1,entries:[` + tc.entry + `]}]}`)
			feedback := c.ExpectPlay(protocol.IDPlaySystemChat)
			if got := barrelItems(srv, "minecraft:stick"); got != 1 {
				t.Fatalf("loot awarded %d sticks, want one stick", got)
			}
			servertest.ExpectFeedbackSoon(t, c, [][]byte{feedback}, "commands.drop.success.single",
				servertest.ItemNumber(1), lootStackText(t, "minecraft:stick", 1))
			c.Disconnect()
		})
	}
}

// vanilla: EntryGroup.compose, AlternativesEntry.compose
func TestLootSingleChildGroupFailureFallsThrough(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	c.ChatCommand(`loot insert 2 64 3 loot {pools:[{rolls:1,entries:[{type:"minecraft:alternatives",children:[` +
		`{type:"minecraft:group",children:[{type:"minecraft:item",name:"minecraft:diamond",condition:{type:"minecraft:random_chance",chance:0}}]},` +
		`{type:"minecraft:item",name:"minecraft:stick"}]}]}]}`)
	c.ExpectPlay(protocol.IDPlaySystemChat)
	if got := barrelItems(srv, "minecraft:stick"); got != 1 {
		t.Fatalf("loot awarded %d sticks, want one stick", got)
	}
	c.Disconnect()
}
