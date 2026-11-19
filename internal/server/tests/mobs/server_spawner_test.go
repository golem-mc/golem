package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	server "github.com/golem-mc/golem/internal/server"
)

// The spawner block tests: a spawner set by /setblock with its SpawnData,
// the player standing where the join puts them at (0.5, 64, 0.5) on the
// flat world's surface, the clock at midnight so the surface is dark enough
// for a monster, and the block entity pass run by hand with the main loop
// stopped, so only the spawner stands mobs up.

// spawnerTests joins an operator, runs the command with the loop still
// running, and stops the loop once the spawner's data has gone out.
func spawnerTests(t *testing.T, command string, before ...string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Spawnwatcher"))
	c.Login("Spawnwatcher")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()
	srv.StopTick()
	srv.SetDayTime(midnight)
	for _, b := range before {
		c.ChatCommand(b)
		c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	}
	c.ChatCommand(command)
	servertest.ExpectBlockEntityData(t, c)
	return srv, c
}

// zombiesAround lists the zombies the server holds, failing for any that
// stands outside the spawner at (4, 64, 4)'s spawn range of 4: the centred
// spread on x and z, and the block below, level or above on y.
func zombiesAround(t *testing.T, srv *server.Server) int {
	t.Helper()
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:zombie" {
			t.Errorf("the spawner stood up a %s, want only its zombie", m.Kind)
			continue
		}
		n++
		if math.Abs(m.X-4.5) > 4 || math.Abs(m.Z-4.5) > 4 || m.Y < 63 || m.Y > 65 {
			t.Errorf("a zombie stands at (%.2f, %.2f, %.2f), outside the spawner's range", m.X, m.Y, m.Z)
		}
	}
	return n
}

// TestSpawnerSpawnsItsMobUpToTheCap sets a zombie spawner with short delays
// beside the player and runs its ticks: zombies come, all within the spawn
// range, and the count stops at the six MaxNearbyEntities defaults to.
// vanilla: BaseSpawner.serverTick
func TestSpawnerSpawnsItsMobUpToTheCap(t *testing.T) {
	t.Parallel()
	srv, c := spawnerTests(t,
		`setblock 4 64 4 minecraft:spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnData:{entity:{id:"minecraft:zombie"}}}`)
	for range 2000 {
		srv.TickBlockEntities()
		if n := zombiesAround(t, srv); n > 6 {
			t.Fatalf("%d zombies stand around the spawner, more than the 6 it may keep near", n)
		}
	}
	if n := zombiesAround(t, srv); n != 6 {
		t.Errorf("%d zombies stand around the spawner after 2000 ticks, want the cap of 6", n)
	}
	c.Disconnect()
}

// TestSpawnerCustomSpawnRulesReplaceTheDarknessCheck sets a zombie spawner at
// noon: with custom_spawn_rules allowing every light level zombies come; with
// none the monster darkness check keeps them away.
// vanilla: BaseSpawner.serverTick
func TestSpawnerCustomSpawnRulesReplaceTheDarknessCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rules string
		spawns      bool
	}{
		{"custom rules", `,custom_spawn_rules:{block_light_limit:[0,15],sky_light_limit:[0,15]}`, true},
		{"no custom rules", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := spawnerTests(t,
				`setblock 4 64 4 minecraft:spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnData:{entity:{id:"minecraft:zombie"}`+tc.rules+`}}`)
			srv.SetDayTime(6000)
			n := 0
			for range 300 {
				srv.TickBlockEntities()
				if n = zombiesAround(t, srv); n > 0 {
					break
				}
			}
			if (n > 0) != tc.spawns {
				t.Errorf("%d zombies at noon, want spawning=%v", n, tc.spawns)
			}
			c.Disconnect()
		})
	}
}

// TestSpawnerCustomSpawnRulesFloorTheDarkenedSky sets a zombie spawner whose
// rules limit only block light under a wide stone roof at midnight: the sky
// light there is 0, and the night's darkening of 11 floors at 0 instead of
// falling below the default sky limit of 0..15, so zombies come.
// vanilla: SpawnData.CustomSpawnRules.isValidPosition
func TestSpawnerCustomSpawnRulesFloorTheDarkenedSky(t *testing.T) {
	t.Parallel()
	srv, c := spawnerTests(t,
		`setblock 4 64 4 minecraft:spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnData:{entity:{id:"minecraft:zombie"},custom_spawn_rules:{block_light_limit:[0,15]}}}`,
		`fill -20 67 -20 28 67 28 minecraft:stone`)
	srv.OverworldWorld().FlushLighting()
	if sky := srv.SkyLightAt(4, 64, 4); sky != 0 {
		t.Fatalf("sky light under the roof is %d, want 0", sky)
	}
	n := 0
	for range 300 {
		srv.TickBlockEntities()
		if n = zombiesAround(t, srv); n > 0 {
			break
		}
	}
	if n == 0 {
		t.Error("no zombie came under the roof at midnight, want the darkened sky light floored at 0 to pass the rules")
	}
	c.Disconnect()
}

