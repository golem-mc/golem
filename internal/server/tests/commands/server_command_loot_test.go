package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// diamondBlockTable always rolls one diamond block.
const diamondBlockTable = "minecraft:blocks/diamond_block"

// lootStackText is the stack a /loot run reports, hover included.
func lootStackText(t *testing.T, item string, count int32) protocol.Text {
	t.Helper()
	return servertest.ItemFeedbackText(item, servertest.ItemDisplayKey(t, item), count)
}

// barrelItems counts the items of the barrel at 2 64 3 that are the item.
func barrelItems(srv *server.Server, item string) int32 {
	id := servertest.MustItem(item)
	var n int32
	for _, s := range srv.InventoryAt(2, 64, 3) {
		if s.ItemID == id {
			n += s.Count
		}
	}
	return n
}

// TestLootInsertMergesIntoContainer checks /loot insert with the loot source:
// the stack lands in the first empty slot, the next roll tops it up, and the
// feedback names the stack.
func TestLootInsertMergesIntoContainer(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	block := servertest.MustItem("minecraft:diamond_block")

	c.ChatCommand("loot insert 2 64 3 loot " + diamondBlockTable)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.single",
		servertest.ItemNumber(1), lootStackText(t, "minecraft:diamond_block", 1))
	awaitBarrelSlot(t, srv, 0, "a diamond block", func(s protocol.ItemStack) bool { return s.ItemID == block && s.Count == 1 })

	c.ChatCommand("loot insert 2 64 3 loot " + diamondBlockTable)
	awaitBarrelSlot(t, srv, 0, "two diamond blocks", func(s protocol.ItemStack) bool { return s.Count == 2 })
	if got := srv.InventoryAt(2, 64, 3)[1]; !got.Empty() {
		t.Errorf("barrel slot 1 = %+v, want the second roll merged into slot 0", got)
	}

	c.ChatCommand("loot insert 3 64 4 loot " + diamondBlockTable)
	awaitItemFailure(t, c, itemTargetNoContainerKey, servertest.ItemNumber(3), servertest.ItemNumber(64), servertest.ItemNumber(4))
	c.Disconnect()
}

// TestLootReplaceBlockFillsSlots checks /loot replace block: the stacks fill
// the slots from the named one, a count past the stacks empties the rest and
// is counted, and a slot past the container is refused.
func TestLootReplaceBlockFillsSlots(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)
	block := servertest.MustItem("minecraft:diamond_block")

	c.ChatCommand("item replace block 2 64 3 container.5 with minecraft:stone 4")
	awaitBarrelSlot(t, srv, 5, "stone", func(s protocol.ItemStack) bool { return s.Count == 4 })
	c.ChatCommand("loot replace block 2 64 3 container.4 3 loot " + diamondBlockTable)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.multiple", servertest.ItemNumber(3))
	awaitBarrelSlot(t, srv, 4, "a diamond block", func(s protocol.ItemStack) bool { return s.ItemID == block })
	if got := srv.InventoryAt(2, 64, 3)[5]; !got.Empty() {
		t.Errorf("barrel slot 5 = %+v, want the count to empty it", got)
	}

	c.ChatCommand("loot replace block 2 64 3 container.27 loot " + diamondBlockTable)
	awaitItemFailure(t, c, itemTargetNoSlotKey, servertest.ItemNumber(27))
	c.Disconnect()
}

// TestLootKillRollsTheTargetsTable checks the kill source: an iron golem
// always drops iron ingots, and the player's table is empty, so it rolls
// nothing and counts zero stacks.
func TestLootKillRollsTheTargetsTable(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand("summon minecraft:iron_golem 2.5 64 6.5")
	c.ChatCommand("loot insert 2 64 3 kill @e[type=minecraft:iron_golem,limit=1]")
	servertest.UntilTicks(t, srv, 200, "iron ingots in the barrel", func() bool {
		return barrelItems(srv, "minecraft:iron_ingot") >= 3
	})

	c.ChatCommand("loot insert 2 64 3 kill @s")
	servertest.AwaitCommandFeedbackText(t, c, "commands.drop.success.multiple_with_table",
		servertest.NumberText(0), protocol.Text{Text: "minecraft:entities/player"})
	c.Disconnect()
}

