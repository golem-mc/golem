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
	"github.com/golem-mc/golem/registry"
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

// vanilla: Entity.move selects the supporting block after setting the final position.
func TestBoatBounceUsesLandingSupport(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, from, to string
		wantVY         float64
	}{
		{"slime_to_stone", "minecraft:slime_block", "minecraft:stone", 0},
		{"stone_to_slime", "minecraft:stone", "minecraft:slime_block", 0.8473846280116301},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Crosser", servertest.OpsListing(t, "Crosser"))
			servertest.HoldTicks(t, srv)
			from, to := servertest.BlockState(t, tc.from), servertest.BlockState(t, tc.to)
			for x := int32(5); x <= 9; x++ {
				state := to
				if x < 7 {
					state = from
				}
				for z := int32(19); z <= 21; z++ {
					srv.SetBlock(x, 63, z, state)
				}
			}
			boat := srv.SpawnBoat(6.5, 64.5, 20.5, "minecraft:oak_boat")
			c.ChatCommand(`data merge entity @e[type=minecraft:oak_boat,limit=1] {Motion:[2d,-1d,0d]}`)
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			if x, y, z, _, grounded, ok := srv.BoatState(boat); !ok || !grounded || math.Abs(x-8.3) > 1e-9 || y != 64 || z != 20.5 {
				t.Fatalf("boat at (%v, %v, %v), grounded %v, exists %v; want (8.3, 64, 20.5), grounded", x, y, z, grounded, ok)
			}
			if _, vy, _, ok := srv.BoatVelocity(boat); !ok || math.Abs(vy-tc.wantVY) > 1e-9 {
				t.Errorf("landing VY = %v, exists %v; want %v", vy, ok, tc.wantVY)
			}
		})
	}
}

// A boat landing on slime is told to its watcher within a tick of the
// landing whichever tick of the update interval that is: the bounce asks the
// tracker for a movement tick at once.
// vanilla: Entity.restituteMovementAfterCollisions, ServerEntity.sendChanges
func TestBoatBounceIsToldBetweenTrackingUpdates(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	// Drop heights half a block apart land on every tick of the 3-tick
	// interval.
	heights := []float64{68.5, 69, 69.5, 70, 70.5, 71}
	ids := make([]int32, len(heights))
	for i, y := range heights {
		x := int32(4 + 2*i)
		srv.SetBlock(x, 63, 20, servertest.BlockState(t, "minecraft:slime_block"))
		ids[i] = srv.SpawnBoat(float64(x)+0.5, y, 20.5, "minecraft:oak_boat")
	}
	landed := make([]int, len(ids)) // ticks since the bounce, 0 before it
	told := make([]bool, len(ids))
	for range 30 {
		moved := make([]bool, len(ids))
		c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
			switch pid {
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayTeleportEntity:
				id := protocol.NewReader(data).ReadVarInt()
				for i := range ids {
					moved[i] = moved[i] || id == ids[i]
				}
			}
			return false
		})
		done := true
		for i, id := range ids {
			if landed[i] > 0 && landed[i] < 2 {
				told[i] = told[i] || moved[i]
				landed[i]++
			} else if _, vy, _, _ := srv.BoatVelocity(id); vy > 0 && landed[i] == 0 {
				told[i] = moved[i]
				landed[i] = 1
			}
			done = done && landed[i] >= 2
		}
		if done {
			break
		}
	}
	for i := range ids {
		if landed[i] == 0 {
			t.Fatalf("boat dropped from %v never bounced", heights[i])
		}
		if !told[i] {
			t.Errorf("the bounce of the boat dropped from %v was not told within a tick", heights[i])
		}
	}
}

// A TNT whose move is slowed by a cobweb has its velocity cleared before it
// lands, so the slime gives nothing back.
// vanilla: Entity.move, Entity.restituteMovementAfterCollisions
func TestWebbedTNTDoesNotBounceOnSlime(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Webber", servertest.OpsListing(t, "Webber"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 63, 20, servertest.BlockState(t, "minecraft:slime_block"))
	srv.SetBlock(4, 64, 20, servertest.BlockState(t, "minecraft:cobweb"))
	srv.SpawnTNT(4.5, 64, 20.5, 200)
	servertest.RunTicks(t, srv, 1) // the web is entered
	c.ChatCommand(`data merge entity @e[type=minecraft:tnt,limit=1] {Motion:[0d,-1d,0d]}`)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	tnts := srv.TNTsInDimension(world.DimensionName)
	if len(tnts) != 1 {
		t.Fatalf("TNT bodies = %v, want one", tnts)
	}
	if tnts[0].Y != 64 || tnts[0].VY != 0 {
		t.Fatalf("webbed TNT at Y %v with VY %v, want Y 64 and VY 0", tnts[0].Y, tnts[0].VY)
	}
}

// A boat and a minecart bouncing on a shelf mushroom play its bounce sound at
// the block; primed TNT bounces on one silently.
// vanilla: ShelfMushroomBlock.bounceOn
func TestBoatsAndCartsBounceOnShelfMushroomWithItsSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	servertest.HoldTicks(t, srv)
	shelf := servertest.MustState(t, "minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "0"})
	for _, x := range []int32{4, 8, 12} {
		srv.SetBlock(x, 63, 21, servertest.BlockState(t, "minecraft:stone"))
		srv.SetBlock(x, 63, 20, shelf)
	}
	// The boat's hull is wider than the shelf: the floor beside it and the
	// backing stone would carry it (FeetSupport), so both are cleared.
	for x := int32(3); x <= 5; x++ {
		for z := int32(19); z <= 20; z++ {
			if x != 4 || z != 20 {
				srv.SetBlock(x, 63, z, servertest.BlockState(t, "minecraft:air"))
			}
		}
	}
	srv.SpawnBoat(4.5, 68.5, 20.0, "minecraft:oak_boat")
	srv.SpawnMinecart(8.5, 68.5, 20.5)
	srv.SpawnTNT(12.5, 68.5, 20.5, 200)
	c.MoveToGround(8.5, 64, 15.5, true) // within the sound's 16 blocks
	c.Settle()

	bounce, _ := registry.SoundEventID("minecraft:block.shelf_mushroom.bounce")
	heard := map[int32]bool{}
	c.ReadTicks(srv, 25, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt()-1 == bounce {
			r.ReadVarInt()
			heard[r.ReadI32()/8] = true
		}
		return false
	})
	if !heard[4] {
		t.Error("the boat's bounce played no shelf mushroom sound")
	}
	if !heard[8] {
		t.Error("the minecart's bounce played no shelf mushroom sound")
	}
	if heard[12] {
		t.Error("the primed TNT's bounce played a shelf mushroom sound")
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
