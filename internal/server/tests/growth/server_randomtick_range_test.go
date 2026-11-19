package growth

import (
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