// TestSpawnerWaitsOutItsDelays runs a spawner holding only its SpawnData:
// nothing before the first delay of 20 ticks has run out, and once a round
// has stood a zombie up, nothing for the next 200 ticks at least, the low
// end of the delay it rolls.
// vanilla: BaseSpawner.delay
func TestSpawnerWaitsOutItsDelays(t *testing.T) {
	t.Parallel()
	srv, c := spawnerTests(t, `setblock 4 64 4 minecraft:spawner{SpawnData:{entity:{id:"minecraft:zombie"}}}`)
	for tick := 1; tick <= 20; tick++ {
		srv.TickBlockEntities()
		if n := zombiesAround(t, srv); n != 0 {
			t.Fatalf("%d zombies after %d ticks, want none before the first delay runs out", n, tick)
		}
	}
	first := 0
	for range 2000 {
		srv.TickBlockEntities()
		if first = zombiesAround(t, srv); first > 0 {
			break
		}
	}
	if first == 0 {
		t.Fatal("no zombie came in 2000 ticks after the first delay")
	}
	for tick := 1; tick <= 200; tick++ {
		srv.TickBlockEntities()
		if n := zombiesAround(t, srv); n != first {
			t.Fatalf("%d zombies %d ticks after a round stood up %d, want none before the next delay", n, tick, first)
		}
	}
	c.Disconnect()
}

// TestSpawnerIdlesWithoutAPlayerOrTheRule checks the two gates on the tick:
// a required player range the player stands outside, and the
// spawner_blocks_work game rule off.
// vanilla: BaseSpawner.isNearPlayer, ServerLevel.isSpawnerBlockEnabled
func TestSpawnerIdlesWithoutAPlayerOrTheRule(t *testing.T) {
	t.Parallel()
	t.Run("out of range", func(t *testing.T) {
		t.Parallel()
		srv, c := spawnerTests(t,
			`setblock 4 64 4 minecraft:spawner{Delay:0s,RequiredPlayerRange:2s,SpawnData:{entity:{id:"minecraft:zombie"}}}`)
		for range 1000 {
			srv.TickBlockEntities()
		}
		if n := zombiesAround(t, srv); n != 0 {
			t.Errorf("%d zombies came with the player outside the required range", n)
		}
		c.Disconnect()
	})
	t.Run("rule off", func(t *testing.T) {
		t.Parallel()
		srv, c := spawnerTests(t,
			`setblock 4 64 4 minecraft:spawner{Delay:0s,SpawnData:{entity:{id:"minecraft:zombie"}}}`)
		srv.SetGameRule("spawner_blocks_work", false)
		for range 1000 {
			srv.TickBlockEntities()
		}
		if n := zombiesAround(t, srv); n != 0 {
			t.Errorf("%d zombies came with spawner_blocks_work off", n)
		}
		c.Disconnect()
	})
}

// TestSpawnerTellsEachNewSpawnData sets a spawner with two potentials and a
// delay to roll: the clients hear the block entity's new data and the block
// event of the new delay.
// vanilla: SpawnerBlockEntity.setNextSpawnData, BaseSpawner.delay
func TestSpawnerTellsEachNewSpawnData(t *testing.T) {
	t.Parallel()
	srv, c := spawnerTests(t, `setblock 4 64 4 minecraft:spawner{Delay:-1s,SpawnPotentials:[`+
		`{weight:1,data:{entity:{id:"minecraft:zombie"}}},{weight:1,data:{entity:{id:"minecraft:skeleton"}}}]}`)
	srv.TickBlockEntities()
	var data, action bool
	c.ReadTicks(srv, 0, func(id int32, _ []byte) bool {
		switch id {
		case protocol.IDPlayBlockEntityData:
			data = true
		case protocol.IDPlayBlockAction:
			action = true
		}
		return false
	})
	if !data {
		t.Error("no Block Entity Data followed the roll of the spawner's SpawnData")
	}
	if !action {
		t.Error("no block event followed the new delay")
	}
	c.Disconnect()
}

