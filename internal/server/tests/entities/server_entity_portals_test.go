package entities

import (
	"fmt"

	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The portals the non-player kinds cross. An item thrown into a nether
// portal leaves the overworld and arrives in the nether as the stack it
// carried; a primed TNT keeps the fuse it had left and burns it on the far
// side; a mob walks through; and the cooldown of a crossing stops the body
// that arrives inside the portal it came by from going straight back. The
// departing side sees the body go as a Remove Entities and the arriving
// side sees the one that got there as a Spawn Entity carrying the same
// UUID — the vanilla teleportCrossDimension pair, a new entity of the same
// identity raised from the old one's own tag.

// itemSpawnOf reads a Spawn Entity packet as an item entity's arrival: the
// entity ID and the UUID it was spawned under, or false for a spawn of some
// other kind.
func itemSpawnOf(data []byte) (int32, string, bool) {
	r := protocol.NewReader(data)
	id := r.ReadVarInt()
	u := r.ReadUUID()
	if typ := r.ReadVarInt(); typ != protocol.EntityTypeItem {
		return 0, "", false
	}
	return id, u.String(), true
}

// awaitItemSpawn reads until a Spawn Entity packet for an item entity
// arrives, stepping over the spawns of other kinds — a joining player's
// body among them — and returns the ID and UUID it was spawned under.
func awaitItemSpawn(t *testing.T, c *servertest.Client) (int32, string) {
	t.Helper()
	for {
		if id, u, ok := itemSpawnOf(c.AwaitPlay(protocol.IDPlaySpawnEntity)); ok {
			return id, u
		}
	}
}

// TestAnItemDroppedIntoAPortalTravelsToTheNether checks the crossing a
// player's own throw makes: the stack dropped into a lit portal is shown to
// the thrower as it leaves their hand, taken back out of their world as a
// Remove Entities the tick the portal takes it, and stands in the nether
// as the same stack under the same identity — where a second player,
// waiting there, is shown the body that got there.
func TestAnItemDroppedIntoAPortalTravelsToTheNether(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, connect := servertest.Start(t, cfg)

	// A second player waits in the nether eight blocks south of where the
	// crossing arrives, out of the stack's pickup reach. Their view keeps
	// the arrival's neighbours loaded: the stack flies on north out of the
	// arrival's chunk, and a chunk no one holds would unload and save it.
	// They leave before the thrower joins, so the thrower's only removal is
	// the stack's.
	mover := connect().Join("Arrival")
	srv.MovePlayerToDimension("Arrival", world.NetherName, 0, 80, 8)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	watcher := connect().Join("Thrower")

	servertest.LightPortal(t, srv, watcher)

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	watcher.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 3})
	// One block south of the portal, facing it — yaw 180 looks north, the
	// -Z the portal's interior lies in — and the drop key throws one stone
	// of the stack into it.
	watcher.MoveTo(5.0, servertest.PortalFloorY, 5.5, 180, 0)
	servertest.DropHeld(watcher, 5, servertest.PortalFloorY, 5)

	itemID, itemUUID := awaitItemSpawn(t, watcher)

	// The crossing: the body the thrower was shown is taken back out of
	// their world.
	remove := protocol.NewReader(watcher.AwaitPlay(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want 1", got)
	}
	if got := remove.ReadVarInt(); got != itemID {
		t.Errorf("removed entity = %d, want the item's %d", got, itemID)
	}

	// The arrival: the stack in the nether, the same identity, and none of
	// it left in the overworld. The waiting player's view holds stacks of
	// its own, plants the terrain pops among them, so the arrival is told
	// by its UUID.
	var arrival server.TestItem
	servertest.WaitFor(t, srv, 400, func() bool {
		nether := srv.ItemsInDimension(world.NetherName)
		i := slices.IndexFunc(nether, func(it server.TestItem) bool { return it.UUID == itemUUID })
		if i < 0 {
			return false
		}
		arrival = nether[i]
		return len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	if arrival.Item != "minecraft:stone" || arrival.Count != 1 {
		t.Errorf("the arrival holds %q x%d, want the thrown stone", arrival.Item, arrival.Count)
	}

	// The waiting player is shown the body that got there: a Spawn Entity
	// of its own, under a new entity ID and the same UUID the departing one
	// carried.
	for {
		arrivalID, arrivalUUID := awaitItemSpawn(t, mover)
		if arrivalUUID != itemUUID {
			continue // a stack the terrain popped
		}
		if arrivalID == itemID {
			t.Errorf("the arrival reused the departed body's entity id %d, want a new one", arrivalID)
		}
		break
	}
}

// TestAMobCarriesItsRiderThroughAPortal stands a pig with a zombie seated on
// it by /ride inside a lit portal: the rider may not use the portal itself,
// so the pig takes it along, and both arrive in the nether still riding.
// vanilla: Entity.canUsePortal, Entity.teleportCrossDimension
func TestAMobCarriesItsRiderThroughAPortal(t *testing.T) {
	t.Parallel()
	const name = "Rider"
	cfg := servertest.CrossingTestConfig(t)
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))

	servertest.LightPortal(t, srv, c)
	servertest.HoldTicks(t, srv)
	x, y, z := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5
	c.ChatCommand(fmt.Sprintf("summon minecraft:pig %v %v %v", x, y, z))
	c.ChatCommand(fmt.Sprintf("summon minecraft:zombie %v %v %v", x, y, z))
	c.ChatCommand("ride @e[type=minecraft:zombie,limit=1] mount @e[type=minecraft:pig,limit=1]")
	c.Settle()
	if mobs := srv.MobsInDimension(world.DimensionName); len(mobs) != 2 {
		t.Fatalf("overworld mobs before the crossing = %+v, want the pig and the zombie", mobs)
	}

	servertest.UntilTicks(t, srv, 5, "the pair to cross", func() bool {
		return len(srv.MobsInDimension(world.NetherName)) == 2
	})
	if left := srv.MobsInDimension(world.DimensionName); len(left) != 0 {
		t.Errorf("mobs left in the overworld = %+v, want none", left)
	}
	var pig, zombie server.TestMob
	for _, m := range srv.MobsInDimension(world.NetherName) {
		switch m.Kind {
		case "minecraft:pig":
			pig = m
		case "minecraft:zombie":
			zombie = m
		}
	}
	if pig.ID == 0 || zombie.ID == 0 {
		t.Fatalf("nether arrivals = %+v, want the pig and the zombie", srv.MobsInDimension(world.NetherName))
	}
	if zombie.Vehicle != pig.ID {
		t.Errorf("the zombie in the nether rides %d, want the pig %d", zombie.Vehicle, pig.ID)
	}
	if pig.Vehicle != 0 {
		t.Errorf("the pig in the nether rides %d, want nothing", pig.Vehicle)
	}
}

