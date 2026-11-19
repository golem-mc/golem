package plugins_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func awaitPluginIO(t *testing.T, srv *server.Server, what string, ready func() bool) {
	t.Helper()
	// Accelerated held ticks can exhaust the budget before background I/O
	// gets CPU time. Await workers on the normal clock, then hold again.
	servertest.ReleaseTicks(t, srv)
	defer servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 100, what, ready)
}

func loadPluginChunk(t *testing.T, srv *server.Server, h plugin.Server, run func(func()), pos plugin.ChunkPos, generate bool, visit func(plugin.Chunk)) {
	t.Helper()
	var finished atomic.Bool
	var requested int64
	run(func() {
		requested = h.Ticks().Current()
		h.Overworld().LoadChunk(pos, generate, func(c plugin.Chunk) {
			if h.Ticks().Current() <= requested {
				t.Error("chunk callback ran synchronously or outside a later tick")
			}
			visit(c)
			finished.Store(true)
		})
	})
	awaitPluginIO(t, srv, "plugin chunk callback", finished.Load)
}

// vanilla: ServerChunkCache.getChunk, LevelChunk's saved state and chunk holds.
func TestPluginLoadChunkSaveReload(t *testing.T) {
	t.Parallel()
	srv, h, run, c := worldEffectsPlugin(t)
	w := srv.OverworldWorld()
	pos := plugin.ChunkPos{X: 30, Z: -30}
	native := world.ChunkPos{X: 30, Z: -30}
	var first plugin.Chunk
	key := plugin.Key{"test", "loaded"}
	loadPluginChunk(t, srv, h, run, pos, false, func(chunk plugin.Chunk) {
		if chunk != nil || w.ChunkLoaded(native) || w.HasChunk(native) {
			t.Error("generate=false created missing terrain")
		}
	})
	loadPluginChunk(t, srv, h, run, pos, true, func(chunk plugin.Chunk) {
		if chunk == nil {
			t.Error("generation failed")
			return
		}
		first = chunk
		if chunk.World() != h.Overworld() || chunk.Pos() != pos {
			t.Error("wrong generated chunk identity")
		}
		if loaded, ok := h.Overworld().Chunk(pos); !ok || loaded != chunk {
			t.Error("callback did not hold its live chunk")
		}
		if got := h.Overworld().BlockState(plugin.BlockPos{X: pos.X * 16, Y: 63, Z: pos.Z * 16}); got.Block() != registry.BlockStone {
			t.Errorf("generated flat terrain=%v", got.Block())
		}
		chunk.Data().Set(key, int64(42))
		// This also detects invoking callbacks while holding the world lock.
		h.Overworld().Save(true)
	})
	awaitPluginIO(t, srv, "callback chunk release", func() bool { return !w.ChunkLoaded(native) })
	w.DrainSaves()
	if !w.HasChunk(native) {
		t.Fatal("generated chunk was not saved")
	}
	loadPluginChunk(t, srv, h, run, pos, false, func(chunk plugin.Chunk) {
		if chunk == nil {
			t.Error("saved chunk could not be loaded without generation")
			return
		}
		value, ok := chunk.Data().Get(key)
		if chunk == first || !ok || value != int64(42) {
			t.Error("load did not restore saved data in a fresh handle")
		}
	})
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayChunkData {
			r := protocol.NewReader(data)
			if r.ReadI32() == native.X && r.ReadI32() == native.Z {
				t.Error("loading distant terrain sent an unsolicited client chunk")
			}
		}
		return false
	})
}

func TestPluginLoadChunkLoadedAndInvalid(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	native := world.ChunkPos{X: 30, Z: 30}
	w.Acquire(native)
	defer w.Release(native)
	pos := plugin.ChunkPos{X: 30, Z: 30}
	var original plugin.Chunk
	run(func() { original, _ = h.Overworld().Chunk(pos) })
	for _, generate := range []bool{false, true} {
		loadPluginChunk(t, srv, h, run, pos, generate, func(c plugin.Chunk) {
			if c != original {
				t.Error("loading a held chunk changed its identity")
			}
		})
	}
	for _, generate := range []bool{false, true} {
		loadPluginChunk(t, srv, h, run, plugin.ChunkPos{X: 30 + 1<<32, Z: 30}, generate, func(c plugin.Chunk) {
			if c != nil {
				t.Error("invalid coordinate wrapped into a loaded chunk")
			}
		})
	}
	if !w.ChunkLoaded(native) {
		t.Fatal("callback release consumed another holder's reference")
	}
}

// vanilla: ServerChunkCache.removeTicketWithRadius.
func TestPluginChunkTicketsBelongToTheirPlugin(t *testing.T) {
	t.Parallel()
	var a, b, c plugin.Server
	shared := plugin.ChunkPos{X: 40, Z: 40}
	failed := plugin.ChunkPos{X: 60, Z: 40}
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{
		{Name: "a", Plugin: &testPlugin{enable: func(s plugin.Server) error { a = s; s.Overworld().AddTicket(shared, 1); return nil }}},
		{Name: "b", Plugin: &testPlugin{enable: func(s plugin.Server) error { b = s; s.Overworld().AddTicket(shared, 1); return nil }}},
		{Name: "c", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			c = s
			s.Overworld().AddTicket(failed, 1)
			return errors.New("enable failed")
		}}},
	})
	servertest.HoldTicks(t, srv)
	w := srv.OverworldWorld()
	at := func(p plugin.ChunkPos) world.ChunkPos { return world.ChunkPos{X: int32(p.X), Z: int32(p.Z)} }
	if w.ChunkTicketed(at(failed)) {
		t.Fatal("a plugin that failed to enable kept its ticket")
	}
	// Unmatched removals, by a plugin that holds nothing or already released.
	c.Overworld().RemoveTicket(shared, 1)
	a.Overworld().RemoveTicket(shared, 1)
	a.Overworld().RemoveTicket(shared, 1)
	if !w.ChunkTicketed(at(shared)) {
		t.Fatal("one plugin released another's ticket")
	}
	b.Overworld().RemoveTicket(shared, 1)
	if w.AnyChunkTicketed() {
		t.Fatal("the last holder's removal left the ticket")
	}
	a.Overworld().AddTicket(shared, 1)
	srv.Shutdown()
	if w.AnyChunkTicketed() {
		t.Fatal("a disabled plugin kept its ticket")
	}
}