// TestLootKillRollsAMissingTableAsEmpty checks that an entity whose type has
// no table file still names its default table and rolls nothing.
// vanilla: LootCommand.dropKillLoot, Entity.getLootTable
func TestLootKillRollsAMissingTableAsEmpty(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("summon minecraft:arrow ~ ~2 ~ {NoGravity:1b}")
	c.ChatCommand("loot give @s kill @e[type=minecraft:arrow,limit=1]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.drop.success.multiple_with_table",
		servertest.NumberText(0), protocol.Text{Text: "minecraft:entities/arrow"})
	c.Disconnect()
}

// TestLootKillRollsAnEmptyTableForAKindWithoutOne checks that a boat, whose
// table key has no data, rolls an empty table instead of being refused.
func TestLootKillRollsAnEmptyTableForAKindWithoutOne(t *testing.T) {
	t.Parallel()
	_, c := dialItemBarrel(t)

	c.ChatCommand("summon minecraft:oak_boat 2.5 64 6.5")
	c.ChatCommand("loot give @s kill @e[type=minecraft:oak_boat,limit=1]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.drop.success.multiple_with_table",
		servertest.NumberText(0), protocol.Text{Text: "minecraft:entities/oak_boat"})
	c.Disconnect()
}

// TestLootKillRollsTheDeathLootTable checks that the kill source rolls a
// mob's DeathLootTable instead of its kind's: a zombie set to the iron
// golem's table drops 3-5 iron ingots and no rotten flesh.
func TestLootKillRollsTheDeathLootTable(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand(`summon minecraft:zombie 2.5 64 6.5 {NoAI:1b,DeathLootTable:"minecraft:entities/iron_golem"}`)
	c.ChatCommand("loot spawn 6.5 65 6.5 kill @e[type=minecraft:zombie,limit=1]")
	count := func(item string) int32 {
		var n int32
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == item {
				n += it.Count
			}
		}
		return n
	}
	// The ingots are one stack, so they appear all at once.
	servertest.UntilTicks(t, srv, 200, "iron ingots spawned", func() bool {
		return count("minecraft:iron_ingot") > 0
	})
	if n := count("minecraft:iron_ingot"); n < 3 || n > 5 {
		t.Errorf("spawned %d iron ingots, want 3-5", n)
	}
	if n := count("minecraft:rotten_flesh"); n != 0 {
		t.Errorf("spawned %d rotten flesh, want none from the zombie's own table", n)
	}
	c.Disconnect()
}

// TestLootGiveAndSpawn checks the give target, which lands the stack in the
// player's hotbar, and the spawn target, which drops it as an item entity at
// the point.
func TestLootGiveAndSpawn(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")
	block := servertest.MustItem("minecraft:diamond_block")

	c.ChatCommand("loot give @s loot " + diamondBlockTable)
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Stack.ItemID != block || updates[0].Stack.Count != 1 {
		t.Errorf("loot give slot update = %+v, want one diamond block", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.drop.success.single",
		servertest.ItemNumber(1), lootStackText(t, "minecraft:diamond_block", 1))

	c.ChatCommand("loot spawn 10.5 80 -3.5 loot " + diamondBlockTable)
	for packetID, data := range c.Stream() {
		if packetID != protocol.IDPlaySpawnEntity {
			continue
		}
		drop := servertest.ParseItemSpawn(data)
		if drop.EntityID == 0 {
			continue
		}
		if drop.X != 10.5 || drop.Z != -3.5 {
			t.Errorf("spawned item at %v %v %v, want 10.5 _ -3.5", drop.X, drop.Y, drop.Z)
		}
		break
	}
	c.Disconnect()
}

// TestLootStoresItsCount checks that /execute store result takes the number
// of stacks the target took.
func TestLootStoresItsCount(t *testing.T) {
	t.Parallel()
	srv, c := dialItemBarrel(t)

	c.ChatCommand("execute store result storage test:a n int 1 run loot replace block 2 64 3 container.0 4 loot " + diamondBlockTable)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.multiple", servertest.ItemNumber(4))
	servertest.UntilTicks(t, srv, 50, "the barrel to hold a diamond block", func() bool {
		return barrelItems(srv, "minecraft:diamond_block") == 1
	})
	c.ChatCommand("data get storage test:a n")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "4")
	c.Disconnect()
}

