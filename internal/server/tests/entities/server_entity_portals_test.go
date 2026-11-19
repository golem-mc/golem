package entities

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
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

// TestAnItemAtRestInAPortalCrossesAndKeepsItsStack checks the crossing a
// lying stack makes and the staying of the one that arrives: a stack of
// three at rest inside the portal's bottom interior cell leaves the
// overworld whole and stands in the nether as the same stack — and it stays
// there, the arrival standing inside the portal it came by with the
// crossing's cooldown running, which is what stops an immediate return.
func TestAnItemAtRestInAPortalCrossesAndKeepsItsStack(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Keeper")

	_, _, _, _ = servertest.PortalStates(t)
	servertest.LightPortal(t, srv, c)

	// A stack of three stones lying at rest inside the portal's bottom
	// interior cell, where a throw would leave one: the item's position is
	// the centre of its own box, half a box above the floor it lies on.
	if id := srv.SpawnItemEntity(float64(servertest.PortalInnerX0)+0.5,
		float64(servertest.PortalFloorY)+entity.ItemHalfSize, float64(servertest.PortalInnerZ)+0.5,
		"minecraft:stone", 3); id == 0 {
		t.Fatal("spawning the item entity failed")
	}

	// The crossing: the stack leaves the overworld whole and stands in the
	// nether as the same stack.
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1 &&
			len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	items := srv.ItemsInDimension(world.NetherName)
	if items[0].Item != "minecraft:stone" || items[0].Count != 3 {
		t.Errorf("the arrival holds %q x%d, want the whole stack of three",
			items[0].Item, items[0].Count)
	}

	// The staying: the arrival stands inside the portal it came by, where
	// the cooldown of its own crossing is reset for as long as it stands
	// there — the thing that stops an immediate return. A body without
	// one would be back in the overworld the tick after it arrived; two
	// seconds of standing leaves no doubt about which way it is.
	servertest.AwaitTicks(t, srv, 40)
	if got := srv.ItemsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("the arrival bounced back: %d items in the overworld", len(got))
	}
	if got := srv.ItemsInDimension(world.NetherName); len(got) != 1 {
		t.Errorf("the arrival is no longer in the nether: %d items", len(got))
	}
}

// TestAnItemSlidingOutOfItsArrivalChunkKeepsTicking checks that a crossing
// holds the chunks around its arrival, not just the one it lands in: with no
// player in the nether, a stack that crosses moving south arrives in the
// nether portal built at z=-1 in chunk (0,-1), slides on into chunk (0,0),
// and is still live and ticking there twenty ticks after the crossing.
// vanilla: Entity.placePortalTicket
func TestAnItemSlidingOutOfItsArrivalChunkKeepsTicking(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Slider")

	servertest.LightPortal(t, srv, c)
	servertest.HoldTicks(t, srv)
	// No player stands in the nether, so the chunks the stack arrives in and
	// slides into are held entity-ticking, as a player's would be.
	for _, z := range []int32{-32, -16, 0} {
		t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, z))
	}

	// Moving south, across the portal's plane, fast enough to slide over
	// the half block between the arrival and the next chunk.
	if id := srv.SpawnItemEntityMotion(float64(servertest.PortalInnerX0)+0.5,
		float64(servertest.PortalFloorY)+entity.ItemHalfSize, float64(servertest.PortalInnerZ)+0.5,
		"minecraft:stone", 1, 0, 0, 0.3); id == 0 {
		t.Fatal("spawning the item entity failed")
	}
	var arrival server.TestItem
	servertest.UntilTicks(t, srv, 40, "the stack crossing to the nether", func() bool {
		nether := srv.ItemsInDimension(world.NetherName)
		if len(nether) != 1 {
			return false
		}
		arrival = nether[0]
		return true
	})
	arrivalChunk := world.ChunkPos{X: world.ChunkOf(arrival.X), Z: world.ChunkOf(arrival.Z)}

	servertest.RunTicks(t, srv, 20)
	nether := srv.ItemsInDimension(world.NetherName)
	if len(nether) != 1 || nether[0].UUID != arrival.UUID {
		t.Fatalf("nether items 20 ticks after the crossing = %+v, want the arrival %s", nether, arrival.UUID)
	}
	got := nether[0]
	if chunk := (world.ChunkPos{X: world.ChunkOf(got.X), Z: world.ChunkOf(got.Z)}); chunk == arrivalChunk {
		t.Fatalf("the stack stayed in its arrival chunk %v at z=%.3f; the test needs it to leave", chunk, got.Z)
	}
	// Age counts the ticks the stack was stepped: one held but not ticking
	// would have stopped at its entry into the next chunk.
	if got.Age-arrival.Age != 20 {
		t.Errorf("the stack aged %d ticks in the 20 after the crossing, want 20", got.Age-arrival.Age)
	}
}

