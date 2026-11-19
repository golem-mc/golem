package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// The jockey tests: the riders vanilla's finalizes stand on a mob — a
// spider's one-in-a-hundred skeleton, a baby zombie's two five-per-hundred
// chickens — read through real connections where the Set Passengers link
// travels, and through the spawn pass whose packs the issue asks after. The
// seed SpawnMob takes from the position makes each lattice deterministic:
// the same rolls land every run.

// jockeyTestConfig is the configuration the jockey tests run in: survival,
// so that a swing the rider takes is a real blow.
func jockeyTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// huntAJockey spawns zombies across a lattice of seeded positions — beyond
// the chunks any watcher is sent, so the search draws no traffic — until
// one stands on the fresh chicken its own finalize hatched beneath it, and
// answers the pair. The main loop is stopped beforehand: what the lattice
// stands up stays planted.
func huntAJockey(t *testing.T, srv *server.Server, spawns int) (rider, vehicle int32) {
	t.Helper()
	for i := range spawns {
		x := 96 + float64(i%40)
		z := 8 + float64(i/40)
		id := srv.SpawnMob("minecraft:zombie", x, 64, z)
		if veh := srv.MobVehicleID(id); veh != 0 {
			return id, veh
		}
	}
	t.Fatalf("no baby zombie took a chicken jockey in %d spawns", spawns)
	return 0, 0
}

// mobKinds lists the live mobs by the entity IDs the packets address them by.
func mobKinds(mobs []server.TestMob) map[int32]server.TestMob {
	byID := make(map[int32]server.TestMob, len(mobs))
	for _, m := range mobs {
		byID[m.ID] = m
	}
	return byID
}

// awaitMobDamageEvent reads until the Damage Event a landed blow sent the
// body named, stopping at nothing: the pair's own spawn packets can arrive
// ahead of it, and the trail a landed hit leaves behind.
func awaitMobDamageEvent(c *servertest.Client, victimID int32) {
	c.T.Helper()
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlayDamageEvent {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != victimID {
			return false
		}
		if err := r.Err(); err != nil {
			c.T.Fatalf("damage event: %v", err)
		}
		return true
	}) {
		c.T.Fatalf("no damage event for entity %d arrived within %s", victimID, servertest.ExpectTimeout)
	}
}

// TestSpiderSpawnsGrowJockeyRidersAtAboutOneInAHundred runs a spider's
// finalize across a lattice of seeds and counts the riders: the one in a
// hundred of the spider's own roll, every rider a skeleton on the spider
// that rolled it.
func TestSpiderSpawnsGrowJockeyRidersAtAboutOneInAHundred(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, jockeyTestConfig())
	srv.StopTick()

	const spidersChecked = 2400
	var riders []int32
	jockeys := 0
	for i := range spidersChecked {
		x := 8 + float64(i%72)
		z := 8 + float64(i/72)
		id := srv.SpawnMob("minecraft:spider", x, 64, z)
		got := srv.MobPassengerIDs(id)
		if len(got) == 0 {
			continue
		}
		jockeys++
		riders = append(riders, got...)
		if len(got) != 1 {
			t.Errorf("spider %d carries %d riders, want the one its roll stood up", id, len(got))
		}
	}
	if jockeys < 8 || jockeys > 42 {
		t.Errorf("%d of %d spiders carry a skeleton jockey, want the one-in-a-hundred's share (8..42)",
			jockeys, spidersChecked)
	}
	byID := mobKinds(srv.Mobs())
	for _, riderID := range riders {
		rider, ok := byID[riderID]
		if !ok {
			t.Errorf("the rider of a jockey spider is no live mob")
			continue
		}
		if rider.Kind != "minecraft:skeleton" {
			t.Errorf("the spider's rider is a %s, want a skeleton", rider.Kind)
		}
	}
}

