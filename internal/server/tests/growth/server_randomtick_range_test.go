package growth

import (
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestRandomTicksReachTheChunksAroundASpectator(t *testing.T) {
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Alice", servertest.OpsListing(t, "Alice"))
	srv.StopTick()
	c.ChatCommand("gamemode spectator")
	c.Settle()
	if got := srv.PlayerGameMode("Alice"); got != int32(protocol.GameModeSpectator) {
		t.Fatalf("game mode = %d, want spectator", got)
	}
	p := srv.PlayerPosition("Alice")
	own := world.ChunkPos{X: world.ChunkOf(p.X), Z: world.ChunkOf(p.Z)}
	if !slices.Contains(srv.RandomTickChunkPositions(), own) {
		t.Fatalf("spectator's chunk %v is not random-ticked", own)
	}
}

// A chunk at the corner of the simulation square lies past the 128 blocks of
// the spawning circle but still random-ticks.
func TestRandomTicksReachChunksBeyondTheSpawningRing(t *testing.T) {
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Alice", servertest.FullSimulationDistance)
	c.Settle()
	p := srv.PlayerPosition("Alice")
	cx, cz := world.ChunkOf(p.X), world.ChunkOf(p.Z)
	far := world.ChunkPos{X: cx + 7, Z: cz + 7}
	if dx, dz := float64(far.X*16+8)-p.X, float64(far.Z*16+8)-p.Z; math.Hypot(dx, dz) <= 128 {
		t.Fatalf("chunk %v is within 128 blocks of the player", far)
	}
	servertest.UntilTicks(t, srv, 200, "far chunk loaded", func() bool {
		return srv.ChunkLoadedInDimension("minecraft:overworld", far.X*16, far.Z*16)
	})
	if !slices.Contains(srv.RandomTickChunkPositions(), far) {
		t.Fatalf("chunk %v beyond 128 blocks is not random-ticked", far)
	}
}
