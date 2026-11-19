package storage

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestFlightPastGenerationBacklog parks missing terrain at view distance 32.
// A move beyond that view must still deliver ready chunks in acknowledged
// batches, and leave the tick loop and player interactions running.
func TestFlightPastGenerationBacklog(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = 32
	cfg.SimulationDistance = 2
	srv, c := servertest.Dial(t, cfg)
	srv.SetDropUntouchedChunks(true)
	c.ViewAsk = 2
	c.Join("Flier")
	c.Settle()
	servertest.HoldTicks(t, srv)

	// Hold a small ready destination beyond the old view. The rest of both
	// view-distance-32 areas remain missing terrain.
	const target = 100
	for x := int32(target - 2); x <= target+2; x++ {
		for z := int32(-2); z <= 2; z++ {
			release := srv.HoldChunk(x*16, z*16)
			defer release()
		}
	}
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	started := make(chan struct{}, 1)
	var generated atomic.Int32
	var generatedPositions sync.Map
	srv.OverworldWorld().SetChunkGenerationGate(func(pos world.ChunkPos) {
		generated.Add(1)
		generatedPositions.Store(pos, true)
		select {
		case started <- struct{}{}:
		default:
		}
		<-gate
	})
	setRenderDistance(c, 32)
	c.Settle()
	awaitBeats(t, srv, 2)
	if len(started) == 0 {
		t.Fatal("missing terrain did not reach generation")
	}
	if n := srv.ChunksPending(); n < 4000 {
		t.Fatalf("backlog = %d, want a view-distance-32 backlog", n)
	}

	c.NoBatchAcks = true
	acknowledge(c, 4)
	c.MoveTo(target*16+0.5, 80, 0.5, 0, 0)
	c.SettleKeeping()
	c.ExpectWithinTicks(srv, 10, protocol.IDPlayCenterChunk)
	if n := expectBatch(c); n != 4 {
		t.Fatalf("first ready batch = %d chunks, want 4", n)
	}
	acknowledge(c, 4)
	if n := expectBatch(c); n != 4 {
		t.Fatalf("acknowledged ready batch = %d chunks, want 4", n)
	}
	c.Chat("still flying")
	c.SettleKeeping()
	servertest.RunTicks(t, srv, 3)
	c.ExpectChat("Flier", "still flying")

	// A smaller view drops queued work. Only running acquisitions may finish;
	// cancelled queued work must never reach the generator.
	setRenderDistance(c, 2)
	c.SettleKeeping()
	awaitBeats(t, srv, 2)
	release()
	srv.OverworldWorld().FlushLighting()
	for beats := 0; beats < 100 && srv.ChunksPending() > 0; beats++ {
		awaitBeats(t, srv, 1)
	}
	if n := srv.ChunksPending(); n != 0 {
		t.Fatalf("%d acquisitions did not drain after generation resumed", n)
	}
	if n := generated.Load(); n > 8 {
		t.Errorf("generated %d obsolete chunks, want at most 8 running workers", n)
	}
	generatedPositions.Range(func(key, _ any) bool {
		if pos := key.(world.ChunkPos); srv.OverworldWorld().ChunkLoaded(pos) {
			t.Errorf("departed chunk %v is still held", pos)
		}
		return true
	})
}
