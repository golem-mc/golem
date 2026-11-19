package explosions

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
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

// TestABlastPushesThePrimedTNTPosedBesideIt checks the chain's own shove: a
// primed body standing six blocks from another's blast is not hurt — a
// vanilla one refuses every hurt — but takes the push, its own motion check
// hearing of the velocity the tick after the blast and telling whoever
// watches.
func TestABlastPushesThePrimedTNTPosedBesideIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig() // creative, so nothing else is keeping drops

	srv, c := servertest.Join(t, cfg, "Chainer")

	// The first body at the origin, the second six blocks east: inside the
	// double radius of the blast the first will make, and far enough out
	// that the blast's rays cannot reach the ground under the second.
	servertest.HoldTicks(t, srv)
	first := srv.SpawnTNT(0.5, 64, 0.5, 20)
	second := srv.SpawnTNT(6.5, 64, 0.5, 600)
	if first == 0 || second == 0 {
		t.Fatal("SpawnTNT came to nothing")
	}

	w := newBlastWatch()
	c.ReadTicks(srv, 300, func(gotID int32, data []byte) bool {
		w.readBlastPacket(c, gotID, data)
		return w.boomSeen && w.motions[second] != [3]float64{}
	})
	if !w.boomSeen {
		t.Fatal("the first body never went up")
	}
	if w.boom.X != 0.5 || w.boom.Z != 0.5 {
		t.Errorf("the blast stood at (%v %v %v), want the first body's own column", w.boom.X, w.boom.Y, w.boom.Z)
	}
	if w.removed[second] {
		t.Error("the pushed body was taken out of the world, its fuse still burning")
	}
	push, ok := w.motions[second]
	if !ok {
		t.Fatal("the primed body beside the blast was never pushed")
	}
	// The push runs away from the blast, which stood six blocks west: a
	// quarter of the falloff's whole before the physics of the tick after
	// takes its share, so the shove the packet carries is eastward and a
	// good slice of a block a tick.
	if push[0] < 0.1 {
		t.Errorf("the pushed body's shove = (%v %v %v), want the eastward push away from the blast", push[0], push[1], push[2])
	}
	if n := srv.TNTCount(); n != 1 {
		t.Errorf("primed bodies left in the world = %d, want the pushed one alone", n)
	}
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

// TestABoatSurvivesACreeperBlastWithMobGriefingOff: the one ignore
// VehicleEntity.ignoreExplosion names is a mob's blast with mob_griefing off,
// where the hull rides the whole blast out.
func TestABoatSurvivesACreeperBlastWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Bystander")
	srv.SetGameRule("mob_griefing", false)

	if srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat") == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	if srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5) == 0 {
		t.Fatal("the creeper never stood up")
	}

	exploded := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.BlastSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlayExplosion {
			exploded = true
		}
		return exploded
	})
	if !exploded {
		t.Fatal("the creeper never exploded")
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing after a mob blast with mob_griefing off = %d, want the one spared", got)
	}
}

// TestASilentCreeperBlastsWithoutItsSound checks the Explosion packet's
// playSound flag follows the creeper's Silent tag.
// vanilla: ServerLevel.explode
func TestASilentCreeperBlastsWithoutItsSound(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Bystander")
	id := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	if id == 0 || !srv.SetMobSilent(id, true) {
		t.Fatal("the creeper never stood up")
	}
	var data []byte
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.BlastSkip, func(pid int32, d []byte) bool {
		if pid == protocol.IDPlayExplosion {
			data = d
		}
		return data != nil
	})
	if data == nil {
		t.Fatal("the creeper never exploded")
	}
	if servertest.ReadExplosion(c, data).PlaySound {
		t.Error("playSound = true, want false for a silent creeper")
	}
}

// TestACreativeBlastLeavesNoBoatBehind: a blast a creative player lit
// discards the hull it reaches, the item the break would leave skipped.
// vanilla: VehicleEntity.hurtServer
func TestACreativeBlastLeavesNoBoatBehind(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Artist") // creative

	if srv.SpawnBoat(10.5, 64, 10.5, "minecraft:oak_boat") == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	if srv.SpawnPlayerTNT("Artist", 11.5, 64, 10.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}

	servertest.WaitFor(t, srv, 200, func() bool { return srv.BoatCount() == 0 })
	if blastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:oak_boat") {
		t.Error("a creative blast's wreck left a boat item behind")
	}
}

