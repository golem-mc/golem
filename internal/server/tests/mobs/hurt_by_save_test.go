package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LivingEntity.addAdditionalSaveData, readAdditionalSaveData
func TestPlayerKillCreditSurvivesRegionReload(t *testing.T) {
	t.Parallel()
	const name = "Slayer"
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name), freeze)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon zombie 0.5 64 2.5 {NoAI:1b,PersistenceRequired:1b}`)
	id := c.AwaitSpawn(zombieType(t)).EntityID
	c.AttackLanded(srv, name, id)
	servertest.RunTicks(t, srv, 2)
	want := nbt.UUIDToIntArray(protocol.OfflineUUID(name))
	check := func(s *server.Server, id int32, stage string) {
		t.Helper()
		tag, ok := s.MobSaveTag(id)
		if !ok {
			t.Fatalf("%s: zombie missing", stage)
		}
		got, _ := nbt.Get[nbt.IntArray](tag, "last_hurt_by_player")
		if len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
			t.Errorf("%s: last_hurt_by_player = %v, want %v", stage, got, want)
		}
		if ticks, _ := nbt.IntField(tag, "last_hurt_by_player_memory_time"); ticks <= 0 || ticks > 100 {
			t.Errorf("%s: last_hurt_by_player_memory_time = %d, want 1..100", stage, ticks)
		}
	}
	check(srv, id, "before reload")
	srv.FlushMobs()
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, again, _ := servertest.JoinAs(t, &restarted, name, servertest.OpsListing(t, name), freeze)
	servertest.HoldTicks(t, back)
	servertest.RunTicks(t, back, 1)
	again.Settle()
	zombies := back.MobIDsOfKind("minecraft:zombie")
	if len(zombies) != 1 {
		t.Fatalf("reloaded zombies = %v", zombies)
	}
	check(back, zombies[0], "after reload")
	// Out of the player's reach, which would collect the orbs the moment they appear.
	again.ChatCommand("tp @e[type=minecraft:zombie] 0.5 200 0.5")
	again.ChatCommand("kill @e[type=minecraft:zombie]")
	again.Settle()
	servertest.UntilTicks(t, back, 40, "experience orbs from the kill", func() bool { return back.OrbCount() > 0 })
}

func zombieType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:zombie")
	if !ok {
		t.Fatal("no zombie entity type")
	}
	return id
}