// TestAPrimedTNTKeepsItsFuseAcrossAPortal checks the crossing a burning
// body makes: a primed TNT stood inside a lit portal leaves the overworld
// with the fuse it had left — not the default eighty a fresh one carries —
// burns it down on the far side, and its blast, being a body's that used a
// portal, spares every block of the portal it arrived in, while the portal
// it left through stands unblasted.
func TestAPrimedTNTKeepsItsFuseAcrossAPortal(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Blaster")

	_, portalX, portalZ, _ := servertest.PortalStates(t)
	servertest.LightPortal(t, srv, c)
	// Held, so the arrival is read on the tick it lands: on a free loop a
	// starved reader can miss the forty ticks it burns in the nether.
	servertest.HoldTicks(t, srv)
	// No player stands in the nether, so the chunks the body arrives in are
	// held entity-ticking, as a player's would be.
	for _, z := range []int32{-16, 0} {
		t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, z))
	}

	// A primed body burning on a shortened fuse, stood inside the portal's
	// bottom interior cell.
	const fuse = 40
	if id := srv.SpawnTNT(float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalFloorY),
		float64(servertest.PortalInnerZ)+0.5, fuse); id == 0 {
		t.Fatal("spawning the primed tnt failed")
	}

	// The crossing: the body leaves the overworld burning and arrives in
	// the nether with the fuse it had left.
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.TNTsInDimension(world.NetherName)) == 1 &&
			len(srv.TNTsInDimension(world.DimensionName)) == 0
	})
	arrival := srv.TNTsInDimension(world.NetherName)[0]
	if arrival.Fuse <= 0 || arrival.Fuse > fuse {
		t.Errorf("the arrived fuse = %d, want the shortened fuse it left with, unspent and unburnt-out",
			arrival.Fuse)
	}

	// The arrival stands inside the portal the crossing built.
	ax, ay, az := int32(math.Floor(arrival.X)), int32(arrival.Y), int32(math.Floor(arrival.Z))
	if state := srv.LoadBlockInDimension(world.NetherName, ax, ay, az); state != portalX && state != portalZ {
		t.Fatalf("the arrival cell is block %d, want a nether portal's", state)
	}
	// Every portal block within the blast's reach of the arrival.
	type cell struct{ x, y, z int32 }
	var portal []cell
	for x := ax - 3; x <= ax+3; x++ {
		for y := ay - 3; y <= ay+3; y++ {
			for z := az - 3; z <= az+3; z++ {
				if st := srv.LoadBlockInDimension(world.NetherName, x, y, z); st == portalX || st == portalZ {
					portal = append(portal, cell{x, y, z})
				}
			}
		}
	}

	// The fuse runs out in the nether, the body gone from every world, and
	// the blast — a body's that used a portal — breaks no portal block.
	// vanilla: PrimedTnt.USED_PORTAL_DAMAGE_CALCULATOR
	servertest.WaitFor(t, srv, 400, func() bool { return srv.TNTCount() == 0 })
	servertest.RunTicks(t, srv, 2)
	for _, p := range portal {
		if st := srv.LoadBlockInDimension(world.NetherName, p.x, p.y, p.z); st != portalX && st != portalZ {
			t.Errorf("arrival portal block at (%d, %d, %d) = %d after the blast, want a nether portal's",
				p.x, p.y, p.z, st)
		}
	}

	// And the overworld never heard of it: the portal the body left
	// through still stands, unblasted.
	for _, interior := range servertest.FrameInterior() {
		if got := srv.BlockAt(interior[0], interior[1], interior[2]); got != portalX {
			t.Errorf("overworld portal block at (%d, %d, %d) = %d, want the portal's %d",
				interior[0], interior[1], interior[2], got, portalX)
		}
	}
}

