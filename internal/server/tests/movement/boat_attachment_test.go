package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.positionRider, Avatar.DEFAULT_VEHICLE_ATTACHMENT
func TestRideSeatsABoatPlayerBelowItsPassengerAttachment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind string
		seat float64
	}{
		{"oak_boat", 0.1875},
		{"oak_chest_boat", 0.1875},
		{"bamboo_raft", 0.5},
		{"bamboo_chest_raft", 0.5},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon " + tc.kind + " 0.5 100 0.5 {NoGravity:1b}")
			_, boat := c.AwaitNextSpawn()
			c.ChatCommand("ride @s mount @e[type=" + tc.kind + ",limit=1]")
			servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
			servertest.AwaitPassengersFor(c, boat.EntityID)
			c.Settle()
			wantY := 100 + tc.seat - 0.6
			if got := srv.PlayerPosition("Rider").Y; math.Abs(got-wantY) > 1e-6 {
				t.Errorf("rider Y = %g, want %g", got, wantY)
			}

			srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 1})
			servertest.UseHeld(c, -90, 10)
			pearl := c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)
			if want := wantY + 1.62 - 0.1; math.Abs(pearl.Y-want) > 1e-6 {
				t.Errorf("pearl launch Y = %g, want %g", pearl.Y, want)
			}

			c.ChatCommand("tp @e[type=" + tc.kind + ",limit=1] 6.5 101 6.5")
			c.Settle()
			if got := srv.PlayerPosition("Rider").Y; math.Abs(got-(wantY+1)) > 1e-6 {
				t.Errorf("teleported rider Y = %g, want %g", got, wantY+1)
			}
		})
	}
}
