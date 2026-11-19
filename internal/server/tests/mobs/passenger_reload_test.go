package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: EntityType.loadEntityRecursive
func TestMobPassengersSurviveRegionReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"), freeze)
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon pig 3.5 65 3.5 {NoAI:1b,PersistenceRequired:1b,Passengers:[{id:"minecraft:cow",NoAI:1b,PersistenceRequired:1b}]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Settle()
	pigs, cows := srv.MobIDsOfKind("minecraft:pig"), srv.MobIDsOfKind("minecraft:cow")
	if len(pigs) != 1 || len(cows) != 1 || !slices.Equal(srv.MobPassengerIDs(pigs[0]), cows) {
		t.Fatalf("initial mount = pigs %v, cows %v", pigs, cows)
	}
	srv.FlushMobs()
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, again := servertest.Join(t, &restarted, "Keeper", servertest.OpsListing(t, "Keeper"), freeze)
	servertest.HoldTicks(t, back)
	servertest.RunTicks(t, back, 1)
	again.Settle()
	pigs, cows = back.MobIDsOfKind("minecraft:pig"), back.MobIDsOfKind("minecraft:cow")
	if len(pigs) != 1 || len(cows) != 1 {
		t.Fatalf("reloaded mobs = pigs %v, cows %v", pigs, cows)
	}
	if got := back.MobPassengerIDs(pigs[0]); !slices.Equal(got, cows) {
		t.Errorf("reloaded pig passengers = %v, want %v", got, cows)
	}
}