// TestAMobGoesThroughAPortal checks the crossing a herd's body makes: a pig
// stood inside a lit portal leaves the overworld and stands in the nether,
// the dimension it belongs to having changed with it.
func TestAMobGoesThroughAPortal(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Shepherd")

	_, _, _, _ = servertest.PortalStates(t)
	servertest.LightPortal(t, srv, c)

	// A pig stood inside the portal's bottom interior cell, its feet in
	// the portal the way a player's would be.
	if id := srv.SpawnMob("minecraft:pig", float64(servertest.PortalInnerX0)+0.5,
		float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5); id == 0 {
		t.Fatal("spawning the pig failed")
	}

	// The crossing: the pig leaves the overworld and stands in the nether.
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.MobsInDimension(world.NetherName)) == 1 &&
			len(srv.MobsInDimension(world.DimensionName)) == 0
	})
	mobs := srv.MobsInDimension(world.NetherName)
	if mobs[0].Kind != "minecraft:pig" {
		t.Errorf("the arrival is a %q, want the pig that went in", mobs[0].Kind)
	}
}

// vanilla: Entity.teleportCrossDimension, Entity.canUsePortal
func TestARiderCrossingAPortalTakesTheirHorseAlong(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"horse", "creative", "camel", "stack"} {
		t.Run(kind, func(t *testing.T) {
			cfg := servertest.CrossingTestConfig(t)
			srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Crosser"))
			c := connect().Join("Crosser")
			servertest.LightPortal(t, srv, c)
			clients := []*servertest.Client{c}
			names := map[*servertest.Client]string{c: "Crosser"}
			if kind == "camel" {
				second := connect().Join("Second")
				clients = append(clients, second)
				names[second] = "Second"
			}
			servertest.HoldTicks(t, srv)
			x, y, z := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5
			mount := "horse"
			if kind == "camel" {
				mount = "camel"
			}
			c.ChatCommand(fmt.Sprintf(`summon %s %v %v %v {Tame:1b,NoAI:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`, mount, x, y, z))
			c.Settle()
			old := srv.MobsInDimension(world.DimensionName)[0]
			if kind == "stack" {
				c.ChatCommand(fmt.Sprintf("summon pig %v %v %v {NoAI:1b}", x, y, z))
				c.ChatCommand("ride @e[type=horse,limit=1] mount @e[type=pig,limit=1]")
				c.Settle()
			}
			for i, rider := range clients {
				mode := int32(protocol.GameModeSurvival)
				if kind == "creative" {
					mode = protocol.GameModeCreative
				}
				srv.SetPlayerGameMode(names[rider], mode)
				rider.MoveTo(x, y, z+1.5, 0, 0)
				rider.Settle()
				if kind == "stack" {
					rider.ChatCommand("ride @s mount @e[type=horse,limit=1]")
				} else {
					rider.UseOn(old.ID)
				}
				var ids []int32
				rider.SettleAndCollect(func(id int32, data []byte) {
					if id == protocol.IDPlaySetPassengers {
						vehicle, passengers := servertest.ReadPassengers(rider, data)
						if vehicle == old.ID {
							ids = passengers
						}
					}
				})
				if len(ids) != i+1 || !slices.Contains(ids, rider.PlayerEntityID) {
					t.Fatalf("passengers before crossing = %v, want player %d aboard", ids, rider.PlayerEntityID)
				}
			}
			// Survival players need 80 ticks alone; the mount crosses immediately.
			servertest.UntilTicks(t, srv, 5, "the mounted players to cross", func() bool {
				for _, rider := range clients {
					if srv.PlayerDimension(names[rider]) != world.NetherName {
						return false
					}
				}
				return true
			})
			if left := srv.MobsInDimension(world.DimensionName); len(left) != 0 {
				t.Fatalf("mobs left behind = %+v", left)
			}
			var twin server.TestMob
			for _, m := range srv.MobsInDimension(world.NetherName) {
				if m.UUID == old.UUID {
					twin = m
				}
			}
			if twin.ID == 0 || twin.ID == old.ID {
				t.Fatalf("arrival = %+v, want a new body for %s", twin, old.UUID)
			}
			if kind == "stack" && twin.Vehicle == 0 {
				t.Fatal("the horse lost its pig vehicle")
			}
			for _, rider := range clients {
				seated, sounded := false, false
				if !rider.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
					switch id {
					case protocol.IDPlaySetPassengers:
						vehicle, ids := servertest.ReadPassengers(rider, data)
						if vehicle == twin.ID {
							seated = slices.Contains(ids, rider.PlayerEntityID) && len(ids) == len(clients)
						}
					case protocol.IDPlayWorldEvent:
						sounded = protocol.NewReader(data).ReadI32() == protocol.WorldEventPortalTravel
					}
					return seated && sounded
				}) {
					t.Fatalf("arrival seated = %v, portal sound = %v", seated, sounded)
				}
			}
			servertest.RunTicks(t, srv, 85)
			for _, rider := range clients {
				if got := srv.PlayerDimension(names[rider]); got != world.NetherName {
					t.Errorf("dimension after arrival = %q", got)
				}
			}
		})
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

