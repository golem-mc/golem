package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// A hotbar key pressed while the main hand is in use ends that use with
// nothing applied; a use held in the off hand is left running, the stopped
// use being only the main hand's. These tests drive the Held Item packet a
// client sends on a hotbar key and read the effect back over the wire.

// TestAHotbarSwapDropsTheMeal: a hungry player begins eating bread and
// presses the key for another hotbar slot holding the same bread. The meal
// ends with nothing applied - both loaves untouched - where a meal left
// running would have eaten the new slot's loaf.
func TestAHotbarSwapDropsTheMeal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Eater")
	srv.SetFood("Eater", 10)

	bread := servertest.MustItem("minecraft:bread")
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: bread, Count: 1})
	srv.GiveSlot("Eater", player.HotbarSlot+1, protocol.ItemStack{ItemID: bread, Count: 1})

	servertest.HoldTicks(t, srv)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins with the Use Item packet")
	}

	// The swap is acted on by the time the settle's ping is answered, well
	// inside the thirty-two ticks the meal would take.
	c.SwapHotbarSlot(1)
	c.Settle()
	if srv.Eating("Eater") {
		t.Error("the meal is still in progress after the swap to another slot")
	}

	// Past the full meal: neither loaf was touched.
	servertest.RunTicks(t, srv, 40)
	c.DrainSurvivalTraffic()
	for _, slot := range []int{player.HotbarSlot, player.HotbarSlot + 1} {
		if got := srv.InventorySlot("Eater", slot); got.ItemID != bread || got.Count != 1 {
			t.Errorf("the bread in slot %d after the swap = %+v, want it still whole", slot, got)
		}
	}
}

// TestAHotbarSwapDropsTheBowDraw: a bow drawn in survival is dropped when
// the player presses the key for a second bow, so the release-use that
// follows fires nothing and spends no arrow.
func TestAHotbarSwapDropsTheBowDraw(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer")

	bow := servertest.MustItem("minecraft:bow")
	srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: bow, Count: 1})
	srv.GiveSlot("Archer", player.HotbarSlot+1, protocol.ItemStack{ItemID: bow, Count: 1})
	srv.GiveSlot("Archer", player.HotbarSlot+2, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:arrow"), Count: 5})

	servertest.DrawBow(c, -90, 0)
	c.Settle()
	if srv.BowTicks("Archer") <= 0 {
		t.Fatal("the draw begins with the Use Item packet")
	}

	c.SwapHotbarSlot(1)
	c.Settle()
	if got := srv.BowTicks("Archer"); got != 0 {
		t.Errorf("the draw runs %d ticks after the swap, want it dropped", got)
	}

	servertest.ReleaseBow(c)
	c.ExpectNoArrowish()
	c.DrainSurvivalTraffic()
	if left := srv.InventorySlot("Archer", player.HotbarSlot+2); left.Count != 5 {
		t.Errorf("the quiver holds %d arrows after the swap and release, want all five", left.Count)
	}
}

// TestAHotbarSwapLowersTheShield: a shield raised in the main hand comes
// down when the player presses the key for a second shield, the hand state
// that has everyone draw it raised clearing to the default.
func TestAHotbarSwapLowersTheShield(t *testing.T) {
	t.Parallel()
	srv, c, blockerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Blocker")

	shield := servertest.MustItem("minecraft:shield")
	srv.GiveSlot("Blocker", player.HotbarSlot, protocol.ItemStack{ItemID: shield, Count: 1})
	srv.GiveSlot("Blocker", player.HotbarSlot+1, protocol.ItemStack{ItemID: shield, Count: 1})

	c.UseItemRightClicks(protocol.HandMain)
	c.ExpectHandState(blockerID, protocol.EntityFlagHandActive)

	c.SwapHotbarSlot(1)
	c.ExpectHandState(blockerID, 0)
}

