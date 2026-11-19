package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestVillageSiegeSpawnsTwentyAndHonorsGates(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Siege", servertest.OpsListing(t, "Siege"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	// Occupied POIs cover the player and the ring of siege spawn positions.
	var villagers []int32
	for _, x := range []int32{-32, 0, 32} {
		for _, z := range []int32{-32, 0, 32} {
			srv.SetBlock(x, 64, z, registry.BlockBell.DefaultState())
			srv.SetBlock(x+2, 64, z, registry.BlockWhiteBed.DefaultState())
			srv.SetBlock(x-2, 64, z, registry.BlockComposter.DefaultState())
			villagers = append(villagers, srv.SpawnMob("minecraft:villager", float64(x)+0.5, 64, float64(z)+1.5))
		}
	}
	servertest.UntilTicks(t, srv, 400, "villagers claim the siege village", func() bool {
		for _, x := range []int32{-32, 0, 32} {
			for _, z := range []int32{-32, 0, 32} {
				if !srv.VillageNear("minecraft:overworld", x, 64, z) {
					return false
				}
			}
		}
		return true
	})
	for _, id := range villagers {
		srv.SetMobNoAI(id)
		srv.SetMobInvulnerable(id)
	}
	run := func() {
		// Natural spawning stays parked; each held tick drives only the custom spawner.
		// This roll keeps the square spawn offsets within forty blocks.
		srv.ForceVillageSiege(61)
		for range 60 {
			srv.VillageSiegePass()
			servertest.RunTicks(t, srv, 1)
		}
	}
	c.ChatCommand("time set noon")
	c.Settle()
	run()
	if n := len(srv.MobIDsOfKind("minecraft:zombie")); n != 0 {
		t.Fatalf("day: %d zombies", n)
	}
	c.ChatCommand("time set 18001")
	c.ChatCommand("gamerule spawn_monsters false")
	c.Settle()
	run()
	if n := len(srv.MobIDsOfKind("minecraft:zombie")); n != 0 {
		t.Fatalf("monsters off: %d zombies", n)
	}
	c.ChatCommand("gamerule spawn_monsters true")
	c.Settle()
	c.ChatCommand("gamerule spawn_mobs false")
	c.ChatCommand("difficulty easy")
	c.ChatCommand("difficulty normal")
	c.ChatCommand("gamerule spawn_mobs true")
	c.Settle()
	run()
	if n := len(srv.MobIDsOfKind("minecraft:zombie")); n != 0 {
		t.Fatalf("enemy spawning latched off: %d zombies", n)
	}
	c.ChatCommand("gamerule spawn_monsters false")
	c.ChatCommand("gamerule spawn_monsters true")
	c.Settle()
	run()
	ids := srv.MobIDsOfKind("minecraft:zombie")
	if len(ids) != 20 {
		t.Fatalf("night: %d zombies, want 20", len(ids))
	}
	for _, id := range ids {
		x, _, z, _ := srv.MobPos(id)
		if distance := math.Hypot(x-0.5, z-0.5); distance > 40 {
			t.Fatalf("zombie %d distance %.2f, want <=40", id, distance)
		}
	}
}

// moveThroughVillageRun spawns a zombie 16 blocks from a bell and returns how
// close it gets to the bell in 300 ticks at the given time of day.
func moveThroughVillageRun(t *testing.T, timeCmd string) float64 {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Stroll", servertest.OpsListing(t, "Stroll"))
	c.MoveToGround(0.5, 64, -10.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	c.ChatCommand(timeCmd)
	c.ChatCommand("gamerule advance_time false")
	c.Settle()
	srv.SetBlock(10, 64, 0, registry.BlockBell.DefaultState())
	srv.SetBlock(12, 64, 0, registry.BlockWhiteBed.DefaultState())
	v := srv.SpawnMob("minecraft:villager", 10.5, 64, 1.5)
	servertest.UntilTicks(t, srv, 400, "villagers claim the village", func() bool {
		return srv.VillageNear("minecraft:overworld", 10, 64, 0)
	})
	srv.SetMobNoAI(v)
	srv.SetMobInvulnerable(v)
	z := srv.SpawnMob("minecraft:zombie", -6.5, 64, 0.5)
	srv.SetMobInvulnerable(z)
	best := math.Inf(1)
	for range 300 {
		servertest.RunTicks(t, srv, 1)
		x, _, zz, _ := srv.MobPos(z)
		best = math.Min(best, math.Hypot(x-10.5, zz-0.5))
	}
	return best
}

// MoveThroughVillageGoal sends a zombie to a village point of interest only at
// night, whether or not it breaks doors.
func TestZombieMovesThroughVillageAtNight(t *testing.T) {
	t.Parallel()
	if d := moveThroughVillageRun(t, "time set 18000"); d > 6 {
		t.Fatalf("night: zombie came within %.2f of the bell, want <=6", d)
	}
}

func TestZombieStaysOutOfVillageByDay(t *testing.T) {
	t.Parallel()
	if d := moveThroughVillageRun(t, "time set noon"); d < 8 {
		t.Fatalf("day: zombie came within %.2f of the bell, want >=8", d)
	}
}