// TestABabyZombieTakesTheUnriddenChickenNearby plants hens through a zombie
// lattice and expects a baby zombie's nearby roll to take one: the hen it
// names one of the planted ones, standing unridden until the take and
// carrying the jockey mark after it.
func TestABabyZombieTakesTheUnriddenChickenNearby(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, jockeyTestConfig())
	srv.StopTick()

	// Hens every eight blocks through the lattice's middle, so nearly
	// every position stands within the five the finalize reaches.
	plants := make(map[int32]bool, 32)
	for row := range 4 {
		for col := range 8 {
			plants[srv.SpawnMob("minecraft:chicken", float64(16+col*8), 64, float64(16+row*8))] = true
		}
	}
	// The lattice: 72 columns by 30 rows.
	const spawns = 2160
	onPlantedHens := 0
	for i := range spawns {
		x := 8 + float64(i%72)
		z := 8 + float64(i/72)
		id := srv.SpawnMob("minecraft:zombie", x, 64, z)
		if veh := srv.MobVehicleID(id); veh != 0 && plants[veh] {
			onPlantedHens++
		}
	}
	if onPlantedHens == 0 {
		t.Fatalf("no baby zombie took a planted hen in %d spawns", spawns)
	}
	byID := mobKinds(srv.Mobs())
	for henID := range plants {
		riders := srv.MobPassengerIDs(henID)
		if len(riders) == 0 {
			continue
		}
		if len(riders) > 1 {
			t.Errorf("the planted hen %d carries %d riders, want the one seat a take leaves", henID, len(riders))
			continue
		}
		hen, ok := byID[henID]
		if !ok || !hen.ChickenJockey {
			t.Errorf("the taken hen %d carries no jockey mark, want the one a take leaves", henID)
		}
		rider, ok := byID[riders[0]]
		if !ok || !rider.Baby {
			t.Errorf("the rider of the planted hen %d is no baby zombie", henID)
		}
	}
}

// TestAChickenJockeyHenLaysNoEggs hatches a jockey pair through the seeded
// hunt, winds the fresh hen's egg countdown beside a plain hen's, and lets
// three ticks pass: the plain one lays, and the jockey mark holds its egg.
func TestAChickenJockeyHenLaysNoEggs(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, jockeyTestConfig())
	srv.StopTick()
	// The hunt stands its zombies up in country nobody holds; what ticks
	// here has to live in a chunk the world keeps.
	t.Cleanup(srv.HoldChunk(0, 0))

	rider, vehicle := huntAJockey(t, srv, 6000)
	// The pair into the held chunk, one driven tick to seat the rider.
	srv.MoveMob(vehicle, 4, 3)
	srv.MoveMob(rider, 4, 3)
	srv.TickMobs(1)
	// The hunt's zombies would burn once ticks run; servertest.Midnight keeps the
	// count of what the countdowns leave clean.
	srv.SetDayTime(servertest.Midnight)
	plain := srv.SpawnMob("minecraft:chicken", 4.5, 64, 3.5)
	srv.SetEggTimer(vehicle, 1)
	srv.SetEggTimer(plain, 1)

	before := srv.ItemCount()
	srv.TickMobs(3)
	if d := srv.ItemCount() - before; d != 1 {
		t.Errorf("%d items appeared for the two hens' countdowns, want the plain hen's one egg alone", d)
	}

	byID := mobKinds(srv.Mobs())
	if hen, ok := byID[vehicle]; !ok || hen.Kind != "minecraft:chicken" || !hen.ChickenJockey {
		t.Errorf("the jockey vehicle %d is no marked hen", vehicle)
	}
	if r, ok := byID[rider]; !ok || r.Kind != "minecraft:zombie" || !r.Baby || r.Vehicle != vehicle {
		t.Errorf("the jockey rider %d is not the baby zombie on the hen %d", rider, vehicle)
	}
}

