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