// TestABlastBreaksOnlyTheBoatOfItsOwnDimension: a nether blast at a hull's
// own column destroys the nether hull alone, the overworld body at the same
// coordinates riding it out.
func TestABlastBreaksOnlyTheBoatOfItsOwnDimension(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")

	release := srv.HoldChunkInDimension(world.NetherName, 0, 0)
	defer release()

	// A pocket of air in the nether's netherrack, so the blast measures
	// the hull rather than a wall's worth of exposure blocking it.
	for x := int32(7); x <= 14; x++ {
		for y := int32(61); y <= 68; y++ {
			for z := int32(7); z <= 14; z++ {
				srv.SetBlockInDimension(world.NetherName, x, y, z, 0)
			}
		}
	}

	overworldID := srv.SpawnBoat(10.5, 64, 10.5, "minecraft:oak_boat")
	netherID := srv.SpawnBoatInDimension(world.NetherName, 10.5, 64, 10.5, "minecraft:oak_boat")
	if overworldID == 0 || netherID == 0 {
		t.Fatal("a boat never stood up")
	}
	if srv.SpawnTNTInDimension(world.NetherName, 11.5, 64, 10.5, 1) == 0 {
		t.Fatal("the nether body never stood up")
	}

	servertest.WaitFor(t, srv, 200, func() bool {
		ids := srv.BoatIDs()
		return len(ids) == 1 && ids[0] == overworldID
	})
	if ids := srv.BoatIDs(); len(ids) != 1 || ids[0] != overworldID {
		t.Errorf("boats standing = %v, want the overworld one %d alone", ids, overworldID)
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

// TestABlastWithEntityDropsOffLeavesNoBoatItem: the entity_drops rule
// reaching a blast's wreck gates the boat item alone: the hull is
// destroyed with no item left, while the cargo a chest boat carried
// still spills with the removal.
// vanilla: VehicleEntity.destroy, AbstractChestBoat.remove
func TestABlastWithEntityDropsOffLeavesNoBoatItem(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.SetGameRule("entity_drops", false)

	boatID := srv.SpawnBoat(10.5, 64, 10.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.StowBoatCargo(boatID, 0, protocol.ItemStack{ItemID: mustItemID(t, "minecraft:diamond"), Count: 3})
	if srv.SpawnTNT(11.5, 64, 10.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}

	servertest.WaitFor(t, srv, 200, func() bool {
		items := srv.ItemsInDimension(world.DimensionName)
		return srv.BoatCount() == 0 && servertest.ItemCountOf(items, "minecraft:diamond") == 3
	})
	items := srv.ItemsInDimension(world.DimensionName)
	if blastLeftItem(items, "minecraft:oak_chest_boat") {
		t.Error("a wreck under entity_drops off left its boat item behind")
	}
	if got := servertest.ItemCountOf(items, "minecraft:diamond"); got != 3 {
		t.Errorf("a wreck under entity_drops off spilled %d diamonds, want the 3 it carried", got)
	}
}

// TestABlastAimsItsPushFromASneakingPlayersLoweredEyes: the push a blast
// gives a player points from the blast centre to the player's eyes at the
// pose's own eye height, 1.27 crouching, so a blast two blocks to the side
// at foot level pushes up by 1.27 for every 2 across.
// vanilla: ServerExplosion.hurtEntities (getEyePosition)
func TestABlastAimsItsPushFromASneakingPlayersLoweredEyes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Croucher")

	c.MoveTo(0.5, 64, 0.5, 0, 0)
	c.SendSneak(true)
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSneaking {
		t.Fatalf("pose = %d, want %d (sneaking)", got.Pose, protocol.PoseSneaking)
	}
	c.Settle()

	srv.BlastAt(2.5, 64, 0.5, 2)
	var boom servertest.Explosion
	seen := c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		if id != protocol.IDPlayExplosion {
			return false
		}
		boom = servertest.ReadExplosion(c, data)
		return true
	})
	if !seen {
		t.Fatal("the blast sent no Explosion packet")
	}
	if !boom.Pushed {
		t.Fatal("the Explosion packet carried no push for the player")
	}
	horizontal := math.Hypot(boom.KX, boom.KZ)
	if horizontal == 0 {
		t.Fatalf("the push %v,%v,%v has no horizontal part", boom.KX, boom.KY, boom.KZ)
	}
	if ratio := boom.KY / horizontal; math.Abs(ratio-1.27/2) > 1e-3 {
		t.Errorf("push Y/horizontal = %.4f, want %.4f (crouching eyes, not standing %.4f)", ratio, 1.27/2, 1.62/2)
	}
}

