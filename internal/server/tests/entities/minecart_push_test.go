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

// TestARollingMinecartPassesItsMotionToAResting: a cart rolling east at 0.4
// into a resting cart on the same rail sets the resting one rolling east
// and is slowed by it.
// vanilla: OldMinecartBehavior.pushAndPickupEntities, AbstractMinecart.pushOtherMinecart
func TestARollingMinecartPassesItsMotionToAResting(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "creative", "Watcher", 0)
	servertest.CartRails(t, srv, 2, 14)
	servertest.HoldTicks(t, srv)
	rolling := srv.SpawnMinecart(2.5, 64.0625, 1.5)
	resting := srv.SpawnMinecart(6.5, 64.0625, 1.5)
	srv.SetMinecartMotion(rolling, 0.4, 0, 0)

	lastX, _, _, _, _ := srv.MinecartState(rolling)
	hit := false
	for range 40 {
		servertest.RunTicks(t, srv, 1)
		lastX, _, _, _, _ = srv.MinecartState(rolling)
		if rx, _, _, _, _ := srv.MinecartState(resting); rx > 6.6 {
			hit = true
			break
		}
	}
	rx, _, _, _, _ := srv.MinecartState(resting)
	if !hit {
		t.Fatalf("resting cart at x=%.3f after 40 ticks, want it pushed east by the rolling one (at x=%.3f)", rx, lastX)
	}
	servertest.RunTicks(t, srv, 1)
	x, _, _, _, _ := srv.MinecartState(rolling)
	if step := x - lastX; step >= 0.3 {
		t.Errorf("the rolling cart still moves %.3f a tick after the hit, want it slowed well under its 0.4", step)
	}
	if nx, _, _, _, _ := srv.MinecartState(resting); nx <= rx {
		t.Errorf("the pushed cart went from x=%.3f to %.3f, want it rolling east", rx, nx)
	}
}

// TestACowOverlappingAMinecartPushesItAlongTheRail: a cow standing in a
// resting cart's west half pushes the cart east along the rail.
// vanilla: LivingEntity.pushEntities, AbstractMinecart.push
func TestACowOverlappingAMinecartPushesItAlongTheRail(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "creative", "Watcher", 0)
	servertest.CartRails(t, srv, 2, 14)
	servertest.HoldTicks(t, srv)
	cart := srv.SpawnMinecart(5.5, 64.0625, 1.5)
	srv.SpawnMob("minecraft:cow", 5.0, 64, 1.5)

	servertest.UntilTicks(t, srv, 20, "the cart pushed east along the rail", func() bool {
		cx, _, cz, _, ok := srv.MinecartState(cart)
		return ok && cx > 5.6 && servertest.Abs64(cz-1.5) < 1e-9
	})
}
