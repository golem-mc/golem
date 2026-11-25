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

func TestLootRandomSequenceReset(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.LevelSeed = "12345"
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	const table = "minecraft:entities/zombie"
	roll := func() int64 {
		c.ChatCommand("loot spawn ~ ~ ~ loot " + table)
		reply := c.ExpectPlay(protocol.IDPlaySystemChat)
		for count := range int32(3) {
			cmp := protocol.Text{Translate: "commands.drop.success.multiple", With: []protocol.Text{servertest.ItemNumber(int64(count))}}
			if count != 0 {
				cmp.Translate = "commands.drop.success.single"
				cmp.With = append(cmp.With, lootStackText(t, "minecraft:rotten_flesh", count))
			}
			var want protocol.Buffer
			want.PutText(cmp)
			want.PutBool(false)
			if bytes.Equal(reply, want.Bytes()) {
				return int64(count)
			}
		}
		t.Fatal("zombie loot feedback did not report zero to two rotten flesh")
		return 0
	}

	roll()
	c.ChatCommand("random reset *")
	reply := readFeedback(t, c.ExpectPlay(protocol.IDPlaySystemChat))
	if reply.Translate != "commands.random.reset.all.success" || len(reply.With) != 1 ||
		reply.With[0].Number == nil || *reply.With[0].Number < 1 {
		t.Errorf("reset-all feedback = %+v, want at least one sequence after one loot roll", reply)
	}

	var first, replay [8]int64
	for i := range first {
		first[i] = roll()
	}
	c.ChatCommand("random reset " + table)
	expectResetReply(t, c, table)
	for i := range replay {
		replay[i] = roll()
	}
	// Vanilla's RandomSequence(12345, "minecraft:entities/zombie").random().nextInt(3).
	want := [8]int64{0, 0, 1, 2, 2, 2, 2, 0}
	if first != want || replay != want {
		t.Errorf("zombie loot before reset = %v, after reset = %v, want %v both times", first, replay, want)
	}
	c.Disconnect()
}

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
