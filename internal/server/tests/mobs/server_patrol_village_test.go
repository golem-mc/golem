package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestUnclaimedBedIsNoVillageForPatrols: a bed no villager claimed does not
// keep patrols away from the player.
// vanilla: PatrolSpawner.spawnPatrolMember, ServerLevel.isCloseToVillage
func TestUnclaimedBedIsNoVillageForPatrols(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	cfg.ViewDistance, cfg.SimulationDistance = 3, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Patrol", servertest.OpsListing(t, "Patrol"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	c.ChatCommand("time set 126000")
	c.Settle()
	w := srv.OverworldWorld()
	for x := int32(-3); x <= -2; x++ {
		for z := int32(-3); z <= -1; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			t.Cleanup(func() { w.Release(pos) })
		}
	}
	srv.SetBlock(3, 64, 4, servertest.BedStateName("red", "head", "south"))
	srv.SetBlock(3, 64, 3, servertest.BedStateName("red", "foot", "south"))
	srv.PatrolSpawnerPass(9)
	if got := len(srv.MobIDsOfKind("minecraft:pillager")); got != 3 {
		t.Fatalf("patrol size %d beside an unclaimed bed, want 3", got)
	}
}
