package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
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