// TestLootReplaceEntityFillsSlot checks /loot replace entity: the stack goes
// in the slot the word names, here the player's main hand.
func TestLootReplaceEntityFillsSlot(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")
	block := servertest.MustItem("minecraft:diamond_block")

	c.ChatCommand("loot replace entity @s weapon.mainhand loot " + diamondBlockTable)
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != block {
		t.Errorf("loot replace slot update = %+v, want a diamond block in the held slot", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.drop.success.single",
		servertest.ItemNumber(1), lootStackText(t, "minecraft:diamond_block", 1))
	c.Disconnect()
}

// fillInventory fills the player's 36 inventory slots with stone, except the
// first, which gets the stack.
func fillInventory(srv *server.Server, name string, first protocol.ItemStack) {
	stone := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 64}
	srv.GiveSlot(name, 9, first)
	for slot := 10; slot <= 44; slot++ {
		srv.GiveSlot(name, slot, stone)
	}
}

// TestLootGiveCountsAPartialFit checks that /loot give counts a stack when
// only part of it fits, and that the rest is lost.
// vanilla: Inventory.add
func TestLootGiveCountsAPartialFit(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	seeds := servertest.MustItem("minecraft:pumpkin_seeds")
	fillInventory(srv, "Testificate", protocol.ItemStack{ItemID: seeds, Count: 62})

	c.ChatCommand("loot give @s loot minecraft:carve/pumpkin")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.single",
		servertest.ItemNumber(4), lootStackText(t, "minecraft:pumpkin_seeds", 4))
	if got := srv.InventorySlot("Testificate", 9); got.Count != 64 {
		t.Errorf("seeds slot = %+v, want topped up to 64", got)
	}
	c.Disconnect()
}

// TestLootGiveInlineTable checks that an inline table is read in 26.3's
// shape: a modifier holder and a type-keyed condition holder.
// vanilla: ResourceOrIdArgument.lootTable, LootTable.DIRECT_CODEC
func TestLootGiveInlineTable(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand(`loot give @s loot {pools:[{rolls:1,entries:[{type:"minecraft:item",name:"minecraft:emerald",` +
		`condition:{type:"minecraft:random_chance",chance:1},modifier:{type:"minecraft:set_count",count:3}}]}]}`)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.single",
		servertest.ItemNumber(3), lootStackText(t, "minecraft:emerald", 3))
	c.Disconnect()
}

