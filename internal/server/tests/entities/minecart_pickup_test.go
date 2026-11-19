package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestARollingMinecartTakesAPigAboard: a plain cart rolling east along the
// line into a pig standing on it takes the pig aboard and carries it on.
// vanilla: OldMinecartBehavior.pushAndPickupEntities
func TestARollingMinecartTakesAPigAboard(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "creative", "Watcher", 0)
	servertest.CartRails(t, srv, 2, 14)
	srv.StopTick()
	// Reach the inflated pickup box before a move reaches the pig's solid
	// box; otherwise Entity.collide stops the cart before it can board.
	cart := srv.SpawnMinecart(2.7, 64.0625, 1.5)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 1.5)
	srv.SetMinecartMotion(cart, 0.4, 0, 0)

	for range 20 {
		srv.TickMinecarts(1)
		if slices.Contains(srv.MinecartPassengerIDs(cart), pig) {
			break
		}
	}
	if !slices.Contains(srv.MinecartPassengerIDs(cart), pig) {
		t.Fatalf("cart passengers %v, want the pig %d taken aboard", srv.MinecartPassengerIDs(cart), pig)
	}
	srv.TickMinecarts(5)
	cx, _, _, _, _ := srv.MinecartState(cart)
	px, _, _, ok := srv.MobPos(pig)
	if !ok || servertest.Abs64(px-cx) > 0.001 || cx <= 6.5 {
		t.Errorf("pig at x=%v (found %v), cart at x=%v: want the pig carried in the cart's seat past where it stood", px, ok, cx)
	}
}