// TestAStuckArrowCrossesBackAfterTheProjectileCooldown pins the projectile's
// dimension-changing delay: an arrow that crosses a portal carries a 2-tick
// cooldown (vanilla: Projectile.getDimensionChangingDelay), not the 300 a
// plain entity carries, so a few ticks out of the arrival portal let it
// cross back where 300 would hold it for fifteen seconds.
func TestAStuckArrowCrossesBackAfterTheProjectileCooldown(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, _ := servertest.Join(t, cfg, "Archer")

	servertest.PortalFrame(t, srv)
	// No player stands in the nether, so the chunks about the arrival are
	// held entity-ticking.
	for _, z := range []int32{-16, 0, 16} {
		t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, z))
	}
	// A stuck shaft in the portal's bottom interior cell: it crosses at
	// once, and stands in the arrival portal where its cooldown runs.
	if id := srv.SpawnStuckArrow("disallowed", float64(servertest.PortalInnerX0)+0.5,
		float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5); id == 0 {
		t.Fatal("spawning the stuck arrow failed")
	}
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ArrowsInDimension(world.NetherName)) == 1 &&
			len(srv.ArrowsInDimension(world.DimensionName)) == 0
	})
	arrival := srv.ArrowsInDimension(world.NetherName)[0]

	// Out of the arrival portal for a few ticks, long enough for the
	// projectile's 2-tick cooldown to run out, then back in: the shaft
	// crosses back within a tick or two.
	srv.MoveArrow(arrival.ID, arrival.X, arrival.Y, arrival.Z+2)
	servertest.AwaitTicks(t, srv, 3)
	srv.MoveArrow(arrival.ID, arrival.X, arrival.Y, arrival.Z)
	servertest.WaitFor(t, srv, 100, func() bool {
		return len(srv.ArrowsInDimension(world.DimensionName)) == 1
	})
}

