package movement

import (
	"math"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A boat, a minecart off the rails and a primed TNT dropped on a slime block
// leave it upward with each body's own drag; on stone they stay down.
// vanilla: Entity.restituteMovementAfterCollisions, AbstractBoat.getAirDrag,
// AbstractMinecart.getAirDrag
func TestBoatsCartsAndTNTBounceOnSlime(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	servertest.HoldTicks(t, srv)
	slime := servertest.BlockState(t, "minecraft:slime_block")
	stone := servertest.BlockState(t, "minecraft:stone")
	for _, x := range []int32{4, 8, 12} {
		srv.SetBlock(x, 63, 20, slime)
	}
	for _, x := range []int32{16, 20} {
		srv.SetBlock(x, 63, 20, stone)
	}

	boat := srv.SpawnBoat(4.5, 68.5, 20.5, "minecraft:oak_boat")
	cart := srv.SpawnMinecart(8.5, 68.5, 20.5)
	srv.SpawnTNT(12.5, 68.5, 20.5, 200)
	srv.SpawnTNT(16.5, 68.5, 20.5, 200)
	stoneBoat := srv.SpawnBoat(20.5, 68.5, 20.5, "minecraft:oak_boat")

	var boatVY, cartVY float64
	var boatTold, cartTold bool
	// The tracker runs before the cart's physics, so a landing is told on
	// the tick after it; the tick of the landing and the next both count.
	var boatMoves, cartMoves [2]bool
	boatSince, cartSince := -1, -1
	var tntLanded, tntBounced, stoneTNTLanded, stoneTNTBounced, stoneBoatLanded, stoneUp bool
	moved := func(id int32) func(int32, []byte) bool {
		return func(pid int32, data []byte) bool {
			switch pid {
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayTeleportEntity:
				return protocol.NewReader(data).ReadVarInt() == id
			}
			return false
		}
	}
	// Run to the tick after the bodies land, one tick at a time so each
	// landing tick's packets are told apart.
	for range 25 {
		var boatMoved, cartMoved bool
		bm, cm := moved(boat), moved(cart)
		c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			boatMoved = boatMoved || bm(id, data)
			cartMoved = cartMoved || cm(id, data)
			return false
		})
		if boatSince >= 0 && boatSince < 2 {
			boatMoves[boatSince] = boatMoved
			boatSince++
		}
		if cartSince >= 0 && cartSince < 2 {
			cartMoves[cartSince] = cartMoved
			cartSince++
		}
		if _, vy, _, _ := srv.BoatVelocity(boat); vy > 0 && boatVY == 0 {
			boatVY, boatSince = vy, 0
			boatMoves[0] = boatMoved
			boatSince = 1
		}
		if vy, _ := srv.MinecartVelocityY(cart); vy > 0 && cartVY == 0 {
			cartVY, cartSince = vy, 0
			cartMoves[0] = cartMoved
			cartSince = 1
		}
		for _, tnt := range srv.TNTsInDimension(world.DimensionName) {
			// The ground's (0.7, -0.5, 0.7) multiplier turns the bounce
			// back down, so the TNT's tell is a velocity at rest on slime
			// that stone does not leave.
			switch tnt.X {
			case 12.5:
				tntLanded = tntLanded || tnt.Y == 64
				tntBounced = tntBounced || (tnt.Y == 64 && tnt.VY < -0.01)
			case 16.5:
				stoneTNTLanded = stoneTNTLanded || tnt.Y == 64
				stoneTNTBounced = stoneTNTBounced || (tnt.Y == 64 && tnt.VY != 0)
			}
		}
		if _, vy, _, _ := srv.BoatVelocity(stoneBoat); vy > 0 {
			stoneUp = true
		} else if !stoneBoatLanded && vy == 0 {
			stoneBoatLanded = true
		}
		if boatSince == 2 && cartSince == 2 && tntLanded && stoneTNTLanded && stoneBoatLanded {
			break
		}
	}
	boatTold, cartTold = boatMoves[0] || boatMoves[1], cartMoves[0] || cartMoves[1]
	// Landing velocities of a 4.5-block drop, with the drag vanilla gives
	// each body (boat 1.0, cart 0.95f).
	if math.Abs(boatVY-0.496000007390976) > 1e-9 {
		t.Errorf("boat landing VY = %v, want 0.496000007390976", boatVY)
	}
	if math.Abs(cartVY-0.37811525639048) > 1e-9 {
		t.Errorf("cart landing VY = %v, want 0.37811525639048", cartVY)
	}
	if !boatTold {
		t.Error("the boat's landing bounce was not told to its watcher that tick")
	}
	if !cartTold {
		t.Error("the minecart's landing bounce was not told to its watcher that tick")
	}
	if !tntLanded || !stoneTNTLanded || !stoneBoatLanded {
		t.Fatalf("a body never landed: slime TNT %v, stone TNT %v, stone boat %v", tntLanded, stoneTNTLanded, stoneBoatLanded)
	}
	if !tntBounced {
		t.Error("the primed TNT kept no bounce velocity on the slime")
	}
	if stoneTNTBounced {
		t.Error("the TNT on stone kept a vertical velocity")
	}
	if stoneUp {
		t.Error("the boat rose off stone")
	}
}

