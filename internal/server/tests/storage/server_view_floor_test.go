package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A render distance below two is raised to two, both for the chunks sent and
// for entity tracking; a later request of three widens both.
// vanilla: ChunkMap.getPlayerViewDistance clamps to [2, server distance]
func TestRenderDistanceBelowTwoActsAsTwo(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelType = "minecraft:flat"
	cfg.ViewDistance, cfg.SimulationDistance = 4, 2
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.ViewAsk = 1
	c.Join("Watcher")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	srv.SpawnMob("minecraft:cow", 24.5, 64, 0.5)
	if !servertest.ASpawnOfWithinTicks(c, srv, 5, protocol.EntityTypeCow) {
		t.Fatal("a cow 24 blocks away is not paired at a requested distance of 1")
	}

	srv.SpawnMob("minecraft:cow", 40.5, 64, 0.5)
	if servertest.ASpawnOfWithinTicks(c, srv, 5, protocol.EntityTypeCow) {
		t.Fatal("a cow 40 blocks away is paired at a requested distance of 1")
	}
	setRenderDistance(c, 3)
	if !servertest.ASpawnOfWithinTicks(c, srv, 5, protocol.EntityTypeCow) {
		t.Fatal("a cow 40 blocks away is not paired after requesting 3")
	}
}