// vanilla: LootTable.DIRECT_CODEC, LootItemFunctions.TYPED_CODEC
func TestLootInlineTableRequiresCurrentFunctionType(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	const table = `loot give @s loot {pools:[{rolls:1,entries:[{type:"item",name:"stone"}]}],modifier:`
	c.ChatCommand(table + `{type:"set_count",count:2}}`)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot))
	r.ReadVarInt() // container
	r.ReadVarInt() // state
	slot := r.ReadI16()
	if got := servertest.MustReadSlot(t, r); slot != 36 || got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 2 || r.Err() != nil {
		t.Fatalf("slot update = %d, %+v, %v, want two stone in the main hand", slot, got, r.Err())
	}
	wantFeedback := servertest.SystemChatWire(protocol.Text{Translate: "commands.drop.success.single", With: []protocol.Text{
		servertest.ItemNumber(2), lootStackText(t, "minecraft:stone", 2),
	}})
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantFeedback) {
		t.Errorf("loot feedback = % X, want % X", got, wantFeedback)
	}

	const directError = `Failed to parse either. First: No key type in MapLike[{count:2,function:"set_count"}]; ` +
		`Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: ` +
		`Not a list: {count:2,function:"set_count"}; Second: No key type in MapLike[{count:2,function:"set_count"}]`
	const contentsError = `Failed to parse either. First: ` + directError +
		`; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {component:"container",modifier:{count:2,function:"set_count"},type:"modify_contents"}; Second: ` + directError
	for _, tc := range []struct{ command, message string }{
		{table + `{function:"set_count",count:2}}`, directError},
		{`loot give @s loot {modifier:{type:"modify_contents",component:"container",modifier:{function:"set_count",count:2}}}`, contentsError},
	} {
		c.ChatCommand(tc.command)
		wantError := servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.message)
		if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantError) {
			t.Errorf("%s: command error = % X, want % X", tc.command, got, wantError)
		}
		wantContext := servertest.CommandErrorContextForTest(tc.command, len(tc.command))
		if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantContext) {
			t.Errorf("%s: command context = % X, want % X", tc.command, got, wantContext)
		}
		c.ExpectNoneWithinTicks(srv, 0, func(id int32, _ []byte) bool { return id == protocol.IDPlayContainerSlot })
		if got := srv.InventorySlot("Testificate", 36); got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 2 {
			t.Errorf("%s: main hand = %+v, want two stone after the refused command", tc.command, got)
		}
	}
	c.Disconnect()
}

// vanilla: LootTable.DIRECT_CODEC
func TestLootInlineTableIgnoresLegacyFunctions(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`loot give @s loot {pools:[{rolls:1,entries:[{type:"item",name:"stone"}]}],functions:[{function:"set_count",count:2}]}`)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot))
	r.ReadVarInt() // container
	r.ReadVarInt() // state
	slot := r.ReadI16()
	if got := servertest.MustReadSlot(t, r); slot != 36 || got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 1 || r.Err() != nil {
		t.Fatalf("slot update = %d, %+v, %v, want one stone in the main hand", slot, got, r.Err())
	}
	wantFeedback := servertest.SystemChatWire(protocol.Text{Translate: "commands.drop.success.single", With: []protocol.Text{
		servertest.ItemNumber(1), lootStackText(t, "minecraft:stone", 1),
	}})
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantFeedback) {
		t.Errorf("loot feedback = % X, want % X", got, wantFeedback)
	}
	c.Disconnect()
}

// TestLootGiveInlineTableRefusesUnknownNames checks that an inline table
// naming an item or a nested table the server does not have is refused when
// the command is parsed, in vanilla's words.
// vanilla: LootItem.CODEC, NestedLootTable.CODEC
func TestLootGiveInlineTableRefusesUnknownNames(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	for _, tc := range []struct{ entry, msg string }{
		{`{type:"minecraft:item",name:"minecraft:nope"}`,
			"Unknown registry key in ResourceKey[minecraft:root / minecraft:item]: minecraft:nope"},
		{`{type:"minecraft:alternatives",children:[{type:"minecraft:loot_table",value:"minecraft:nope"}]}`,
			`Failed to parse either. First: Not a tag id; Second: Failed to parse either. First: Not a list: "minecraft:nope"; ` +
				"Second: Failed to get element minecraft:nope from registry minecraft:loot_table"},
	} {
		command := "loot give @s loot {pools:[{rolls:1,entries:[" + tc.entry + "]}]}"
		c.ChatCommand(command)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.msg),
			servertest.CommandErrorContextForTest(command, len(command)))
	}
	c.Disconnect()
}