// TestASwapHandsLowersTheShield: with a shield in both hands the swap leaves
// a shield in the main hand, so only the swap's own stopUsingItem lowers it.
// vanilla: ServerGamePacketListenerImpl.handlePlayerAction
func TestASwapHandsLowersTheShield(t *testing.T) {
	t.Parallel()
	srv, c, blockerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Blocker")

	shield := servertest.MustItem("minecraft:shield")
	srv.GiveSlot("Blocker", player.HotbarSlot, protocol.ItemStack{ItemID: shield, Count: 1})
	srv.GiveSlot("Blocker", player.OffHandSlot, protocol.ItemStack{ItemID: shield, Count: 1})

	c.UseItemRightClicks(protocol.HandMain)
	c.ExpectHandState(blockerID, protocol.EntityFlagHandActive)

	c.SendPlayerAction(protocol.PlayerActionSwapItemInHand)
	c.ExpectHandState(blockerID, 0)
}

// TestAHotbarSwapKeepsAnOffHandMeal: the vanilla condition stops a use only
// when it is held in the main hand, so a meal eaten from the off hand runs
// on across a hotbar key and finishes on its own.
func TestAHotbarSwapKeepsAnOffHandMeal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Eater")
	servertest.HoldTicks(t, srv)
	srv.SetFood("Eater", 10)

	bread := servertest.MustItem("minecraft:bread")
	srv.GiveSlot("Eater", player.OffHandSlot, protocol.ItemStack{ItemID: bread, Count: 1})

	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins with the Use Item packet")
	}

	c.SwapHotbarSlot(1)
	c.Settle()
	if !srv.Eating("Eater") {
		t.Error("the swap called off a meal held in the off hand")
	}

	// vanilla: Consumables.defaultFood (1.6 seconds, 32 ticks).
	servertest.UntilTicks(t, srv, 32+5, "the off-hand meal completes on its own",
		func() bool { return !srv.Eating("Eater") })
	c.DrainSurvivalTraffic()
	if got := srv.InventorySlot("Eater", player.OffHandSlot); !got.Empty() {
		t.Errorf("the off-hand bread after the meal = %+v, want it eaten", got)
	}
}

// TestADroppedMealStopsOnTheNextTick: a meal whose bread leaves the hand
// (the Ctrl+Q drop empties the slot with no Player Action release) stops at
// once, clears the hand state an observer saw and feeds nothing.
// vanilla: LivingEntity.updatingUsingItem
func TestADroppedMealStopsOnTheNextTick(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	watcher := connect().Join("Watcher")
	eater := connect()
	eaterID := eater.JoinAs("Eater")
	watcher.ExpectPlayerAdded("Eater")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Eater")
	servertest.HoldTicks(t, srv)
	srv.SetFood("Eater", 10)

	bread := servertest.MustItem("minecraft:bread")
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: bread, Count: 1})
	eater.UseItemRightClicks(protocol.HandMain)
	eater.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins with the Use Item packet")
	}
	watcher.ExpectHandState(eaterID, protocol.EntityFlagHandActive)

	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{})
	// Well inside the 1.6 s a meal takes: the stop is the tick check, not
	// the meal running out.
	servertest.UntilTicks(t, srv, 16, "the meal stops once the bread is gone from the hand",
		func() bool { return !srv.Eating("Eater") })
	watcher.ExpectHandState(eaterID, 0)

	servertest.RunTicks(t, srv, 40)
	eater.DrainSurvivalTraffic()
	if got := srv.PlayerFood("Eater"); got != 10 {
		t.Errorf("food after the dropped meal = %d, want 10", got)
	}
}

// TestABowTakenOutOfTheHandStopsTheDraw: the drawn bow leaving the hand
// ends the draw on the next tick, so the release fires nothing.
// vanilla: LivingEntity.updatingUsingItem
func TestABowTakenOutOfTheHandStopsTheDraw(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer")
	servertest.HoldTicks(t, srv)

	srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	srv.GiveSlot("Archer", player.HotbarSlot+2, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:arrow"), Count: 5})
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	if srv.BowTicks("Archer") <= 0 {
		t.Fatal("the draw begins with the Use Item packet")
	}

	srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{})
	// vanilla: LivingEntity.updatingUsingItem checks the hand each tick.
	servertest.UntilTicks(t, srv, 1+5, "the draw stops once the bow is gone from the hand",
		func() bool { return srv.BowTicks("Archer") == 0 })

	srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	servertest.ReleaseBow(c)
	c.ExpectNoArrowish()
	c.DrainSurvivalTraffic()
	if left := srv.InventorySlot("Archer", player.HotbarSlot+2); left.Count != 5 {
		t.Errorf("the quiver holds %d arrows, want all five", left.Count)
	}
}
