package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func itemStat(srv *server.Server, statType, item string) int32 {
	return srv.StatValue("Actor", statType, item)
}

// TestAPlacedBlockIsCountedAsUsed: placing a block is a use of its item.
// vanilla: ItemStack.useOn
func TestAPlacedBlockIsCountedAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:cobblestone")

	actor.UseBlock(3, 63, 0)
	actor.Settle()

	if got := itemStat(srv, registry.StatUsed, "minecraft:cobblestone"); got != 1 {
		t.Errorf("used cobblestone = %d, want 1", got)
	}
}

// TestAThrownSnowballIsCountedAsUsed: a throw from the air is a use.
// vanilla: SnowballItem.use
func TestAThrownSnowballIsCountedAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:snowball")

	actor.UseItemRightClicks(protocol.HandMain)
	actor.Settle()

	if got := itemStat(srv, registry.StatUsed, "minecraft:snowball"); got != 1 {
		t.Errorf("used snowball = %d, want 1", got)
	}
}

// TestAToolThatBreaksIsCountedAsBroken: the hit that wears the last point
// off a pickaxe counts one broken.
// vanilla: ServerPlayer.onEquippedItemBroken
func TestAToolThatBreaksIsCountedAsBroken(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stone")
	pick := servertest.EnchantedStack(t, "minecraft:netherite_pickaxe", "minecraft:efficiency", 5)
	pick.Damage = registry.MaxDamage(pick.ItemID) - 1
	srv.GiveSlot("Actor", player.HotbarSlot, pick)
	srv.SetBlock(3, 64, 0, registry.MustState("minecraft:stone", nil))
	actor.Settle()

	actor.BreakBlock(3, 64, 0)
	// The dig finishes some ticks after the start; step until the pickaxe is gone.
	for range 200 {
		if srv.InventorySlot("Actor", player.HotbarSlot).Empty() {
			break
		}
		srv.RunTick()
	}
	actor.Settle()

	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Empty() {
		t.Fatalf("the pickaxe after the break = %+v, want it gone", got)
	}
	if got := itemStat(srv, registry.StatBroken, "minecraft:netherite_pickaxe"); got != 1 {
		t.Errorf("broken netherite pickaxe = %d, want 1", got)
	}
}

// TestAPickedUpStackIsCountedByItsCount: taking a dropped stack counts every
// item of it.
// vanilla: ItemEntity.playerTouch
func TestAPickedUpStackIsCountedByItsCount(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stone")
	at := srv.PlayerPosition("Actor")

	srv.SpawnItemEntityDelay(at.X, at.Y, at.Z, "minecraft:dirt", 5, 0)
	for range 40 {
		srv.RunTick()
	}
	actor.Settle()

	// Actor and Watcher stand together; whichever reached the stack first took it.
	got := itemStat(srv, registry.StatPickedUp, "minecraft:dirt") +
		srv.StatValue("Watcher", registry.StatPickedUp, "minecraft:dirt")
	if got != 5 {
		t.Errorf("picked up dirt = %d, want 5", got)
	}
}

// TestADroppedStackIsCountedByItsCount: Ctrl+Q counts the whole stack, Q one.
// vanilla: ServerPlayer.drop
func TestADroppedStackIsCountedByItsCount(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 10})
	actor.Settle()

	actor.DropKey(false)
	actor.Settle()
	if got := itemStat(srv, registry.StatDropped, "minecraft:stone"); got != 1 {
		t.Errorf("dropped stone after Q = %d, want 1", got)
	}
	actor.DropKey(true)
	actor.Settle()
	if got := itemStat(srv, registry.StatDropped, "minecraft:stone"); got != 10 {
		t.Errorf("dropped stone after Ctrl+Q = %d, want 10", got)
	}
}

// TestAClickOutsideTheInventoryIsCountedAsDropped: a container click outside
// throws the carried stack, which counts as dropped.
// vanilla: AbstractContainerMenu.doClick
func TestAClickOutsideTheInventoryIsCountedAsDropped(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 7})
	actor.Settle()
	w := &servertest.WindowedClient{Client: actor}

	w.Click(36, 0, protocol.ClickModePickup)
	w.Click(-999, 0, protocol.ClickModePickup)
	actor.Settle()

	if got := itemStat(srv, registry.StatDropped, "minecraft:stone"); got != 7 {
		t.Errorf("dropped stone after the click outside = %d, want 7", got)
	}
}

// TestAShiftClickCraftRemainderThatDoesNotFitIsNotCountedAsDropped: the part
// of a shift-clicked craft that finds no room is spilled with thrownFromHand
// false, so it does not count as dropped.
// vanilla: CraftingMenu.quickMoveStack, ServerPlayer.drop
func TestAShiftClickCraftRemainderThatDoesNotFitIsNotCountedAsDropped(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:oak_log")
	actor.Settle()
	w := &servertest.WindowedClient{Client: actor}

	w.Click(36, 0, protocol.ClickModePickup)
	w.Click(1, 0, protocol.ClickModePickup)
	actor.Settle()

	// Every slot holds dirt but one, which has room for two of the four planks.
	for slot := 9; slot <= 44; slot++ {
		srv.GiveSlot("Actor", slot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:dirt"), Count: 64})
	}
	srv.GiveSlot("Actor", 40, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:oak_planks"), Count: 62})
	actor.Settle()

	w.Click(0, 0, protocol.ClickModeQuickMove)
	actor.Settle()

	if got := itemStat(srv, registry.StatCrafted, "minecraft:oak_planks"); got != 2 {
		t.Fatalf("crafted planks = %d, want 2 (the part that fit)", got)
	}
	if got := itemStat(srv, registry.StatDropped, "minecraft:oak_planks"); got != 0 {
		t.Errorf("dropped planks = %d, want 0: the spilled remainder is not thrown from the hand", got)
	}
}

// TestCountedItemStatsReachAwardStatistics: the statistics screen's request is
// answered with the used count of a placed block.
func TestCountedItemStatsReachAwardStatistics(t *testing.T) {
	t.Parallel()
	_, actor, _ := servertest.ToolScene(t, "minecraft:cobblestone")
	actor.UseBlock(3, 63, 0)
	actor.Settle()

	typeID, keyID, ok := registry.StatID(registry.StatUsed, "minecraft:cobblestone")
	if !ok {
		t.Fatal("no used statistic for cobblestone")
	}
	actor.SendClientCommand(protocol.ClientCommandRequestStats)
	for id, raw := range actor.Packets(servertest.ExpectTimeout) {
		if id != protocol.IDPlayAwardStats {
			continue
		}
		r := protocol.NewReader(raw)
		for range r.ReadVarInt() {
			tp, key, n := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
			if tp == typeID && key == keyID {
				if n != 1 {
					t.Errorf("Award Statistics' used cobblestone = %d, want 1", n)
				}
				return
			}
		}
		t.Fatal("Award Statistics has no used cobblestone")
	}
	t.Fatal("no Award Statistics arrived")
}