// thrownSpawnOf reads a Spawn Entity packet as a thrown egg's arrival: the
// entity ID and the UUID it was spawned under and the speed it still flies
// with, or false for a spawn of some other kind.
func thrownSpawnOf(data []byte) (int32, string, float64, bool) {
	r := protocol.NewReader(data)
	id := r.ReadVarInt()
	u := r.ReadUUID()
	if typ := r.ReadVarInt(); typ != protocol.EntityTypeEgg {
		return 0, "", 0, false
	}
	x, _, _ := r.ReadF64(), r.ReadF64(), r.ReadF64()
	_, _, vz := r.ReadLpVec3()
	_ = x
	return id, u.String(), vz, true
}

// awaitThrownEggSpawn reads until a Spawn Entity packet for a thrown egg
// arrives, stepping over the spawns of other kinds — a joining player's
// body among them — and returns the ID and UUID it was spawned under and
// the speed it was given.
func awaitThrownEggSpawn(t *testing.T, c *servertest.Client) (int32, string, float64) {
	t.Helper()
	for {
		if id, u, vz, ok := thrownSpawnOf(c.AwaitPlay(protocol.IDPlaySpawnEntity)); ok {
			return id, u, vz
		}
	}
}

// TestAThrownEggFliesThroughAPortalToTheNether throws an egg level at a lit
// portal standing a block out of the spawn and checks the crossing: the egg
// the thrower was shown is taken back out of their world as a Remove
// Entities, and the body that got there is shown to a player already
// standing in the nether — a Spawn Entity under a new entity ID and the same
// UUID the departing one carried, with the flight's own speed still in it.
// The nether portal the crossing needs is built beforehand by dropping a
// stone through the same portal, so the arrival stands where a second
// player can be waiting for the egg rather than chasing a body that flies
// on and breaks the tick after it lands.
func TestAThrownEggFliesThroughAPortalToTheNether(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, connect := servertest.Start(t, cfg)

	thrower := connect().Join("Thrower")

	servertest.LightPortal(t, srv, thrower)

	// A stone dropped through the portal first: its crossing builds the
	// nether portal the egg is to arrive by, and its arrival says where
	// that stands.
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	thrower.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	// One block south of the portal, facing it — yaw 180 looks north, the
	// -Z the portal's interior lies in — and the drop key throws the stone
	// into it.
	thrower.MoveTo(5.0, servertest.PortalFloorY, 5.5, 180, 0)
	servertest.DropHeld(thrower, 5, servertest.PortalFloorY, 5)
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1 &&
			len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	spot := srv.ItemsInDimension(world.NetherName)[0]

	// A second player standing in the nether two blocks out of the
	// arrival's plane — the walk-out a built portal clears, so their own
	// standing does not send them straight back — inside the four blocks
	// a thrown body is shown within.
	mover := connect().Join("Arrival")
	srv.MovePlayerToDimension("Arrival", world.NetherName,
		math.Floor(spot.X), math.Floor(spot.Y), math.Floor(spot.Z)+2)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	// The throw: an egg from two blocks south of the portal's plane, level
	// with the interior's middle, facing it — yaw 180 looks north — and a
	// level throw crosses the plane inside its one tick and a half of
	// flight: a tick boundary stands the egg inside the portal's cell, the
	// crossing's own question.
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:egg"), Count: 16})
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	// The same spot again, the movement the throw rides nought — the walk
	// out of the spawn's is not the northward flight's own.
	thrower.MoveTo(5.0, servertest.PortalFloorY, 6.0, 180, 0)
	servertest.UseHeld(thrower, 180, 0)

	eggID, eggUUID, eggVZ := awaitThrownEggSpawn(t, thrower)
	if eggVZ > -1.0 {
		t.Errorf("the thrown egg's spawn velocity vz = %v, want the northward flight's own speed", eggVZ)
	}

	// The crossing: the body the thrower was shown is taken back out of
	// their world.
	remove := protocol.NewReader(thrower.AwaitPlay(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want 1", got)
	}
	if got := remove.ReadVarInt(); got != eggID {
		t.Errorf("removed entity = %d, want the egg's %d", got, eggID)
	}

	// The arrival, as the waiting mover is shown it: a Spawn Entity of its
	// own, under a new entity ID and the same UUID the departing one
	// carried, with the flight's own speed still in it — vanilla's
	// transition carrying the movement relative, the egg keeps flying on
	// the far side. The spawn goes out the moment the body is registered,
	// so the reading needs no racing against a flight that ends the tick
	// after it lands.
	arrivalID, arrivalUUID, arrivalVZ := awaitThrownEggSpawn(t, mover)
	if arrivalID == eggID {
		t.Errorf("the arrival reused the departed body's entity id %d, want a new one", eggID)
	}
	if arrivalUUID != eggUUID {
		t.Errorf("the arrival's spawn uuid = %s, want the departed body's %s", arrivalUUID, eggUUID)
	}
	if arrivalVZ > -1.0 {
		t.Errorf("the arrival's spawn velocity vz = %v, want the flight carried on at its own speed", arrivalVZ)
	}

	// And the arrival's own life: the egg breaks on the far side the tick
	// after it lands, and the standing player is told so — the body the
	// spawn showed is taken back out again.
	gone := protocol.NewReader(mover.AwaitPlay(protocol.IDPlayRemoveEntities))
	if got := gone.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities at the arrival = %d, want 1", got)
	}
	if got := gone.ReadVarInt(); got != arrivalID {
		t.Errorf("removed entity at the arrival = %d, want the arrived egg's %d", got, arrivalID)
	}
}

// TestAMobStandingWhereAPortalIsLitCrossesAfterAnIdleStretch lights the
// portal around a pig that has stood in a dimension with no portal block
// for a while, so the mob portal pass has been skipped, and checks the pig
// still crosses.
func TestAMobStandingWhereAPortalIsLitCrossesAfterAnIdleStretch(t *testing.T) {
	t.Parallel()
	const name = "Lighter"
	cfg := servertest.CrossingTestConfig(t)
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))

	x, y, z := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5
	c.ChatCommand(fmt.Sprintf("summon minecraft:pig %v %v %v", x, y, z))
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	servertest.LightPortal(t, srv, c)
	servertest.UntilTicks(t, srv, 10, "the pig to cross", func() bool {
		return len(srv.MobsInDimension(world.NetherName)) == 1
	})
}
