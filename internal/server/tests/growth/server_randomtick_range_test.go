package growth

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
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

// vanilla: ChunkMap.forEachBlockTickingChunk, RedStoneOreBlock.randomTick
func TestRandomTicksRefreshLoadedAndTicketedChunks(t *testing.T) {
	srv := servertest.NewTestServer(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	w := srv.OverworldWorld()
	// With the seeded dice, one eligible pass reaches the ore in its section.
	w.SetIntRule(world.RuleRandomTickSpeed, 65536)

	pos := world.ChunkPos{X: 30, Z: -30}
	ticket := world.ChunkPos{X: 31, Z: -30}
	w.Acquire(ticket)
	defer w.Release(ticket)
	loaded := w.LoadedChunkPositions()
	wantLoaded := slices.Clone(loaded)
	prefix := world.ChunkPos{X: 99, Z: 99}
	appended := w.AppendLoadedChunkPositions([]world.ChunkPos{prefix})
	if len(appended) != len(loaded)+1 || appended[0] != prefix || !slices.Contains(appended[1:], ticket) {
		t.Fatalf("appending loaded chunks to [%v] returned %v, want the prefix followed by %v in any order", prefix, appended, loaded)
	}
	servertest.RunTicks(t, srv, 1)

	unlit := registry.BlockRedstoneOre.DefaultState()
	lit, _ := registry.WithBool(unlit, registry.PropLit, true)
	lightOre := func(p world.ChunkPos) {
		t.Helper()
		if !w.SetBlock(p.X*16, 80, p.Z*16, lit) {
			t.Fatalf("could not light the ore in chunk %v", p)
		}
	}
	tick := func() {
		t.Helper()
		srv.SetPhysicsSeed(1)
		servertest.RunTicks(t, srv, 1)
		if !slices.Equal(loaded, wantLoaded) {
			t.Fatalf("saved loaded positions changed from %v to %v", wantLoaded, loaded)
		}
	}
	checkOre := func(p world.ChunkPos, want uint16) {
		t.Helper()
		if got := w.BlockAt(p.X*16, 80, p.Z*16); got != want {
			t.Errorf("ore state in %v = %d, want %d", p, got, want)
		}
	}

	w.Acquire(pos)
	lightOre(pos)
	tick()
	checkOre(pos, lit)
	w.SetChunkForced(pos, true)
	tick()
	checkOre(pos, unlit)
	selected := srv.RandomTickChunkPositions()
	if !slices.Equal(selected, []world.ChunkPos{pos}) {
		t.Fatalf("forced chunk selection = %v, want [%v]", selected, pos)
	}
	w.SetChunkForced(pos, false)
	lightOre(pos)
	tick()
	checkOre(pos, lit)
	w.Release(pos)
	w.FlushSaves()
	if w.ChunkLoaded(pos) {
		t.Fatalf("released chunk %v stayed loaded", pos)
	}

	w.AddTicket(ticket, 1)
	lightOre(ticket)
	tick()
	checkOre(ticket, unlit)
	checkOre(pos, lit) // The ticket loads the outer ring without block ticking it.
	w.RemoveTicket(ticket, 1)
	lightOre(ticket)
	tick()
	checkOre(ticket, lit)

	if !slices.Equal(selected, []world.ChunkPos{pos}) {
		t.Errorf("saved random tick selection changed from [%v] to %v", pos, selected)
	}
}