// TestLootGiveCountsForCreative checks that a creative player's full
// inventory still counts a given stack.
// vanilla: Inventory.add
func TestLootGiveCountsForCreative(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")
	fillInventory(srv, "Testificate", protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 64})

	c.ChatCommand("loot give @s loot " + diamondBlockTable)
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.drop.success.single",
		servertest.ItemNumber(1), lootStackText(t, "minecraft:diamond_block", 1))
	c.Disconnect()
}

// TestLootFeedbackStaysWithRunner checks that /loot success is not echoed to
// the other operators.
// vanilla: LootCommand.callback
func TestLootFeedbackStaysWithRunner(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)
	bob := servertest.JoinFeedbackWorld(t, connect, "Bob", []*servertest.Client{alice})

	alice.ChatCommand("loot give @s loot " + diamondBlockTable)
	servertest.ExpectFeedbackSoon(t, alice, nil, "commands.drop.success.single",
		servertest.ItemNumber(1), lootStackText(t, "minecraft:diamond_block", 1))
	alice.Settle()
	servertest.AwaitNoSystemChat(t, bob)
	alice.Disconnect()
	bob.Disconnect()
}

// TestLootChestRollsWithTheRunnerAsThisEntity checks that a burning runner
// rolling a mob's table gets the smelted drop.
// vanilla: LootCommand.dropChestLoot
func TestLootChestRollsWithTheRunnerAsThisEntity(t *testing.T) {
	t.Parallel()
	// A survival runner: a creative one's burn is held at a single tick.
	// vanilla: Player.setRemainingFireTicks
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
	// Held, so the burn is still the one set when the roll runs.
	servertest.HoldTicks(t, srv)
	cooked := servertest.MustItem("minecraft:cooked_chicken")
	raw := servertest.MustItem("minecraft:chicken")
	srv.SetPlayerFire("Testificate", 200)

	c.ChatCommand("loot give @s loot minecraft:entities/chicken")
	c.Settle()
	servertest.UntilTicks(t, srv, 200, "a cooked chicken", func() bool {
		for slot := 9; slot <= 44; slot++ {
			if srv.InventorySlot("Testificate", slot).ItemID == cooked {
				return true
			}
			if srv.InventorySlot("Testificate", slot).ItemID == raw {
				t.Error("rolled a raw chicken for a burning runner")
				return true
			}
		}
		return false
	})
	c.Disconnect()
}

// TestAbandonedCampBarrelTableRolls checks that the barrel table abandoned
// camps name is known: /loot spawn drops its 4-8 stacks and its one tool or
// bundle, and a barrel set to it opens with stacks in it.
// vanilla: RandomizableContainer.unpackLootTable
func TestAbandonedCampBarrelTableRolls(t *testing.T) {
	t.Parallel()
	const table = "minecraft:barrels/abandoned_camp_barrel"
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()

	servertest.Setblock(t, w.Client, 2, 64, 3, `minecraft:barrel{LootTable:"`+table+`"}`)
	w.UseBlock(2, 64, 3)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 2, 64, 3)
	empty := true
	for _, s := range srv.InventoryAt(2, 64, 3) {
		if !s.Empty() {
			empty = false
		}
	}
	if empty {
		t.Error("the barrel opened empty")
	}

	// Held, so the dropped stacks cannot merge before they are counted.
	servertest.HoldTicks(t, srv)
	srv.RunLine("loot spawn 10.5 80 -3.5 loot " + table)
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) < 5 {
		t.Errorf("spawned %d item entities, want at least 5", len(items))
	}
	tool := false
	for _, it := range items {
		switch it.Item {
		case "minecraft:bundle", "minecraft:wooden_axe", "minecraft:fishing_rod":
			tool = true
		}
	}
	if !tool {
		t.Errorf("spawned %+v, want a bundle, wooden axe or fishing rod among them", items)
	}
}
