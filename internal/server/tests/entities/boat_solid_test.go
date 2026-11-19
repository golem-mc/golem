package entities

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// The boat's own solidity: Entity.collide runs the level's entity
// colliders in beside the block shapes, and AbstractBoat.canBeCollidedWith
// is true, so the bodies the server steps — the mobs, the items and the
// orbs — stop at a hull. A dropped item, a falling cow and a falling orb
// come to rest at the hull's top instead of sinking to the ground under
// it, and a mob walking at a side is held out of the hull's box, stalled
// at its face or afoot on the deck. Each test stands a hull on the flat
// world's dry ground, whose top y=64 the cells at 63 carry.

// TestADroppedItemRestsOnABoatHull: a stack dropped over a hull falls no
// further than the hull's top, resting with the middle of its quarter
// block box one half over the deck.
// vanilla: Entity.collide, AbstractBoat.canBeCollidedWith
func TestADroppedItemRestsOnABoatHull(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Dropper", 0)
	if srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat") == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	id := srv.SpawnItemEntity(2.5, 66, 2.5, "minecraft:dirt", 1)
	rest := 64 + rules.BoatHeight + entity.ItemHalfSize

	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 200, "the item at rest", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.ID == id {
				return it.VY == 0 && math.Abs(it.Y-rest) <= 0.01
			}
		}
		return false
	})
	var where *server.TestItem
	if items := srv.ItemsInDimension(world.DimensionName); len(items) > 0 {
		for i := range items {
			if items[i].ID == id {
				where = &items[i]
				break
			}
		}
	}
	if where == nil {
		t.Fatal("the dropped item is gone from the world")
	}
	if math.Abs(where.Y-rest) > 0.005 {
		t.Errorf("the item rests at y=%v, want the hull's top %v plus half its box %v",
			where.Y, 64+rules.BoatHeight, entity.ItemHalfSize)
	}
	if math.Abs(where.X-2.5) > 0.05 || math.Abs(where.Z-2.5) > 0.05 {
		t.Errorf("the item drifted to (%v %v), want where it was dropped over the hull", where.X, where.Z)
	}
}

// TestAFallingOrbRestsOnABoatHull: the experience orb falls onto a hull
// and comes to rest standing on its top, three whole ticks at the deck and
// never below it.
// vanilla: Entity.collide, AbstractBoat.canBeCollidedWith
func TestAFallingOrbRestsOnABoatHull(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	defer srv.HoldChunkInDimension(world.DimensionName, 2, 2)()
	if srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat") == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	orb := srv.SpawnOrb(2.5, 66, 2.5, 1)
	if orb == 0 {
		t.Fatal("SpawnOrb came to nothing")
	}
	deck := 64 + rules.BoatHeight
	servertest.HoldTicks(t, srv)
	atDeck := 0
	rested := false
	for tick := 1; tick <= 200; tick++ {
		servertest.RunTicks(t, srv, 1)
		if _, y, _, ok := srv.OrbPos(orb); ok && math.Abs(y-deck) <= 0.005 {
			if atDeck == 0 {
				atDeck = tick
			}
			if tick >= atDeck+3 {
				rested = true
				break
			}
		} else {
			atDeck = 0
		}
	}
	if !rested {
		if _, y, _, ok := srv.OrbPos(orb); ok {
			t.Fatalf("the orb came to rest at y=%v, want the hull's top %v", y, deck)
		}
		t.Fatalf("the orb never came to rest at the hull's top %v", deck)
	}
}

