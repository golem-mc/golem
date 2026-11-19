package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAStrafingSkeletonStaysOnABridge: a skeleton that strafes sideways or
// backwards walks straight on when the step has no floor, so it never leaves a
// one-block bridge.
// vanilla: MoveControl.tick, MoveControl.isWalkable
func TestAStrafingSkeletonStaysOnABridge(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Prey", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.Settle()
	srv.SetDayTime(18000)
	stone := registry.MustState("minecraft:stone", nil)
	air := registry.MustState("minecraft:air", nil)
	for x := int32(-3); x <= 15; x++ {
		for z := int32(-3); z <= 3; z++ {
			for y := int32(58); y <= 67; y++ {
				srv.SetBlock(x, y, z, air)
			}
			if x <= 1 {
				srv.SetBlock(x, 63, z, stone)
			}
		}
	}
	for x := int32(2); x <= 13; x++ {
		srv.SetBlock(x, 63, 0, stone)
	}
	id := srv.SpawnMob("minecraft:skeleton", 9.5, 64, 0.5)
	srv.SetMobHand(id, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	for range 100 {
		servertest.RunTicks(t, srv, 1)
		x, y, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatal("skeleton is gone")
		}
		// The body can overhang the edge while its collision box has support.
		if y < 64 {
			t.Fatalf("skeleton left the bridge: %.2f %.2f %.2f", x, y, z)
		}
	}
}

// TestACrossbowPiglinBackingUpStopsAtTheBridgeEnd: a piglin too close to its
// target backs away, but walks straight on when the step back has no floor, so
// it does not step off the end of the bridge.
// vanilla: MoveControl.tick, MoveControl.isWalkable
func TestACrossbowPiglinBackingUpStopsAtTheBridgeEnd(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	stone := registry.MustState("minecraft:stone", nil)
	air := registry.MustState("minecraft:air", nil)
	for x := int32(-3); x <= 12; x++ {
		for z := int32(-3); z <= 3; z++ {
			for y := int32(58); y <= 67; y++ {
				srv.SetBlock(x, y, z, air)
			}
			if x <= 1 {
				srv.SetBlock(x, 63, z, stone)
			}
		}
	}
	for x := int32(2); x <= 6; x++ {
		srv.SetBlock(x, 63, 0, stone)
	}
	id := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	srv.SetMobBaby(id, false)
	c.ChatCommand("item replace entity @e[type=minecraft:piglin,limit=1] weapon.mainhand with minecraft:crossbow")
	c.Settle()
	for range 80 {
		servertest.RunTicks(t, srv, 1)
		x, y, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatal("piglin is gone")
		}
		if y < 64 || math.Floor(z) != 0 {
			t.Fatalf("piglin left the bridge: %.2f %.2f %.2f", x, y, z)
		}
	}
}
