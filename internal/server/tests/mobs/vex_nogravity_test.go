package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Vex.tick, Entity.setNoGravity
func TestAVexTellsWatchersNoGravityOnItsFirstTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 1, 1
	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	}, servertest.OpsListing(t, "Watcher"))
	c := connect().Join("Watcher")
	c.ChatCommand(`summon minecraft:vex ~2 ~2 ~ {NoAI:1b}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeVex)
	if e := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexNoGravity); e != nil && e.FlagVal {
		t.Fatal("the unticked vex spawned with NoGravity set")
	}

	servertest.HoldTicks(t, srv)
	e, ok := c.MetaEntryWithinTicks(srv, 1, spawn.EntityID, protocol.MetadataIndexNoGravity)
	if !ok {
		t.Fatal("the vex's first tick sent no NoGravity metadata update")
	}
	if e.Typ != protocol.MetadataBoolean || !e.FlagVal {
		t.Fatalf("NoGravity update = %+v, want boolean true", e)
	}
}
