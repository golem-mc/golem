package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The blast's reach over the things that are not players and not mobs: the
// item entities, the experience orbs and the primed TNT a blast catches in
// its double radius. Vanilla's hurtEntities measures every one of them —
// the exposure of the body's own box, the damage against the health the
// burns spend, the stack's damage_resistant component standing between an
// item and the blast — and pushes every one of them that does not ignore
// the explosion, whichever kind it is. The tests drive all of it through a
// real connection, in the flat world the harness grows, against a summoned
// body's own fuse.

// blastWatch is what one watching client has been told about the bodies a
// blast reached: which entity IDs were removed from the world, and the
// velocity each Set Entity Motion packet carried, by the entity it named.
type blastWatch struct {
	removed  map[int32]bool
	motions  map[int32][3]float64
	boomSeen bool
	boom     servertest.Explosion
}

// newBlastWatch starts an empty watch.
func newBlastWatch() *blastWatch {
	return &blastWatch{removed: map[int32]bool{}, motions: map[int32][3]float64{}}
}

// readBlastPacket files one packet against the watch: the removals, the
// motions and the blast itself.
func (w *blastWatch) readBlastPacket(c *servertest.Client, id int32, data []byte) {
	switch id {
	case protocol.IDPlayRemoveEntities:
		r := protocol.NewReader(data)
		for range r.ReadVarInt() {
			w.removed[r.ReadVarInt()] = true
		}
	case protocol.IDPlaySetEntityMotion:
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		vx, vy, vz := r.ReadLpVec3()
		w.motions[entityID] = [3]float64{vx, vy, vz}
	case protocol.IDPlayExplosion:
		w.boom = servertest.ReadExplosion(c, data)
		w.boomSeen = true
	}
}

