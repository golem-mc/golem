package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

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
