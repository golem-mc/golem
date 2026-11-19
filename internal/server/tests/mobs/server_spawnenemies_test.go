package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: MinecraftServer.updateMobSpawningFlags, ServerChunkCache.tickChunks
func TestMonsterSpawningFlagsStayLatched(t *testing.T) {
	t.Parallel()
	for _, refresh := range []string{"monster rule", "difficulty", "same-value packet"} {
		t.Run(refresh, func(t *testing.T) {
			cfg := servertest.SpawningTestConfig()
			cfg.GameMode = "creative"
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"), func(s *server.Server) {
				servertest.FreezeUntilHeld(t, s)
			})
			servertest.AwaitChunksSettled(t, srv)
			servertest.HoldTicks(t, srv)
			for _, cmd := range []string{
				"time set midnight",
				"gamerule advance_time false",
				"gamerule spawn_mobs false",
				"difficulty normal",
				"gamerule spawn_mobs true",
			} {
				c.ChatCommand(cmd)
				c.Settle()
			}
			srv.SetSpawning(true)
			servertest.RunTicks(t, srv, 300)
			// The flat stone fixture has no grass, water or caves: only the
			// monster category can spawn here, on the dark surface.
			if mobs := srv.Mobs(); len(mobs) != 0 {
				t.Fatalf("spawn_mobs alone refreshed the enemy flag: %d mobs", len(mobs))
			}
			// The command refuses an unchanged value before writing the rule.
			c.ChatCommand("gamerule spawn_monsters true")
			c.Settle()
			servertest.RunTicks(t, srv, 50)
			if mobs := srv.Mobs(); len(mobs) != 0 {
				t.Fatalf("refused command refreshed the enemy flag: %d mobs", len(mobs))
			}
			switch refresh {
			case "monster rule":
				c.ChatCommand("gamerule spawn_monsters false")
				c.ChatCommand("gamerule spawn_monsters true")
			case "difficulty":
				c.ChatCommand("difficulty easy")
			case "same-value packet":
				c.SendSetGameRule([2]string{"minecraft:spawn_monsters", "true"})
			}
			c.Settle()
			servertest.UntilTicks(t, srv, 400, "monsters after refreshing the enemy flag", func() bool { return len(srv.Mobs()) != 0 })
		})
	}
}