// TestHittingAJockeyRiderLeavesItsSpiderStill swings at a spider jockey's
// skeleton and expects the spider untouched: the knockback lands on the
// rider, whose seat clears it, and the pair stays linked.
// vanilla: LivingEntity.knockback, Entity.rideTick
func TestHittingAJockeyRiderLeavesItsSpiderStill(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, jockeyTestConfig(), "Watcher")
	srv.StopTick()

	// The pair at the watcher's arm's length, one driven tick to seat the
	// rider and show the link.
	spider := srv.SpawnMob("minecraft:spider", 2, 64, 2)
	skeleton := srv.SpawnMob("minecraft:skeleton", 2, 64, 2)
	if !srv.LinkMobsForTest(spider, skeleton) {
		t.Fatal("the test pair could not be linked")
	}
	srv.TickMobs(1)
	if riders := servertest.AwaitPassengersFor(c, spider); len(riders) != 1 || riders[0] != skeleton {
		t.Fatalf("the spider's link names %v, want the skeleton %d", riders, skeleton)
	}
	x0, y0, z0, _ := srv.MobPos(spider)
	vx0, vy0, vz0, _ := srv.MobVelocity(spider)

	c.Attack(skeleton)
	awaitMobDamageEvent(c, skeleton)

	if x, y, z, _ := srv.MobPos(spider); x != x0 || y != y0 || z != z0 {
		t.Errorf("the spider stands at (%g, %g, %g) after the rider's hit, want (%g, %g, %g)", x, y, z, x0, y0, z0)
	}
	if vx, vy, vz, _ := srv.MobVelocity(spider); vx != vx0 || vy != vy0 || vz != vz0 {
		t.Errorf("the spider's velocity is (%g, %g, %g) after the rider's hit, want (%g, %g, %g)", vx, vy, vz, vx0, vy0, vz0)
	}
	if riders := srv.MobPassengerIDs(spider); len(riders) != 1 || riders[0] != skeleton {
		t.Errorf("the spider carries %v after the hit, want the skeleton still seated", riders)
	}
}

// TestARidersPairingTellsTheMountsLink checks the packet order a late viewer
// depends on: a jockey that walks into a player's range has its mount's spawn
// and link shown first, so the rider's own pairing carries the mount's
// passenger list behind the rider's spawn, where a client that dropped the
// link for an unknown rider takes it again.
// vanilla: ServerEntity.sendPairingData
func TestARidersPairingTellsTheMountsLink(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, jockeyTestConfig())
	srv.StopTick()

	vehicle := srv.SpawnMob("minecraft:chicken", 2, 64, 2)
	rider := srv.SpawnMob("minecraft:skeleton", 2, 64, 2)
	if !srv.LinkMobsForTest(vehicle, rider) {
		t.Fatal("the test pair could not be linked")
	}
	if ids := srv.MobPairingIDsForTest(rider); !slices.Contains(ids, protocol.IDPlaySetPassengers) {
		t.Errorf("the rider's pairing %v carries no Set Passengers, want the mount's link behind its own spawn", ids)
	}
}

// TestAJockeyPairSurvivesAChunkReload links a pair, lets its chunk unload and
// come back, and expects the file to have kept the ride: the reloaded hen
// carries the rider its compound named.
// vanilla: Entity.addSaveData's Passengers
func TestAJockeyPairSurvivesAChunkReload(t *testing.T) {
	t.Parallel()
	cfg := jockeyTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(servertest.Midnight)
	c := connect().Join("Keeper")

	vehicle := srv.SpawnMob("minecraft:chicken", 2, 64, 2)
	rider := srv.SpawnMob("minecraft:zombie", 2, 64, 2)
	if !srv.LinkMobsForTest(vehicle, rider) {
		t.Fatal("the test pair could not be linked")
	}
	vu, _ := srv.MobUUID(vehicle)
	ru, _ := srv.MobUUID(rider)
	if vu == "" || ru == "" {
		t.Fatal("the pair is not in the overworld herd")
	}

	c.Disconnect()
	servertest.WaitFor(t, srv, 400, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	connect().Join("Keeper")
	find := func(id string) server.TestMob {
		for _, m := range srv.MobsInDimension(world.DimensionName) {
			if m.UUID == id {
				return m
			}
		}
		return server.TestMob{}
	}
	servertest.WaitFor(t, srv, 400, func() bool { return find(vu).ID != 0 && find(ru).ID != 0 })

	hen, zombie := find(vu), find(ru)
	if riders := srv.MobPassengerIDs(hen.ID); len(riders) != 1 || riders[0] != zombie.ID {
		t.Errorf("the reloaded hen carries %v, want the rider %d the file kept", riders, zombie.ID)
	}
}

// TestAJockeyRiderIsSavedNestedAlone checks the file shape a save writes: the
// hen's own compound carries its rider nested, and no top-level compound
// lists the rider as well.
// vanilla: Entity.save, Entity.addSaveData
func TestAJockeyRiderIsSavedNestedAlone(t *testing.T) {
	t.Parallel()
	cfg := jockeyTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()

	vehicle := srv.SpawnMob("minecraft:chicken", 2, 64, 2)
	rider := srv.SpawnMob("minecraft:zombie", 2, 64, 2)
	if !srv.LinkMobsForTest(vehicle, rider) {
		t.Fatal("the test pair could not be linked")
	}
	srv.FlushMobs()

	topLevel, nested := 0, 0
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}) {
		if id, _ := nbt.Get[nbt.String](compound, "id"); string(id) == "minecraft:zombie" {
			topLevel++
		}
		if list, ok := nbt.Get[nbt.List](compound, "Passengers"); ok {
			for _, item := range list.Items {
				if pc, ok := item.(nbt.Compound); ok {
					if id, _ := nbt.Get[nbt.String](pc, "id"); string(id) == "minecraft:zombie" {
						nested++
					}
				}
			}
		}
	}
	if topLevel != 0 {
		t.Errorf("the file lists %d top-level zombies, want the rider written nested alone", topLevel)
	}
	if nested != 1 {
		t.Errorf("the file nests %d zombies under a mount, want the rider once", nested)
	}
}