// TestAnItemSurvivesAMobBlastWithMobGriefingOff: an item entity ignores a
// mob's blast with mob_griefing off, so it is neither destroyed nor moved;
// with the rule on the same blast destroys it.
// vanilla: ItemEntity.ignoreExplosion
func TestAnItemSurvivesAMobBlastWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	for _, griefing := range []bool{false, true} {
		srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
		servertest.HoldTicks(t, srv)
		srv.SetGameRule("mob_griefing", griefing)
		if srv.SpawnItemEntityDelay(2.5, 64, 0.5, "minecraft:stone", 1, 0) == 0 {
			t.Fatal("SpawnItemEntityDelay came to nothing")
		}
		servertest.RunTicks(t, srv, 60) // the item pops out of the surface and slides to rest
		before := srv.ItemsInDimension("minecraft:overworld")
		srv.BlastAtMob(0.5, 64, 0.5, 3)
		servertest.RunTicks(t, srv, 1)
		after := srv.ItemsInDimension("minecraft:overworld")
		if griefing {
			if n := srv.ItemCount(); n != 0 {
				t.Errorf("items after a blast with mob_griefing on = %d, want 0", n)
			}
			continue
		}
		if srv.ItemCount() != 1 || len(before) != 1 || len(after) != 1 {
			t.Fatalf("items = %d, want the one spared", srv.ItemCount())
		}
		b, a := before[0], after[0]
		// A push is tenths of a block; the resting slide is under a hundredth.
		if math.Abs(a.X-b.X) > 0.01 || math.Abs(a.Y-b.Y) > 0.01 || math.Abs(a.Z-b.Z) > 0.01 ||
			math.Abs(a.VX-b.VX) > 0.01 || math.Abs(a.VZ-b.VZ) > 0.01 {
			t.Errorf("item moved: before %+v after %+v", b, a)
		}
	}
}

// TestAWindChargeBurstLeavesItemsWhereTheyLie: a wind charge's burst neither
// pushes an item nor tells the client of a push.
// vanilla: ServerExplosion.shouldAffectBlocklikeEntities
func TestAWindChargeBurstLeavesItemsWhereTheyLie(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnItemEntityDelay(10.5, 64, 10.5, "minecraft:stone", 1, 32767)
	if id == 0 {
		t.Fatal("SpawnItemEntityDelay came to nothing")
	}
	// Reading the settling ticks drains the motion the slide sent.
	c.ReadTicks(srv, 60, func(int32, []byte) bool { return false })
	before := srv.ItemsInDimension("minecraft:overworld")
	srv.RunWindBurst(before[0].X-1, before[0].Y, before[0].Z)
	c.ReadTicks(srv, 2, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySetEntityMotion && metadataEntityID(data) == id {
			r := protocol.NewReader(data)
			r.ReadVarInt()
			vx, vy, vz := r.ReadLpVec3()
			// The resting item's own gravity tick sends a vertical-only motion;
			// a burst's push would carry a horizontal part.
			if vx != 0 || vz != 0 {
				t.Errorf("the item was pushed (%.3f, %.3f, %.3f) after a wind burst", vx, vy, vz)
			}
		}
		return false
	})
	after := srv.ItemsInDimension("minecraft:overworld")
	if len(before) != 1 || len(after) != 1 || math.Abs(before[0].X-after[0].X) > 0.01 || math.Abs(before[0].Z-after[0].Z) > 0.01 {
		t.Errorf("item moved: before %+v after %+v", before, after)
	}
}

// TestABlastAimsItsPushAtALargeFireballsEye: a blast level with a resting
// large fireball, three blocks to the side, pushes it along the line to the
// eye 0.85 of its one-block height above its position.
// vanilla: ServerExplosion.hurtEntities, EntityDimensions.defaultEyeHeight
func TestABlastAimsItsPushAtALargeFireballsEye(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Eyer")

	servertest.HoldTicks(t, srv)
	id := srv.LaunchFireball(entity.ThrownLargeFireball, 3.5, 66, 0.5, 0, 0, 0, 0, 1)
	if id == 0 {
		t.Fatal("LaunchFireball came to nothing")
	}
	srv.BlastAt(0.5, 66, 0.5, 2)
	_, _, vx, vy, vz, ok := srv.ThrownTagOf(id)
	if !ok {
		t.Fatal("the fireball is gone after the blast")
	}
	horizontal := math.Hypot(vx, vz)
	if horizontal == 0 || vx < 0 {
		t.Fatalf("the push %v,%v,%v does not run east, away from the blast", vx, vy, vz)
	}
	want := float64(float32(0.85)) / 3
	if ratio := vy / horizontal; math.Abs(ratio-want) > 1e-6 {
		t.Errorf("push Y/horizontal = %.6f, want %.6f (an eye 0.85 above the fireball, not 0.2)", ratio, want)
	}
}