// A boat set down at rest on slime stays there: the one tick of gravity it
// falls does not exceed the gravity a bounce needs.
// vanilla: Entity.restituteMovementAfterCollisions
func TestRestingBoatOnSlimeDoesNotBounce(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Rester")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 63, 20, servertest.BlockState(t, "minecraft:slime_block"))
	boat := srv.SpawnBoat(4.5, 64, 20.5, "minecraft:oak_boat")
	for range 3 {
		servertest.RunTicks(t, srv, 1)
		if _, vy, _, _ := srv.BoatVelocity(boat); vy > 0 {
			t.Fatalf("a boat at rest on slime rose with VY %v", vy)
		}
	}
}

// A bounce posts its game event on the landing tick, sourced by the body,
// even when the TNT's fuse runs out on that same tick.
// vanilla: Entity.restituteMovementAfterCollisions, PrimedTnt.tick
func TestBodyBounceEventsAreDeliveredOnTheLandingTick(t *testing.T) {
	t.Parallel()
	// bounces drops the three bodies onto slime, ticks one at a time and
	// returns the tick the TNT left the slime upward. The TNT's fuse is the
	// one given; the bodies' bounce events are counted by source.
	bounces := func(t *testing.T, fuse int, stop int) (landing int, count func(id int32) int, tnt int32, tntUUID uuid.UUID) {
		srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
		servertest.HoldTicks(t, srv)
		for _, x := range []int32{4, 8, 12} {
			srv.SetBlock(x, 63, 20, servertest.BlockState(t, "minecraft:slime_block"))
		}
		boat := srv.SpawnBoat(4.5, 68.5, 20.5, "minecraft:oak_boat")
		cart := srv.SpawnMinecart(8.5, 68.5, 20.5)
		tnt = srv.SpawnTNT(12.5, 68.5, 20.5, fuse)
		ids := []int32{boat, cart, tnt}
		tntUUID = srv.BodyUUID(tnt)
		uuids := map[int32]uuid.UUID{}
		for _, id := range ids {
			uuids[id] = srv.BodyUUID(id)
		}
		var mu sync.Mutex
		sources := map[uuid.UUID]int{}
		srv.ObserveGameEventContexts(func(event string, x, y, z float64, ctx rules.GameEventContext) {
			if event == "minecraft:bounce" {
				mu.Lock()
				sources[ctx.Mob]++
				mu.Unlock()
			}
		})
		count = func(id int32) int {
			mu.Lock()
			defer mu.Unlock()
			return sources[uuids[id]]
		}
		up := map[int32]bool{}
		for tick := 1; tick <= 30; tick++ {
			servertest.RunTicks(t, srv, 1)
			if stop > 0 {
				if tick == stop {
					return tick, count, tnt, tntUUID
				}
				continue
			}
			_, bvy, _, _ := srv.BoatVelocity(boat)
			cvy, _ := srv.MinecartVelocityY(cart)
			var tvy float64
			for _, b := range srv.TNTsInDimension(world.DimensionName) {
				if b.ID == tnt && b.Y <= 64 {
					tvy = 1 // landed: the slime turns its velocity downward
				}
			}
			for id, vy := range map[int32]float64{boat: bvy, cart: cvy, tnt: tvy} {
				if vy > 0 && !up[id] {
					up[id] = true
					if count(id) == 0 {
						t.Errorf("the bounce of body %d was not posted with the body as its source on the landing tick %d", id, tick)
					}
				}
			}
			if len(up) == len(ids) {
				return tick, count, tnt, tntUUID
			}
		}
		t.Fatalf("only %d of the bodies bounced", len(up))
		return 0, nil, 0, uuid.Nil
	}
	var landing int
	t.Run("surviving", func(t *testing.T) { landing, _, _, _ = bounces(t, 200, 0) })
	t.Run("expiring", func(t *testing.T) {
		if landing == 0 {
			t.Skip("no landing tick")
		}
		// The TNT's fuse runs out on the tick it lands.
		_, count, tnt, _ := bounces(t, landing, landing)
		if count(tnt) == 0 {
			t.Error("the TNT whose fuse ran out as it landed posted no bounce event")
		}
	})
}