// TestANestedRiderLoadsFromAVanillaFile plants a hen whose baby zombie exists
// only nested under its compound — the layout a vanilla save writes — and
// expects the wake to stand the rider up on the hen.
// vanilla: Entity.addSaveData
func TestANestedRiderLoadsFromAVanillaFile(t *testing.T) {
	t.Parallel()
	cfg := jockeyTestConfig()
	cfg.LevelName = t.TempDir()
	henID := servertest.FixtureUUID(4, 2, 0, 21)
	zombieID := servertest.FixtureUUID(4, 2, 0, 22)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0},
		nbt.Compound{
			"id":   nbt.String("minecraft:chicken"),
			"UUID": servertest.UuidWords(henID),
			"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(5.5), nbt.Double(64), nbt.Double(5.5),
			}},
			"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{
					"id":     nbt.String("minecraft:zombie"),
					"UUID":   servertest.UuidWords(zombieID),
					"Pos":    nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(5.5), nbt.Double(64.5), nbt.Double(5.5)}},
					"IsBaby": nbt.Byte(1),
				},
			}},
		})

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(servertest.Midnight)
	connect().Join("Keeper")

	find := func(id uuid.UUID) server.TestMob {
		for _, m := range srv.MobsInDimension(world.DimensionName) {
			if m.UUID == id.String() {
				return m
			}
		}
		return server.TestMob{}
	}
	servertest.WaitFor(t, srv, 400, func() bool { return find(henID).ID != 0 && find(zombieID).ID != 0 })

	hen, zombie := find(henID), find(zombieID)
	if srv.MobVehicleID(zombie.ID) != hen.ID {
		t.Errorf("the loaded baby zombie rides vehicle %d, want the hen %d", srv.MobVehicleID(zombie.ID), hen.ID)
	}
	if riders := srv.MobPassengerIDs(hen.ID); len(riders) != 1 || riders[0] != zombie.ID {
		t.Errorf("the loaded hen carries %v, want the nested rider %d", riders, zombie.ID)
	}
}

// TestAnUnmodelledNestedRiderSurvivesTheSave is the carry-through of a
// passenger this server does not simulate: a vanilla file's nested villager
// under a hen is written back under Passengers, where a modelled tag that
// swallowed the raw list would drop it with the wake.
// vanilla: Entity.addSaveData
func TestAnUnmodelledNestedRiderSurvivesTheSave(t *testing.T) {
	t.Parallel()
	cfg := jockeyTestConfig()
	cfg.LevelName = t.TempDir()
	henID := servertest.FixtureUUID(4, 2, 0, 31)
	villagerID := servertest.FixtureUUID(4, 2, 0, 32)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0},
		nbt.Compound{
			"id":   nbt.String("minecraft:chicken"),
			"UUID": servertest.UuidWords(henID),
			"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(5.5), nbt.Double(64), nbt.Double(5.5),
			}},
			"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{
					"id":   nbt.String("minecraft:villager"),
					"UUID": servertest.UuidWords(villagerID),
					"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(5.5), nbt.Double(64.5), nbt.Double(5.5)}},
				},
			}},
		})

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(servertest.Midnight)
	connect().Join("Keeper")

	servertest.WaitFor(t, srv, 400, func() bool {
		for _, m := range srv.MobsInDimension(world.DimensionName) {
			if m.UUID == henID.String() {
				return true
			}
		}
		return false
	})
	srv.FlushMobs()

	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}) {
		if id, _ := nbt.Get[nbt.String](compound, "id"); string(id) != "minecraft:chicken" {
			continue
		}
		list, _ := nbt.Get[nbt.List](compound, "Passengers")
		for _, item := range list.Items {
			pc, ok := item.(nbt.Compound)
			if !ok {
				continue
			}
			if id, _ := nbt.Get[nbt.String](pc, "id"); string(id) == "minecraft:villager" {
				return
			}
		}
	}
	t.Error("the hen's unmodelled nested villager was not written back under Passengers")
}