// TestACowFallingOnABoatLandsOnItsHull: a cow dropped over a hull lands
// standing on it, its whole fall measured by the lowest place a tick's end
// ever left it — at the hull's top and never below. The landing holds in
// every dimension alike: the pass answers the hulls of whatever world the
// body falls in.
// vanilla: Entity.collide, AbstractBoat.canBeCollidedWith
func TestACowFallingOnABoatLandsOnItsHull(t *testing.T) {
	t.Parallel()
	for _, dim := range []string{world.DimensionName, world.NetherName, world.EndName} {
		t.Run(dim, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Start(t, servertest.BlockTestConfig())
			defer srv.HoldChunkInDimension(dim, 2, 2)()
			if dim != world.DimensionName {
				// The nether's flat fill runs solid to its roof and the
				// End holds no ground at all: both are given the flat
				// world's own shape — stone at 63, air above — so the
				// hull stands where the overworld's fixture does.
				air := servertest.MustState(t, "minecraft:air", nil)
				stone := servertest.MustState(t, "minecraft:stone", nil)
				for x := int32(1); x <= 3; x++ {
					for z := int32(1); z <= 3; z++ {
						srv.SetBlockInDimension(dim, x, 63, z, stone)
						for y := int32(64); y <= 67; y++ {
							srv.SetBlockInDimension(dim, x, y, z, air)
						}
					}
				}
			}

			var boat int32
			if dim == world.DimensionName {
				boat = srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat")
			} else {
				boat = srv.SpawnBoatInDimension(dim, 2.5, 64, 2.5, "minecraft:oak_boat")
			}
			if boat == 0 {
				t.Fatalf("no boat stood in %s", dim)
			}
			cow := srv.SpawnMobInDimension("minecraft:cow", dim, 2.5, 66, 2.5)
			if cow == 0 {
				t.Fatalf("no cow stood in %s", dim)
			}

			deck := 64 + rules.BoatHeight
			// The lowest place any tick's end ever left the cow at:
			// the deck itself where the hull's top stopped the fall,
			// deeper where it did not.
			mobY := func() (float64, bool) {
				for _, m := range srv.MobsInDimension(dim) {
					if m.ID == cow {
						return m.Y, true
					}
				}
				return 0, false
			}
			lowest := math.Inf(1)
			reached := false
			servertest.HoldTicks(t, srv)
			for tick := 0; tick < 200 && !reached; tick++ {
				servertest.RunTicks(t, srv, 1)
				if y, ok := mobY(); ok {
					lowest = math.Min(lowest, y)
					reached = y <= deck+0.002
				}
			}
			if lowest == math.Inf(1) || !reached {
				t.Fatalf("the cow never landed in %s, its lowest y=%v", dim, lowest)
			}
			if lowest < deck-0.002 {
				t.Errorf("the cow sank to y=%v in %s, want the fall stopped at the hull's top %v", lowest, dim, deck)
			}
		})
	}
}

// TestACowFallingOnAFloatingBoatStandsOnTheDeck: a hull floated on water
// bobs as the water finds its level, and its own move stops at a pushable
// body — vanilla's AbstractBoat.tick runs the move first, and
// canVehicleCollide counts isPushable — so a cow dropped over it comes to
// rest standing on the deck instead of being taken into the seat once the
// rising hull passes under it. The deck holds in every dimension alike.
// vanilla: AbstractBoat.tick's move, Entity.collide, AbstractBoat.canVehicleCollide
func TestACowFallingOnAFloatingBoatStandsOnTheDeck(t *testing.T) {
	t.Parallel()
	for _, dim := range []string{world.DimensionName, world.NetherName, world.EndName} {
		t.Run(dim, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Start(t, servertest.BlockTestConfig())
			defer srv.HoldChunkInDimension(dim, 2, 2)()
			// A pool of source water a block deep: a hull laid a little
			// under the surface rises to the level it floats at, so its
			// move meets the cow as it falls.
			water := servertest.MustState(t, "minecraft:water", nil)
			air := servertest.MustState(t, "minecraft:air", nil)
			stone := servertest.MustState(t, "minecraft:stone", nil)
			for x := int32(0); x <= 4; x++ {
				for z := int32(0); z <= 4; z++ {
					if dim != world.DimensionName {
						// The nether's flat fill runs solid to its roof
						// and the End holds no ground at all: both are
						// given the flat world's own shape — stone at 63,
						// air above — around the pool.
						srv.SetBlockInDimension(dim, x, 63, z, stone)
						for y := int32(64); y <= 70; y++ {
							srv.SetBlockInDimension(dim, x, y, z, air)
						}
					}
					srv.SetBlockInDimension(dim, x, 63, z, water)
				}
			}
			boat := srv.SpawnBoatInDimension(dim, 2.5, 63.5, 2.5, "minecraft:oak_boat")
			if boat == 0 {
				t.Fatalf("no boat stood in %s", dim)
			}
			cow := srv.SpawnMobInDimension("minecraft:cow", dim, 2.5, 67, 2.5)
			if cow == 0 {
				t.Fatalf("no cow stood in %s", dim)
			}

			deckOf := func() (float64, bool) {
				_, by, _, _, _, ok := srv.BoatState(boat)
				return by + rules.BoatHeight, ok
			}
			mobY := func() (float64, bool) {
				for _, m := range srv.MobsInDimension(dim) {
					if m.ID == cow {
						return m.Y, true
					}
				}
				return 0, false
			}
			// The cow must never take a seat: the hull's move stopping at
			// it is what keeps the boarding sweep off it. Once the fall
			// is spent it stays on the deck.
			var cowY float64
			servertest.HoldTicks(t, srv)
			for range 60 {
				servertest.RunTicks(t, srv, 1)
				if len(srv.BoatPassengerIDs(boat)) > 0 {
					t.Fatalf("the cow was taken into a seat in %s, want it left standing on the deck", dim)
				}
				y, ok := mobY()
				if !ok {
					t.Fatalf("the cow is gone from %s", dim)
				}
				cowY = y
			}
			if deck, ok := deckOf(); !ok || math.Abs(cowY-deck) > 0.02 {
				t.Fatalf("the cow rests at y=%v in %s, want the deck %v", cowY, dim, deck)
			}
		})
	}
}