// TestATridentThroughAPortalLosesItsGlint pins that a portal's copy of a
// trident is raised from the entity tag, which keeps the enchanted stack but
// not the synced glint: the copy is not shown glinting.
// vanilla: Entity.restoreFrom, ThrownTrident.readAdditionalSaveData
func TestATridentThroughAPortalLosesItsGlint(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, _ := servertest.Join(t, cfg, "Thrower")

	servertest.PortalFrame(t, srv)
	// Beside the portal first: the enchanted stack's glint is told.
	x, y, z := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5
	id := srv.SpawnStuckTrident(x, y, z+3, 1, 0)
	if id == 0 {
		t.Fatal("spawning the stuck trident failed")
	}
	before := srv.ArrowsInDimension(world.DimensionName)
	if len(before) != 1 || !before[0].Foil {
		t.Fatalf("the enchanted trident's glint = %+v, want shown", before)
	}
	srv.MoveArrow(id, x, y, z)
	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.ArrowsInDimension(world.NetherName)) == 1 &&
			len(srv.ArrowsInDimension(world.DimensionName)) == 0
	})
	if arrival := srv.ArrowsInDimension(world.NetherName)[0]; arrival.Foil {
		t.Error("the trident's copy in the Nether shows the glint, want it unset")
	}
}

// TestAnEmptyVehicleInAPortalTravelsToTheNether checks an empty minecart, an
// empty boat and an armor stand stood inside a lit portal: each is in the
// nether within the portal's wait (none for a body that is no player) and a
// few ticks, and gone from the overworld. The portal is turned to run along
// Z while the exit portal stood in the nether runs along X (one built for the
// crossing would take the source's axis), so each arrives with its yaw of 0
// turned by 90.
// vanilla: NetherPortalBlock.getPortalTransitionTime, Entity.handlePortal,
// NetherPortalBlock.getDimensionTransitionFromExit
func TestAnEmptyVehicleInAPortalTravelsToTheNether(t *testing.T) {
	t.Parallel()
	x, z := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalInnerZ)+0.5
	for _, tc := range []struct {
		name  string
		spawn func(srv *server.Server) int32
		yaws  func(srv *server.Server, dimension string) []float32
	}{
		{"minecart", func(srv *server.Server) int32 {
			return srv.SpawnMinecart(x, float64(servertest.PortalFloorY), z)
		}, func(srv *server.Server, dimension string) []float32 {
			var out []float32
			for _, m := range srv.MinecartsInDimension(dimension) {
				out = append(out, m.Yaw)
			}
			return out
		}},
		{"boat", func(srv *server.Server) int32 {
			return srv.SpawnBoat(x, float64(servertest.PortalFloorY), z, "minecraft:oak_boat")
		}, (*server.Server).BoatYawsInDimension},
		{"armor stand", func(srv *server.Server) int32 {
			return srv.SpawnArmorStand(x, float64(servertest.PortalFloorY), z)
		}, func(srv *server.Server, dimension string) []float32 {
			var out []float32
			for _, a := range srv.ArmorStandsInDimension(dimension) {
				out = append(out, a.Yaw)
			}
			return out
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.CrossingTestConfig(t)
			srv, c := servertest.Join(t, cfg, "Carter")
			servertest.LightPortal(t, srv, c)
			_, portalX, portalZ, _ := servertest.PortalStates(t)
			for _, cell := range servertest.FrameInterior() {
				if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != portalX {
					t.Fatalf("portal cell %v = %d, want the lit portal along X", cell, got)
				}
				srv.SetBlock(cell[0], cell[1], cell[2], portalZ)
			}
			// The arrival's search is centred on (0, PortalFloorY, 0): the
			// interior scaled by 1/8.
			t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
			for dy := range int32(2) {
				if !srv.SetBlockInDimension(world.NetherName, 0, servertest.PortalFloorY+dy, 0, portalX) {
					t.Fatal("placing the nether's exit portal failed")
				}
			}
			servertest.HoldTicks(t, srv)

			if id := tc.spawn(srv); id == 0 {
				t.Fatalf("spawning the %s failed", tc.name)
			}
			var nether []float32
			servertest.UntilTicks(t, srv, 5, "the "+tc.name+" in the nether and gone from the overworld", func() bool {
				nether = tc.yaws(srv, world.NetherName)
				return len(nether) == 1 && len(tc.yaws(srv, world.DimensionName)) == 0
			})
			if nether[0] != 90 {
				t.Errorf("the %s arrived facing yaw %v, want 90 (0 turned between a Z and an X portal)", tc.name, nether[0])
			}
		})
	}
}