// TestAJockeyRiderStaysWithItsMountWhenOnlyItsChunkUnloads checks the unload
// path on a pair straddling a chunk border: the hen's seat pushes the rider's
// body into the next chunk, and only that chunk leaves. The rider is not
// written or dropped by itself — vanilla never saves a passenger alone — but
// stays with the mount whose chunk the world still holds.
// vanilla: PersistentEntitySectionManager.getPassengersAndSelf
func TestAJockeyRiderStaysWithItsMountWhenOnlyItsChunkUnloads(t *testing.T) {
	t.Parallel()
	cfg := jockeyTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()

	// The mount in chunk (0,0) is held; the rider is moved over the border
	// into chunk (0,1), which nothing holds.
	release := srv.HoldChunk(2, 15)
	defer release()
	vehicle := srv.SpawnMob("minecraft:chicken", 2, 64, 15.95)
	rider := srv.SpawnMob("minecraft:skeleton", 2, 64, 15.95)
	if !srv.LinkMobsForTest(vehicle, rider) {
		t.Fatal("the test pair could not be linked")
	}
	srv.MoveMob(rider, 2, 16.05)
	srv.TickMobs(1)

	byID := mobKinds(srv.Mobs())
	if _, ok := byID[rider]; !ok {
		t.Error("the rider left the world when only its own chunk unloaded, want it kept with the mount whose chunk is held")
	}
	if riders := srv.MobPassengerIDs(vehicle); len(riders) != 1 || riders[0] != rider {
		t.Errorf("the mount carries %v after the unload, want the rider %d still seated", riders, rider)
	}
}

// TestAJockeyRiderLeavesWithItsMountsChunk is the same pair the other way
// round: the mount's chunk leaves while the rider's own is held. Vanilla
// unloads and saves the whole ride under the root vehicle, so the rider is
// not left seated on a body the world has let go.
// vanilla: PersistentEntitySectionManager.getPassengersAndSelf
func TestAJockeyRiderLeavesWithItsMountsChunk(t *testing.T) {
	t.Parallel()
	cfg := jockeyTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()

	// Both spawn in chunk (0,0), which is then given up; the rider is moved
	// into the held chunk (0,1).
	mountRelease := srv.HoldChunk(2, 15)
	vehicle := srv.SpawnMob("minecraft:chicken", 2, 64, 15.95)
	rider := srv.SpawnMob("minecraft:skeleton", 2, 64, 15.95)
	if !srv.LinkMobsForTest(vehicle, rider) {
		t.Fatal("the test pair could not be linked")
	}
	mountRelease()
	srv.FlushSaves()
	riderRelease := srv.HoldChunk(2, 16)
	defer riderRelease()
	srv.MoveMob(rider, 2, 16.05)
	srv.TickMobs(1)

	byID := mobKinds(srv.Mobs())
	if _, ok := byID[vehicle]; ok {
		t.Error("the mount stayed in the world after its chunk left")
	}
	if _, ok := byID[rider]; ok {
		t.Error("the rider stayed seated on a mount whose chunk left the world, want the whole ride unloaded together")
	}
}

