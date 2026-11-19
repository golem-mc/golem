package entities

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
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