// TestAMobRiddenVehicleCarriesItsRiderThroughAPortal: a boat or a minecart
// with a mob aboard, in a lit nether portal, crosses and takes the mob along,
// seated again on arrival; neither is left in the overworld.
// vanilla: Entity.teleportCrossDimension
func TestAMobRiddenVehicleCarriesItsRiderThroughAPortal(t *testing.T) {
	t.Parallel()
	x, z := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalInnerZ)+0.5
	for _, tc := range []struct {
		name   string
		spawn  func(srv *server.Server) int32
		mount  func(srv *server.Server, mob, vehicle int32) bool
		riders func(srv *server.Server, dimension string) [][]string
	}{
		{"boat", func(srv *server.Server) int32 {
			return srv.SpawnBoat(x, float64(servertest.PortalFloorY), z, "minecraft:oak_boat")
		}, (*server.Server).MountMobOnBoat, (*server.Server).BoatMobRidersInDimension},
		{"minecart", func(srv *server.Server) int32 {
			return srv.SpawnMinecart(x, float64(servertest.PortalFloorY), z)
		}, (*server.Server).MountMobOnMinecart, (*server.Server).MinecartMobRidersInDimension},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.CrossingTestConfig(t)
			srv, c := servertest.Join(t, cfg, "Ferry")
			servertest.LightPortal(t, srv, c)
			servertest.HoldTicks(t, srv)

			vehicleID := tc.spawn(srv)
			pigID := srv.SpawnMob("minecraft:pig", x, float64(servertest.PortalFloorY), z)
			if vehicleID == 0 || pigID == 0 {
				t.Fatalf("spawning the %s or the pig failed", tc.name)
			}
			if !tc.mount(srv, pigID, vehicleID) {
				t.Fatalf("the pig was not seated in the %s", tc.name)
			}
			pigs := func(dimension string) int {
				n := 0
				for _, m := range srv.MobsInDimension(dimension) {
					if m.Kind == "minecraft:pig" {
						n++
					}
				}
				return n
			}
			var nether [][]string
			servertest.UntilTicks(t, srv, 5, "the "+tc.name+" and its pig in the nether and gone from the overworld", func() bool {
				nether = tc.riders(srv, world.NetherName)
				return len(nether) == 1 && len(tc.riders(srv, world.DimensionName)) == 0 &&
					pigs(world.DimensionName) == 0 && pigs(world.NetherName) == 1
			})
			if !slices.Equal(nether[0], []string{"minecraft:pig"}) {
				t.Errorf("the %s in the nether carries %v, want the pig still riding", tc.name, nether[0])
			}
		})
	}
}