// TestAutosaveKeepsARiderWithItsMountsChunk plants an asleep cow in the chunk
// a rider's seat is pushed into and flushes the herd: the rider belongs to
// its mount's chunk, so the flush neither wakes the rider's own chunk nor
// stands the cow back up. Grouping the rider by its own chunk would wake
// that chunk for the write and add its saved mobs to the live herd.
// vanilla: PersistentEntitySectionManager.getPassengersAndSelf
func TestAutosaveKeepsARiderWithItsMountsChunk(t *testing.T) {
	t.Parallel()
	cfg := jockeyTestConfig()
	cfg.LevelName = t.TempDir()
	cowID := servertest.FixtureUUID(6, 1, 0, 1)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 1},
		nbt.Compound{
			"id":   nbt.String("minecraft:cow"),
			"UUID": servertest.UuidWords(cowID),
			"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(2.5), nbt.Double(64), nbt.Double(18.5),
			}},
		})

	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()
	// The mount's chunk (0,0) is held; the rider's seat falls over the
	// border into chunk (0,1), which nothing holds.
	release := srv.HoldChunk(2, 15)
	defer release()
	vehicle := srv.SpawnMob("minecraft:chicken", 2, 64, 15.95)
	rider := srv.SpawnMob("minecraft:skeleton", 2, 64, 15.95)
	if !srv.LinkMobsForTest(vehicle, rider) {
		t.Fatal("the test pair could not be linked")
	}
	srv.MoveMob(rider, 2, 16.05)

	live := func() int32 {
		for _, m := range srv.MobsInDimension(world.DimensionName) {
			if m.UUID == cowID.String() {
				return m.ID
			}
		}
		return 0
	}
	if id := live(); id != 0 {
		t.Fatalf("the planted cow %d stood up before the flush, want its chunk left asleep", id)
	}

	srv.FlushMobs()

	if id := live(); id != 0 {
		t.Errorf("the autosave woke the rider's own chunk and stood its asleep cow %d back up, want the rider saved under its mount", id)
	}
}

// TestMidnightSpawnPacksGrowChickenJockeys runs the natural pass through a
// night, clearing the herd between passes so the caps never close, and
// expects baby zombies to stand on fresh jockey hens with the Set
// Passengers link the spawn carries arriving on the wire.
func TestMidnightSpawnPacksGrowChickenJockeys(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, jockeyTestConfig(), "Watcher")
	srv.StopTick()
	srv.SetDayTime(servertest.Midnight)

	// What each drained Set Passengers said: a vehicle's last link is what
	// it carries now.
	links := map[int32][]int32{}
	record := func(id int32, data []byte) {
		if id == protocol.IDPlaySetPassengers {
			vehicle, riders := servertest.ReadPassengers(c, data)
			links[vehicle] = riders
		}
	}
	drain := func() {
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			record(id, data)
			return false
		})
	}

	// A seeded night's worth of passes, stopping at the first pair a pack
	// stands up; the pass's dice give a pack's babies a few chances in a
	// thousand, and the herd is cleared between passes so the caps never
	// close on the next pass's dice.
	const passes = 9000
	var keepRider, keepVehicle int32
	for seed := int64(1); seed <= passes && keepRider == 0; seed++ {
		srv.SpawnPass(seed)
		mobs := srv.Mobs()
		kinds := mobKinds(mobs)
		for _, m := range mobs {
			switch {
			case m.Vehicle != 0 && m.Kind == "minecraft:zombie" && kinds[m.Vehicle].Kind == "minecraft:chicken":
				keepRider, keepVehicle = m.ID, m.Vehicle
			case m.Kind == "minecraft:chicken":
				// A hen lives on: outlives the monsters between the
				// passes, whatever rider it once had.
			default:
				srv.KillMob(m.ID)
			}
		}
		srv.TickMobs(1)
		if seed%20 == 0 {
			drain()
		}
	}
	// A window can close before the mailbox has written the last pass's
	// links; the settle's answer rides the mailbox behind them.
	c.SettleAndCollect(record)
	if keepRider == 0 {
		t.Fatalf("no baby zombie pack grew a chicken jockey in %d seeded passes", passes)
	}

	byID := mobKinds(srv.Mobs())
	hen, ok := byID[keepVehicle]
	if !ok || hen.Kind != "minecraft:chicken" || !hen.ChickenJockey {
		t.Fatalf("the kept jockey vehicle %d is no marked hen", keepVehicle)
	}
	rider, ok := byID[keepRider]
	if !ok || rider.Kind != "minecraft:zombie" || !rider.Baby || rider.Vehicle != keepVehicle {
		t.Fatalf("the kept jockey rider %d is no baby zombie on the hen %d", keepRider, keepVehicle)
	}
	riders, told := links[keepVehicle]
	if !told {
		t.Fatalf("no Set Passengers about the kept hen %d arrived", keepVehicle)
	}
	if len(riders) != 1 || riders[0] != keepRider {
		t.Errorf("the wire's link names %v riding the hen %d, want the kept rider %d", riders, keepVehicle, keepRider)
	}
}