// TestAnItemBesideABlastIsDestroyedWhileANetherStarSurvivesPushed walks the
// blast's reach over the things lying near it: a stack of stone and an
// experience orb six blocks out are destroyed — the vanilla hurt measuring
// their boxes and spending their health — while the nether star beside them
// survives, its stack's damage_resistant component naming the servertest.Explosion
// damage types, and takes the push every non-ignoring body takes, away from
// the blast.
func TestAnItemBesideABlastIsDestroyedWhileANetherStarSurvivesPushed(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig() // creative, so nothing else is keeping drops

	// The summons the test lays its bodies down with are commands, which
	// need an operator: Watcher is listed.
	srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"))

	// The blast at the origin of the flat world's surface; the things it
	// reaches six blocks out along it and one step off the line — inside
	// the double radius of eight, and far enough out that the rays the
	// blast fires level with the surface cannot chew the ground any of
	// them stands on. The watcher stands twelve blocks south, out of the
	// blast's own reach. The summons' own packets are still to arrive when
	// the body is lit, and the pump that follows reads them off the wire.
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:item 6.5 64 0.5 {Item:{id:"minecraft:stone",count:1}}`)
	c.ChatCommand(`summon minecraft:item -5.5 64 0.5 {Item:{id:"minecraft:nether_star",count:1}}`)
	c.ChatCommand("summon minecraft:experience_orb 6.5 64 1.5 {Value:1}")

	srv.SpawnTNT(0.5, 64, 0.5, 20)

	stoneID, starID, orbID := int32(0), int32(0), int32(0)
	// pendingItem is the item entity whose introducing metadata has not
	// been read yet: the stack that metadata carries is what tells a
	// summoned stone from the summoned star.
	pendingItem := int32(0)
	w := newBlastWatch()
	c.ReadTicks(srv, 300, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			entityID, kind := spawnEntityHead(data)
			if kind == protocol.EntityTypeItem {
				pendingItem = entityID
			}
			if kind == protocol.EntityTypeExperienceOrb {
				orbID = entityID
			}
		case protocol.IDPlayEntityMetadata:
			if pendingItem != 0 && metadataEntityID(data) == pendingItem {
				stack := itemStackMetadata(data)
				if !stack.Empty() && stack.ItemID == mustItemID(t, "minecraft:stone") {
					stoneID = pendingItem
				}
				if !stack.Empty() && stack.ItemID == mustItemID(t, "minecraft:nether_star") {
					starID = pendingItem
				}
				pendingItem = 0
			}
		}
		w.readBlastPacket(c, gotID, data)
		return stoneID != 0 && starID != 0 && orbID != 0 && w.boomSeen &&
			w.removed[stoneID] && w.removed[orbID] && w.motions[starID] != [3]float64{}
	})
	if stoneID == 0 || starID == 0 || orbID == 0 {
		t.Fatalf("the summoned bodies never arrived (stone %d star %d orb %d)", stoneID, starID, orbID)
	}
	if !w.boomSeen {
		t.Fatal("the body's fuse ran out without an Explosion packet")
	}
	if w.boom.Radius != 4.0 {
		t.Errorf("servertest.Explosion radius = %v, want the primed TNT's 4", w.boom.Radius)
	}
	if !w.removed[stoneID] {
		t.Error("the stone beside the blast was never destroyed")
	}
	if !w.removed[orbID] {
		t.Error("the experience orb beside the blast was never destroyed")
	}
	if w.removed[starID] {
		t.Error("the nether star was destroyed, its damage_resistant naming the servertest.Explosion types")
	}
	push, ok := w.motions[starID]
	if !ok {
		t.Fatal("the surviving nether star was never pushed")
	}
	// The push runs away from the blast, which stood six blocks east of
	// the star: the falloff at that distance is a quarter of the whole,
	// and the physics of the tick after it takes its share, so the shove
	// the packet carries is a good slice of a block a tick westward.
	if push[0] > -0.1 {
		t.Errorf("the nether star's push = (%v %v %v), want the westward shove away from the blast", push[0], push[1], push[2])
	}
	if n := srv.OrbCount(); n != 0 {
		t.Errorf("orbs left in the world = %d, want none", n)
	}
}

// mustItemID is the registry ID of the named item, failing the test when the
// registry does not know it.
func mustItemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

// metadataEntityID is the entity a Set Entity Metadata packet is about.
func metadataEntityID(data []byte) int32 {
	return protocol.NewReader(data).ReadVarInt()
}

// itemStackMetadata reads the item stack a Set Entity Metadata packet names,
// the field a spawned item's introducing packet carries; a packet about some
// other kind of body reads as the empty stack.
func itemStackMetadata(data []byte) protocol.ItemStack {
	r := protocol.NewReader(data)
	r.ReadVarInt() // the entity, which the stack's own arrival names
	stack := protocol.ItemStack{}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		if index == protocol.MetadataIndexItemSlot && fieldType == protocol.MetadataSlot {
			stack, _ = r.ReadSlot()
			continue
		}
		stepOverMetadataValue(r, fieldType)
	}
	return stack
}

// TestABlastInWaterBreaksNoBlocksAndHurtsThePlayerBeside checks the fluid's
// own share of a blast: a body whose fuse runs out inside a pool of water
// makes a blast whose rays die against the water's resistance where they
// stand — the fluid's hundred — so no block in the world moves, while the
// entities the double radius reaches are measured and hurt all the same:
// the player beside the pool through the pipeline, and the orb floating in
// the pool destroyed by the health the blast spends on it.
func TestABlastInWaterBreaksNoBlocksAndHurtsThePlayerBeside(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig() // survival, the mode that can be hurt

	srv, c, _ := servertest.JoinAs(t, cfg, "Bystander")

	// The one-deep pool the flat world's surface block makes way for, the
	// player three blocks east of it — close enough for the blast's double
	// radius to reach them, far enough that the ground's own lip stands
	// between, which is what leaves the damage the vanilla falloff's +1
	// floor. The orb goes into the pool, where it floats, and the primed
	// body sinks to the pool's floor to go up there.
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	if !srv.SetBlock(0, 63, 0, water) {
		t.Fatal("the pool's chunk is not loaded")
	}
	c.MoveTo(3.5, 64, 0.5, 0, 0)
	c.Settle()
	servertest.HoldTicks(t, srv)
	orbID := srv.SpawnOrb(0.5, 63.5, 0.5, 1)
	tntID := srv.SpawnTNT(0.5, 63.5, 0.5, 20)
	if orbID == 0 || tntID == 0 {
		t.Fatal("the pool's tenants never stood up")
	}

	// The window covers the summoning, the fuse and the blast's wake: any
	// Block Update packet at all in it is a block the blast moved, which
	// the water's resistance forbids.
	blockUpdates := 0
	exploded := false
	var boom servertest.Explosion
	hurt := false
	var health float32
	orbRemoved := false
	c.ReadTicks(srv, 300, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayBlockUpdate:
			blockUpdates++
		case protocol.IDPlayExplosion:
			boom = servertest.ReadExplosion(c, data)
			exploded = true
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 {
				hurt = true
				health = h.Health
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				if r.ReadVarInt() == orbID {
					orbRemoved = true
				}
			}
		}
		return exploded && hurt && orbRemoved
	})
	if !exploded {
		t.Fatal("the body's fuse ran out without an Explosion packet")
	}
	if boom.BlockCount != 0 {
		t.Errorf("the blast in water reported %d blocks destroyed, want none", boom.BlockCount)
	}
	if blockUpdates != 0 {
		t.Errorf("the blast in water moved %d blocks on the wire, want none", blockUpdates)
	}
	if !hurt {
		t.Fatal("the blast in water never hurt the player beside the pool")
	}
	if health != 19 {
		t.Errorf("the player's health after the blast = %v, want the falloff's own 19", health)
	}
	if !orbRemoved {
		t.Error("the orb floating in the pool was never destroyed")
	}
	if got := srv.BlockAt(0, 63, 0); got != water {
		t.Errorf("the pool's block = %d, want the water still standing", got)
	}
	if got := srv.BlockAt(1, 63, 0); got == 0 {
		t.Error("the pool's rim was broken, want the water's resistance to have stopped every ray")
	}
}

// blastLeftItem reports whether an item list holds a stack of the named item.
func blastLeftItem(items []server.TestItem, name string) bool {
	for _, it := range items {
		if it.Item == name {
			return true
		}
	}
	return false
}

// TestATNTBlastDestroysABoatAndLeavesItsItem: a hull in a primed body's
// reach is hurt by the blast, the tenfold of the explosion damage
// VehicleEntity.hurtServer carries past forty destroying it, and the boat
// tick takes the wreck out with the item its break leaves where it stood.
// vanilla: ServerExplosion.hurtEntities, VehicleEntity.hurtServer
func TestATNTBlastDestroysABoatAndLeavesItsItem(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")

	boatID := srv.SpawnBoat(10.5, 64, 10.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	if srv.SpawnTNT(11.5, 64, 10.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}

	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BoatCount() == 0 &&
			blastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:oak_boat")
	})
	if got := srv.BoatCount(); got != 0 {
		t.Fatalf("boats standing after the blast = %d, want none", got)
	}
	if !blastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:oak_boat") {
		t.Error("the wreck left no boat item behind")
	}
}

// TestABlastSpillsAChestBoatsCargo: a chest boat destroyed by a blast drops
// its own item and spills the stacks it carried, the way a broken container
// leaves its cargo.
func TestABlastSpillsAChestBoatsCargo(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")

	boatID := srv.SpawnBoat(10.5, 64, 10.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.StowBoatCargo(boatID, 0, protocol.ItemStack{ItemID: mustItemID(t, "minecraft:diamond"), Count: 3})
	if srv.SpawnTNT(11.5, 64, 10.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}

	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BoatCount() == 0 &&
			blastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:oak_chest_boat")
	})
	items := srv.ItemsInDimension(world.DimensionName)
	if !blastLeftItem(items, "minecraft:oak_chest_boat") {
		t.Error("the wreck left no chest boat item behind")
	}
	diamonds := 0
	for _, it := range items {
		if it.Item == "minecraft:diamond" {
			diamonds += int(it.Count)
		}
	}
	if diamonds != 3 {
		t.Errorf("diamonds dropped from the cargo = %d, want the three it carried", diamonds)
	}
}

// TestAWeakBlastShovesABoatAndRocksIt: a hull inside the blast's reach but
// outside the falloff that would break it — the damage left under the forty
// VehicleEntity.hurtServer breaks at — is shoved away from the blast on the
// velocity Entity.push adds, carried to a watcher on a boat-tick movement
// packet, and left standing with the rocking a blow winds.
// vanilla: ServerExplosion.hurtEntities, Entity.push, VehicleEntity.hurtServer
func TestAWeakBlastShovesABoatAndRocksIt(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	watcher.Settle()

	// The hull three and a half blocks out, the primed body seven and a half
	// east of it: inside the double radius of eight the hurt reaches, far
	// enough out that the falloff leaves the damage under forty, and clear
	// of the ground the blast chews.
	boatID := srv.SpawnBoat(3.5, 64, 0.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	if srv.SpawnTNT(11, 64, 0.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}

	servertest.WaitFor(t, srv, 200, func() bool {
		x, _, _, _, _, ok := srv.BoatState(boatID)
		return ok && x < 3.49
	})
	x, _, _, _, _, ok := srv.BoatState(boatID)
	if !ok {
		t.Fatal("the shoved hull left the world")
	}
	if x >= 3.49 {
		t.Errorf("the hull stands at x=%v, want it shoved west of where it stood", x)
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing after the weak blast = %d, want the one it spared", got)
	}
	// The shove rides the boat tick's own movement out to whoever watches.
	told := false
	watcher.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayTeleportEntity:
			if protocol.NewReader(data).ReadVarInt() == boatID {
				told = true
				return true
			}
		}
		return false
	})
	if !told {
		t.Error("no movement packet about the shoved hull reached the watcher")
	}
	// The blow's rocking: the ten ticks and the damage it laid.
	fields := servertest.AwaitBoatMetadata(watcher, boatID)
	if h, _ := fields[protocol.MetadataIndexVehicleShake].(int32); h <= 0 {
		t.Errorf("the hull's rocking = %v ticks, want the blow's own", fields[protocol.MetadataIndexVehicleShake])
	}
	if d, _ := fields[protocol.MetadataIndexVehicleShakeDamage].(float32); d <= 0 {
		t.Errorf("the hull's accumulated damage = %v, want the blow's own", fields[protocol.MetadataIndexVehicleShakeDamage])
	}
}