// TestAFloatingBoatRisesThroughBodiesItCannotPush: a spectator and a dead
// player are not pushable, so neither holds a floating hull back: the hull
// rises through the body's box to the level it floats at, where a living
// body would stall it.
// vanilla: AbstractBoat.canVehicleCollide, LivingEntity.isPushable
func TestAFloatingBoatRisesThroughBodiesItCannotPush(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mode string
		dead bool
	}{
		{"spectator", "spectator", false},
		{"dead", "survival", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.BoatPlayer(t, tc.mode, "Ghost", 0)
			// A partially submerged hull rises into the body just above
			// its ceiling; a fully submerged hull would sink instead.
			water := servertest.MustState(t, "minecraft:water", nil)
			for x := int32(0); x <= 4; x++ {
				for z := int32(0); z <= 4; z++ {
					srv.SetBlockInDimension(world.DimensionName, x, 62, z, water)
					srv.SetBlockInDimension(world.DimensionName, x, 63, z, water)
				}
			}
			c.MoveTo(2.5, 63.95, 2.5, 0, 0)
			c.Settle()
			if tc.dead && (!srv.Hurt("Ghost", 1000) || srv.PlayerHealth("Ghost") > 0) {
				t.Fatal("the ghost did not die")
			}
			boat := srv.SpawnBoat(2.5, 63.35, 2.5, "minecraft:oak_boat")
			if boat == 0 {
				t.Fatal("SpawnBoat came to nothing")
			}
			servertest.HoldTicks(t, srv)
			servertest.UntilTicks(t, srv, 200, "the hull risen", func() bool {
				_, y, _, _, _, ok := srv.BoatState(boat)
				return ok && y > 63.45
			})
			if _, y, _, _, _, ok := srv.BoatState(boat); !ok || y <= 63.45 {
				t.Fatalf("the hull rests at y=%v (%v) under the %s body, want it risen to where it floats", y, ok, tc.name)
			}
		})
	}
}

// TestABoatFallingOnABodyRestsOnItsTop: a hull dropped over a body rests on
// the top of the body's box, the surface Entity.collide clips it to, not on
// the block floor a body's box would snap it to. The body is an iron golem,
// wider than the hull, so the boarding sweep leaves it ashore.
// vanilla: Entity.collide, AbstractBoat.canVehicleCollide
func TestABoatFallingOnABodyRestsOnItsTop(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	defer srv.HoldChunkInDimension(world.DimensionName, 2, 2)()
	srv.StopTick()
	golem := srv.SpawnMob("minecraft:iron_golem", 2.5, 64, 2.5)
	if golem == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	srv.SetMobNoAI(golem)
	boat := srv.SpawnBoat(2.5, 70, 2.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}

	// An iron golem's 2.7 box rests on the flat world's ground, so its top
	// is 66.7: a body's box, not a block's, is what the hull lands on.
	top := 64 + 2.7
	srv.TickBoats(150)
	for range 40 {
		srv.TickBoats(1)
		_, y, _, _, _, ok := srv.BoatState(boat)
		if !ok {
			t.Fatal("the boat is gone from the world")
		}
		if math.Abs(y-top) > 0.02 {
			t.Fatalf("the boat rests at y=%v, want the body's top %v", y, top)
		}
	}
}

// TestABoatOverlappingABodyFallsOutOfIt: a hull laid already inside a body's
// box falls free of the overlap to the ground under it — Entity.collide
// clips a body only when its leading face is outside the body's box — where
// a block-floor snap would shove the hull up onto the box.
// vanilla: Entity.collide, AbstractBoat.canVehicleCollide
func TestABoatOverlappingABodyFallsOutOfIt(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	defer srv.HoldChunkInDimension(world.DimensionName, 2, 2)()
	srv.StopTick()
	golem := srv.SpawnMob("minecraft:iron_golem", 2.5, 64, 2.5)
	if golem == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	srv.SetMobNoAI(golem)
	// Laid with its box already crossing the golem's 64..66.7 box.
	boat := srv.SpawnBoat(2.5, 66.5, 2.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}

	srv.TickBoats(150)
	_, y, _, _, _, ok := srv.BoatState(boat)
	if !ok {
		t.Fatal("the boat is gone from the world")
	}
	if math.Abs(y-64) > 0.02 {
		t.Fatalf("the boat rests at y=%v, want the ground 64: a hull already overlapping a body falls free of it", y)
	}
}

