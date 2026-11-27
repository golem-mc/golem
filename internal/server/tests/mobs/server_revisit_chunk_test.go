package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAMobSummonedInARevisitedEmptyChunkComesBackOnce loads a chunk with no
// entities twice, summons a pig in it, and loads it once more: the pig is
// there exactly once.
func TestAMobSummonedInARevisitedEmptyChunkComesBackOnce(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()

	const x, z = 1608, 1608
	release := srv.HoldChunk(x, z)
	srv.TickMobs(1)
	release()
	srv.TickMobs(1)

	release = srv.HoldChunk(x, z)
	srv.TickMobs(1)
	srv.SpawnMob("minecraft:pig", x+0.5, 64, z+0.5)
	srv.TickMobs(1)
	release()
	srv.TickMobs(1)
	if got := srv.MobCount(); got != 0 {
		t.Fatalf("%d mobs stand in the unloaded chunk, want 0", got)
	}

	release = srv.HoldChunk(x, z)
	defer release()
	srv.TickMobs(1)
	if got := srv.MobCount(); got != 1 {
		t.Fatalf("%d pigs stand in the reloaded chunk, want 1", got)
	}
}