// TestSpawnerWithoutPotentialsTellsItsSpawnData rolls a spawner that carries
// a SpawnData and no SpawnPotentials: the list defaults to that data, so the
// clients still hear the roll.
// vanilla: BaseSpawner.load, BaseSpawner.delay
func TestSpawnerWithoutPotentialsTellsItsSpawnData(t *testing.T) {
	t.Parallel()
	srv, c := spawnerTests(t, `setblock 4 64 4 minecraft:spawner{Delay:-1s,SpawnData:{entity:{id:"minecraft:zombie"}}}`)
	srv.TickBlockEntities()
	var data bool
	c.ReadTicks(srv, 0, func(id int32, _ []byte) bool {
		if id == protocol.IDPlayBlockEntityData {
			data = true
		}
		return false
	})
	if !data {
		t.Error("no Block Entity Data followed the roll of the lone SpawnData")
	}
	c.Disconnect()
}

// TestSpawnerFacesTaggedSpawnsAtRandom spawns zombies whose entity tag
// carries more than the id: each still gets its own random yaw.
// vanilla: BaseSpawner.serverTick
func TestSpawnerFacesTaggedSpawnsAtRandom(t *testing.T) {
	t.Parallel()
	srv, c := spawnerTests(t,
		`setblock 4 64 4 minecraft:spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnData:{entity:{id:"minecraft:zombie",PersistenceRequired:1b}}}`)
	for range 2000 {
		srv.TickBlockEntities()
	}
	turned := 0
	for _, m := range srv.Mobs() {
		if m.Yaw != 0 {
			turned++
		}
	}
	if turned < 5 {
		t.Errorf("%d of the tagged zombies face a rolled yaw, want nearly all of 6", turned)
	}
	c.Disconnect()
}

// TestSpawnerIdlesForSpectatorsAndPeaceful checks that a spectator alone
// does not wake a spawner and that peaceful difficulty refuses a zombie.
// vanilla: EntityGetter.hasNearbyAlivePlayer, Monster.checkMonsterSpawnRules
func TestSpawnerIdlesForSpectatorsAndPeaceful(t *testing.T) {
	t.Parallel()
	for _, prep := range []string{"gamemode spectator", "difficulty peaceful"} {
		t.Run(prep, func(t *testing.T) {
			t.Parallel()
			srv, c := spawnerTests(t,
				`setblock 4 64 4 minecraft:spawner{Delay:0s,SpawnData:{entity:{id:"minecraft:zombie"}}}`, prep)
			for range 1000 {
				srv.TickBlockEntities()
			}
			if n := zombiesAround(t, srv); n != 0 {
				t.Errorf("%d zombies came after %q", n, prep)
			}
			c.Disconnect()
		})
	}
}

// TestSpawnerDressesItsMobFromTheEquipmentTable sets a zombie spawner whose
// SpawnData names the trial chamber melee table: every zombie holds the iron
// or diamond sword the table always rolls, and the single drop chance of 0
// lands on every slot the table filled.
// vanilla: BaseSpawner.serverTick, Mob.equip(EquipmentTable)
func TestSpawnerDressesItsMobFromTheEquipmentTable(t *testing.T) {
	t.Parallel()
	srv, c := spawnerTests(t,
		`setblock 4 64 4 minecraft:spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnData:{entity:{id:"minecraft:zombie"},`+
			`equipment:{loot_table:"minecraft:equipment/trial_chamber_melee",slot_drop_chances:0.0f}}}`)
	for range 400 {
		if srv.MobCount() >= 3 {
			break
		}
		srv.TickBlockEntities()
	}
	mobs := srv.Mobs()
	if len(mobs) < 3 {
		t.Fatalf("%d zombies after 400 spawner ticks, want at least 3", len(mobs))
	}
	swords := []int32{servertest.MustItemID(t, "minecraft:iron_sword"), servertest.MustItemID(t, "minecraft:diamond_sword")}
	for _, m := range mobs {
		if held := srv.MobEquipment(m.ID, "mainhand"); !slices.Contains(swords, held.ItemID) || held.Count != 1 {
			t.Errorf("zombie %d holds item %d x%d, want an iron or diamond sword", m.ID, held.ItemID, held.Count)
		}
		gear, chances := srv.MobGear(m.ID)
		for slot, stack := range gear {
			if !stack.Empty() && chances[slot] != 0 {
				t.Errorf("zombie %d slot %d drop chance %v, want 0", m.ID, slot, chances[slot])
			}
		}
	}
	c.Disconnect()
}