// TestABoatFallsPastAClimbingBody: a climbing mob is not pushable, so a
// hull dropped over it falls through the body's box to the ground, where the
// same mob at rest would stop the fall on its top. The spider is wider than
// the hull, so the boarding sweep leaves it ashore.
// vanilla: Entity.collide, AbstractBoat.canVehicleCollide, LivingEntity.isPushable
func TestABoatFallsPastAClimbingBody(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	defer srv.HoldChunkInDimension(world.DimensionName, 2, 2)()
	srv.StopTick()
	spider := srv.SpawnMob("minecraft:spider", 2.5, 64, 2.5)
	if spider == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	srv.SetMobNoAI(spider)
	if !srv.SetMobClimbing(spider) {
		t.Fatal("the spider is not in the world")
	}
	boat := srv.SpawnBoat(2.5, 68, 2.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}

	srv.TickBoats(200)
	_, y, _, _, _, ok := srv.BoatState(boat)
	if !ok {
		t.Fatal("the boat is gone from the world")
	}
	if math.Abs(y-64) > 0.02 {
		t.Fatalf("the boat rests at y=%v, want the ground 64: a climbing body is not pushable and stops no hull", y)
	}
}

// TestACowDrivenAtABoatsSideDoesNotEnterItsBox: a cow walked at a hull by
// the lure it follows — a player holding wheat beyond the boat — never
// stands inside the hull's box: its move stops at the side, whose
// nine-sixteenths height sits under the step height's 0.6, leaving the
// body stalled at the face or afoot on the deck. The hull's two seats
// are taken first, so the sweep that pushes a mob aboard has nobody to
// take and the walk is the move's alone.
// vanilla: Entity.collide, AbstractBoat.canBeCollidedWith
func TestACowDrivenAtABoatsSideDoesNotEnterItsBox(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Herder", 0)
	boat := srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	for range 2 {
		if srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5) == 0 {
			t.Fatal("SpawnMob came to nothing")
		}
	}
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 200, "both cows seated", func() bool {
		return len(srv.BoatPassengerIDs(boat)) == 2
	})
	srv.GiveSlot("Herder", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wheat"), Count: 1})
	c.MoveToGrounded(7.5, 64, 2.5, 180, 0, true)
	c.Settle()
	cow := srv.SpawnMob("minecraft:cow", 0.5, 64, 2.5)
	if cow == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	if srv.MobIsBaby(cow) {
		t.Fatal("the planted cow is a baby; plant it elsewhere")
	}

	// The hull's box, and whether the cow's own — an adult cow's 0.9 by
	// 1.4 — stands strictly inside it.
	hullMinX, hullMaxX := 2.5-rules.BoatHalfWidth, 2.5+rules.BoatHalfWidth
	hullMinZ, hullMaxZ := 2.5-rules.BoatHalfWidth, 2.5+rules.BoatHalfWidth
	deck := 64 + rules.BoatHeight
	inside := func(x, y, z float64) bool {
		const half, height = 0.45, 1.4
		return x+half > hullMinX && x-half < hullMaxX &&
			z+half > hullMinZ && z-half < hullMaxZ &&
			y < deck && y+height > 64
	}

	// The walk ends where the pressed stint has held the cow out of the
	// hull's box for five seconds — stalled against the side, afoot on
	// the deck or climbing over it — or where it entered, which is the
	// failure the test is for.
	insideSeen := false
	pressedSince := 0
	for tick := 1; tick <= 600; tick++ {
		servertest.RunTicks(t, srv, 1)
		x, y, z, ok := srv.MobPos(cow)
		if ok {
			if inside(x, y, z) {
				insideSeen = true
				break
			}
			if pressedSince == 0 && x+0.45 >= hullMinX {
				pressedSince = tick
			}
		}
		if pressedSince != 0 && tick-pressedSince >= 100 {
			break
		}
	}
	if insideSeen {
		x, y, z, _ := srv.MobPos(cow)
		t.Fatalf("the cow entered the hull's box at (%v %v %v)", x, y, z)
	}
	if pressedSince == 0 {
		x, _, _, _ := srv.MobPos(cow)
		t.Fatalf("the cow never walked its box up to the hull's side: it stands at x=%v", x)
	}
}
