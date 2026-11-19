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

// TestAMinecartLeavesAnIronGolemAndASlowCartTakesNobody: an iron golem is
// never picked up, and a cart under 0.1 blocks a tick picks up nobody.
// vanilla: OldMinecartBehavior.pushAndPickupEntities
func TestAMinecartLeavesAnIronGolemAndASlowCartTakesNobody(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "creative", "Watcher", 0)
	servertest.CartRails(t, srv, 2, 14)
	srv.StopTick()
	fast := srv.SpawnMinecart(2.5, 64.0625, 1.5)
	srv.SpawnMob("minecraft:iron_golem", 3.5, 64, 1.5)
	slow := srv.SpawnMinecart(10.5, 64.0625, 1.5)
	srv.SpawnMob("minecraft:pig", 11.2, 64, 1.5)
	srv.SetMinecartMotion(fast, 0.4, 0, 0)
	srv.SetMinecartMotion(slow, 0.05, 0, 0)

	srv.TickMinecarts(3)
	if got := srv.MinecartPassengerIDs(fast); len(got) != 0 {
		t.Errorf("the fast cart carries %v, want the iron golem left out", got)
	}
	if got := srv.MinecartPassengerIDs(slow); len(got) != 0 {
		t.Errorf("the slow cart carries %v, want nobody picked up", got)
	}
}

// TestAMinecartLeavingAnActivatorRailLeavesThePigItSetDown: a plain cart
// carrying a pig rolls over a powered activator rail, which sets the pig
// down beside it; for the next few ticks the cart, still touching the pig
// and rolling at 0.1 blocks a tick or more, does not take it back aboard.
// vanilla: Entity.removePassenger boardingCooldown, OldMinecartBehavior.pushAndPickupEntities
func TestAMinecartLeavingAnActivatorRailLeavesThePigItSetDown(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "creative", "Watcher", 0)
	servertest.CartRails(t, srv, 2, 14)
	if !srv.SetBlock(6, 64, 1, servertest.MustState(t, "minecraft:activator_rail",
		map[string]string{"shape": "east_west", "powered": "true"})) {
		t.Fatal("the activator rail did not stand")
	}
	srv.StopTick()
	cart := srv.SpawnMinecart(2.5, 64.0625, 1.5)
	pig := srv.SpawnMob("minecraft:pig", 2.5, 64, 1.5)
	if !srv.MountMobOnMinecart(pig, cart) {
		t.Fatal("the pig did not board the cart")
	}
	srv.SetMinecartMotion(cart, 0.4, 0, 0)

	setDown := false
	for range 20 {
		srv.TickMinecarts(1)
		if len(srv.MinecartPassengerIDs(cart)) == 0 {
			setDown = true
			break
		}
	}
	if !setDown {
		t.Fatalf("cart passengers %v after 20 ticks, want the pig set down on the activator rail", srv.MinecartPassengerIDs(cart))
	}
	lastX, _, _, _, _ := srv.MinecartState(cart)
	for tick := 1; tick <= 3; tick++ {
		srv.TickMinecarts(1)
		cx, cy, cz, _, _ := srv.MinecartState(cart)
		px, py, pz, ok := srv.MobPos(pig)
		if !ok {
			t.Fatal("the pig is gone")
		}
		if got := srv.MinecartPassengerIDs(cart); slices.Contains(got, pig) {
			t.Fatalf("tick %d after the set-down: cart passengers %v, want the pig left out", tick, got)
		}
		// The cart's box grown 0.2 sideways (half 0.49 + 0.2, 0.7 high)
		// against the pig's (half 0.45, 0.9 high).
		touches := servertest.Abs64(px-cx) < 0.69+0.45 && servertest.Abs64(pz-cz) < 0.69+0.45 &&
			py < cy+0.7 && cy < py+0.9
		if !touches || cx-lastX < 0.1 {
			t.Fatalf("tick %d after the set-down: cart at (%.3f, %.3f, %.3f) moving %.3f, pig at (%.3f, %.3f, %.3f): want the cart touching the pig and rolling at 0.1 blocks a tick or more",
				tick, cx, cy, cz, cx-lastX, px, py, pz)
		}
		lastX = cx
	}
}
