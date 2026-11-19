package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAPlayerWalkingIntoAMinecartPushesItAlongTheRail: a survival player
// walking east into a resting cart pushes it east along the rail.
// vanilla: LivingEntity.pushEntities, AbstractMinecart.push
func TestAPlayerWalkingIntoAMinecartPushesItAlongTheRail(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "survival", "Pusher", 0)
	servertest.CartRails(t, srv, 2, 14)
	servertest.HoldTicks(t, srv)
	cart := srv.SpawnMinecart(5.5, 64.0625, 1.5)

	x := 2.5
	c.MoveToGround(x, 64, 1.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	moved := false
	for range 20 {
		// Up to 0.2 short of the cart's centre: the boxes (0.3 and 0.49
		// half wide) overlap once the player is within 0.79.
		if cx, _, _, _, _ := srv.MinecartState(cart); x < cx-0.2 {
			x = min(x+0.2, cx-0.2)
			c.MoveToGround(x, 64, 1.5, true)
			c.Settle()
		}
		servertest.RunTicks(t, srv, 1)
		if cx, _, cz, _, ok := srv.MinecartState(cart); ok && cx > 5.6 && servertest.Abs64(cz-1.5) < 1e-9 {
			moved = true
			break
		}
	}
	if cx, cy, cz, _, _ := srv.MinecartState(cart); !moved {
		t.Fatalf("cart at (%.3f, %.3f, %.3f) after 20 ticks of the player walking in, want it pushed east along the rail past x=5.6", cx, cy, cz)
	}
}
