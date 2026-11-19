package tickrhythm

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// lag is the wall time a lagging server spends in one tick, under the loop's
// overload threshold so the backlog is kept.
const lag = 1500 * time.Millisecond

// TestAutoSaveCountsTicksNotTime has a tick take 1.5 s of wall time: the
// auto-save is due after its tick count all the same, and not before.
// vanilla: MinecraftServer.tickServer, ticksUntilAutosave
func TestAutoSaveCountsTicksNotTime(t *testing.T) {
	t.Parallel()
	const every = 4
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0

	srv, c := servertest.Join(t, cfg, "Testificate")
	c.BreakBlock(0, 63, 0)
	c.ExpectBlockChange()
	servertest.HoldTicks(t, srv)
	srv.SetAutoSave(every * time.Second / 20)

	servertest.SkipTime(t, srv, lag)
	servertest.RunTicks(t, srv, every-1)
	if blockIsSaved(t, cfg) {
		t.Fatal("the change was saved before the auto-save ticks ran out")
	}
	servertest.RunTicks(t, srv, 1)
	if !blockIsSaved(t, cfg) {
		t.Fatalf("the change was not saved after %d ticks", every)
	}
	c.Disconnect()
}

func blockIsSaved(t *testing.T, cfg *config.Config) bool {
	t.Helper()
	w, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatalf("reopen world: %v", err)
	}
	defer w.Close()
	pos := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	defer w.Release(pos)
	return w.BlockAt(0, 63, 0) == 0
}

// TestRateLimitCountsTicksNotTime floods past rate-limit and has the
// server lag 1.5 s: the check still comes twenty ticks after the last one.
// vanilla: Connection.tick, tickSecond every 20 ticks
func TestRateLimitCountsTicksNotTime(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.RateLimit = 0 // off until the loop is held, so no tick counts before
	srv, c := servertest.Join(t, cfg, "Testificate")
	servertest.HoldTicks(t, srv)
	srv.Config.RateLimit = 50
	servertest.RunTicks(t, srv, 1) // takes the baseline

	for range 200 {
		c.Send(protocol.IDPlayMovementFlags, func(b *protocol.Buffer) { b.PutU8(0) })
	}
	c.Settle()
	servertest.SkipTime(t, srv, lag)
	c.ExpectNoneWithinTicks(srv, 19, servertest.PacketIs(protocol.IDPlayDisconnect))
	servertest.RunTicks(t, srv, 1)
	c.AwaitPacket(protocol.IDPlayDisconnect)
}
