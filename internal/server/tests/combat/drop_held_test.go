package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestDroppingTheHeldItemSwingsForOthersAndResetsTheCooldown drops one of two
// swords with Q: a watcher sees the dropper's main arm swing, the dropper
// (whose client swung on its own) is sent none, and the next blow starts
// recharging from nothing, as it does after Q on an empty hand.
// vanilla: ServerPlayer.drop, LivingEntity.drop
func TestDroppingTheHeldItemSwingsForOthersAndResetsTheCooldown(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.CombatTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	entityID := c.JoinAs("Dropper")
	watcher.ExpectPlayerAdded("Dropper")
	c.Settle()

	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Dropper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 2})
	servertest.RunTicks(t, srv, 30)
	if got := srv.AttackTicks("Dropper"); got <= 20 {
		t.Fatalf("attack ticks before the drop = %d, want above 20", got)
	}

	c.DropKey(false)
	c.Settle()
	if got := srv.AttackTicks("Dropper"); got != 0 {
		t.Errorf("attack ticks right after the drop = %d, want 0", got)
	}
	r := protocol.NewReader(watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
	if got := r.ReadVarInt(); got != entityID {
		t.Errorf("swinging entity = %d, want the dropper's %d", got, entityID)
	}
	if got := r.ReadVarInt(); got != protocol.HandMain {
		t.Errorf("swinging hand = %d, want the main hand", got)
	}
	c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlaySwingAnimation))

	// Q on the hand the second sword leaves empty still resets the cooldown.
	c.DropKey(false)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	if got := srv.AttackTicks("Dropper"); got == 0 {
		t.Fatal("attack ticks did not recharge after the second drop")
	}
	c.DropKey(false)
	c.Settle()
	if got := srv.AttackTicks("Dropper"); got != 0 {
		t.Errorf("attack ticks after Q on an empty hand = %d, want 0", got)
	}
}

// TestDroppingTheLastOfAStackBeingEatenEndsTheMeal starts eating a single
// apple and drops it with the stack key: the meal stops at once, before any
// tick, and the watcher sees the dropper's hand come down.
// vanilla: ServerPlayer.drop
func TestDroppingTheLastOfAStackBeingEatenEndsTheMeal(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.SurvivalTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	entityID := c.JoinAs("Eater")
	watcher.ExpectPlayerAdded("Eater")
	c.Settle()

	srv.SetFood("Eater", 10)
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:apple"), Count: 1})
	c.UseItemRightClicks(protocol.HandMain)
	watcher.ExpectHandState(entityID, protocol.EntityFlagHandActive)
	if !srv.Eating("Eater") {
		t.Fatal("the meal did not begin")
	}

	// Held, so that no tick can find the hand empty before the drop does.
	servertest.HoldTicks(t, srv)
	c.DropKey(true)
	c.Settle()
	if srv.Eating("Eater") {
		t.Error("the meal goes on after its apple was dropped")
	}
	watcher.ExpectHandState(entityID, 0)
}