// TestAPlayerCrossingAPortalTakesTheirBoatAlong: a player in a boat in a lit
// nether portal crosses with the boat, still seated, and the pig beside them
// in the boat comes too; nothing is left in the overworld.
// vanilla: Entity.teleportCrossDimension
func TestAPlayerCrossingAPortalTakesTheirBoatAlong(t *testing.T) {
	t.Parallel()
	x, z := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalInnerZ)+0.5
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Rower")
	servertest.LightPortal(t, srv, c)
	c.MoveTo(x, servertest.PortalFloorY, z+1, 0, 0)
	c.Settle()

	// Boarded with the loop stopped, so the portal takes neither the rider
	// nor the boat mid-setup.
	srv.StopTick()
	boatID := srv.SpawnBoat(x, float64(servertest.PortalFloorY), z, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	if got, want := srv.PlayerPosition("Rower").Y, float64(servertest.PortalFloorY)+0.1875-0.6; math.Abs(got-want) > 1e-6 {
		t.Fatalf("rider Y = %g, want %g below the portal floor", got, want)
	}
	pigID := srv.SpawnMob("minecraft:pig", x, float64(servertest.PortalFloorY), z)
	if pigID == 0 || !srv.MountMobOnBoat(pigID, boatID) {
		t.Fatal("the pig was not seated in the boat")
	}
	// The hull's Y chooses the upper exit cell; the passenger's lowered
	// feet would choose the lower one. vanilla: NetherPortalBlock.getPortalDestination
	defer srv.HoldChunkInDimension(world.NetherName, 0, 0)()
	for _, y := range []int32{servertest.PortalFloorY - 2, servertest.PortalFloorY} {
		if !srv.SetBlockInDimension(world.NetherName, 0, y, 0, rules.NetherPortalX) {
			t.Fatal("could not place the destination portal")
		}
	}
	srv.StartTick()

	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the riding crossing")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived around the crossing")
	}
	if got := srv.PlayerDimension("Rower"); got != world.NetherName {
		t.Fatalf("dimension after the crossing = %q, want the nether", got)
	}
	servertest.WaitFor(t, srv, 5, func() bool {
		riders := srv.BoatMobRidersInDimension(world.NetherName)
		return len(riders) == 1 && slices.Equal(riders[0], []string{"minecraft:pig"})
	})
	if rider, ok := srv.BoatRider(boatID); !ok || rider != "Rower" {
		t.Errorf("the boat's rider = %q (%v), want Rower still aboard", rider, ok)
	}
	if _, y, _, _, _, ok := srv.BoatState(boatID); !ok || y != servertest.PortalFloorY {
		t.Errorf("arrival hull Y = %g (%v), want %d", y, ok, servertest.PortalFloorY)
	}
	if boats := srv.BoatMobRidersInDimension(world.DimensionName); len(boats) != 0 {
		t.Errorf("boats left in the overworld = %v, want none", boats)
	}
}

// TestAWitherStaysOutOfAPortal stands a NoAI wither and a pig in a lit
// portal: the pig crosses, the wither may never use a portal and stays.
// vanilla: WitherBoss.canUsePortal
func TestAWitherStaysOutOfAPortal(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Lurer")
	servertest.LightPortal(t, srv, c)
	servertest.HoldTicks(t, srv)

	y, z := float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5
	wither := srv.SpawnMob("minecraft:wither", float64(servertest.PortalInnerX0)+0.5, y, z)
	pig := srv.SpawnMob("minecraft:pig", float64(servertest.PortalInnerX1)+0.5, y, z)
	if wither == 0 || pig == 0 {
		t.Fatal("spawning the wither or the pig failed")
	}
	srv.SetMobNoAI(wither)
	srv.SetMobNoAI(pig)

	servertest.UntilTicks(t, srv, 5, "the pig to cross", func() bool {
		return slices.ContainsFunc(srv.MobsInDimension(world.NetherName), func(m server.TestMob) bool { return m.Kind == "minecraft:pig" })
	})
	servertest.RunTicks(t, srv, 5)
	left := srv.MobsInDimension(world.DimensionName)
	if len(left) != 1 || left[0].Kind != "minecraft:wither" {
		t.Errorf("overworld mobs = %+v, want the wither alone", left)
	}
	if got := srv.MobsInDimension(world.NetherName); len(got) != 1 || got[0].Kind != "minecraft:pig" {
		t.Errorf("nether mobs = %+v, want the pig alone", got)
	}
}

// vanilla: Entity.teleportSpectators
func TestASpectatorWatchingAMobIsCarriedThroughItsPortal(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Ghost", servertest.OpsListing(t, "Ghost"))
	servertest.LightPortal(t, srv, c)
	c.ChatCommand("gamemode spectator")
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerGameMode("Ghost") == 3 })

	if id := srv.SpawnMob("minecraft:pig", float64(servertest.PortalInnerX0)+0.5,
		float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5); id == 0 {
		t.Fatal("spawning the pig failed")
	}
	c.ChatCommand("spectate @e[type=pig,limit=1]")

	servertest.WaitFor(t, srv, 400, func() bool {
		return len(srv.MobsInDimension(world.NetherName)) == 1
	})
	servertest.AwaitTicks(t, srv, 5)
	if got := srv.PlayerDimension("Ghost"); got != world.NetherName {
		t.Errorf("the spectator is in %q, want %q with the pig it watches", got, world.NetherName)
	}
}
