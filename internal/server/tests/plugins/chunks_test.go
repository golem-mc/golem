package plugins_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
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

func TestPluginLoadChunkCallbackPanicReleasesHold(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 31, Z: 30}
	var called atomic.Bool
	run(func() {
		h.Overworld().LoadChunk(plugin.ChunkPos{X: 31, Z: 30}, true, func(plugin.Chunk) {
			called.Store(true)
			panic("deliberate chunk callback failure")
		})
	})
	awaitPluginIO(t, srv, "panicking callback", called.Load)
	awaitPluginIO(t, srv, "panicking callback's hold release", func() bool { return !w.ChunkLoaded(pos) })
	run(func() {}) // The plugin's tick scheduler survives its callback's panic.
}

func TestPluginLoadChunkDisableReleasesHold(t *testing.T) {
	var h plugin.Server
	started, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(finish) }) }
	defer unblock()
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "chunks", Plugin: &testPlugin{
		enable:  func(s plugin.Server) error { h = s; return nil },
		disable: unblock,
	}}})
	servertest.HoldTicks(t, srv)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 31, Z: 31}
	w.SetChunkGenerationGate(func(p world.ChunkPos) {
		if p == pos {
			close(started)
			<-finish
		}
	})
	h.Scheduler().RunTask(func() {
		h.Overworld().LoadChunk(plugin.ChunkPos{X: 31, Z: 31}, true, func(plugin.Chunk) { t.Error("disabled plugin received a chunk callback") })
	})
	awaitPluginIO(t, srv, "generation worker", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})
	// Disable releases the generator; cleanup must then release its completed
	// hold even though the callback's tick task has been cancelled.
	srv.Shutdown()
	if w.ChunkLoaded(pos) {
		t.Fatal("disabled plugin retained its chunk hold")
	}
}

func TestPluginLoadChunkConcurrentCallbacks(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	pos := plugin.ChunkPos{X: 32, Z: 32}
	key := plugin.Key{"test", "callbacks"}
	var completed atomic.Int32
	run(func() {
		// More requests than async workers also exercises workers waiting for
		// tick callbacks without starving requests queued behind them.
		for range 12 {
			h.Overworld().LoadChunk(pos, true, func(chunk plugin.Chunk) {
				if chunk == nil {
					t.Error("concurrent generation failed")
					return
				}
				value, _ := plugin.GetData[int64](chunk.Data(), key)
				if value != int64(completed.Load()) {
					t.Errorf("callback data=%d, completed=%d", value, completed.Load())
				}
				chunk.Data().Set(key, int64(completed.Load()+1))
				completed.Add(1)
			})
		}
	})
	awaitPluginIO(t, srv, "concurrent chunk callbacks", func() bool { return completed.Load() == 12 })
	native := world.ChunkPos{X: 32, Z: 32}
	awaitPluginIO(t, srv, "concurrent holds released", func() bool { return !w.ChunkLoaded(native) })
	w.DrainSaves()
	loadPluginChunk(t, srv, h, run, pos, false, func(chunk plugin.Chunk) {
		if chunk == nil {
			t.Error("concurrently edited chunk was not saved")
			return
		}
		if value, ok := plugin.GetData[int64](chunk.Data(), key); !ok || value != 12 {
			t.Error("concurrent callbacks lost saved data")
		}
	})
}

func TestPluginLoadChunkWakesSavedEntitiesBeforeCallback(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 33, Z: 33}
	w.Acquire(pos)
	w.MarkDirty(pos)
	w.Release(pos)
	w.DrainSaves()
	identity := uuid.New()
	if err := w.SaveChunkEntities(pos, []nbt.Tag{nbt.Compound{
		"id": nbt.String("minecraft:cow"), "UUID": nbt.UUIDToIntArray(identity),
		"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(528.5), nbt.Double(64), nbt.Double(528.5)}},
		"NoAI": nbt.Byte(1), "PersistenceRequired": nbt.Byte(1),
	}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		loadPluginChunk(t, srv, h, run, plugin.ChunkPos{X: 33, Z: 33}, false, func(chunk plugin.Chunk) {
			found := 0
			for _, mob := range srv.MobsInDimension("minecraft:overworld") {
				if mob.UUID == identity.String() {
					found++
				}
			}
			if chunk == nil || found != 1 {
				t.Errorf("callback saw %d saved entities, want exactly one", found)
			}
		})
	}
}

func ticketCentre(radius int) plugin.ChunkPos { return plugin.ChunkPos{X: 40 + 10*radius, Z: 40} }

// vanilla: ServerChunkCache.addTicketWithRadius (level 33 - radius), ChunkLevel.isBlockTicking.
func TestPluginChunkTicketRadii(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	run(func() {
		o := h.Overworld()
		o.AddTicket(ticketCentre(0), -1)
		o.AddTicket(ticketCentre(0), world.MaxTicketRadius+1)
		for r := range 3 {
			o.AddTicket(ticketCentre(r), r)
		}
		o.RemoveTicket(ticketCentre(0), 1)
	})
	for r := range 3 {
		c := ticketCentre(r)
		for dz := -4; dz <= 4; dz++ {
			for dx := -4; dx <= 4; dx++ {
				d := max(dx, -dx, dz, -dz)
				pos := world.ChunkPos{X: int32(c.X + dx), Z: int32(c.Z + dz)}
				if got := w.ChunkLoaded(pos); got != (d <= r) {
					t.Errorf("radius %d: chunk at distance %d loaded = %v", r, d, got)
				}
				if got := w.ChunkTicketed(pos); got != (d <= r-1) {
					t.Errorf("radius %d: chunk at distance %d ticking = %v", r, d, got)
				}
			}
		}
	}
	run(func() {
		for r := range 3 {
			h.Overworld().RemoveTicket(ticketCentre(r), r)
		}
	})
	if w.AnyChunkTicketed() {
		t.Fatal("ticket survived its removal")
	}
	awaitPluginIO(t, srv, "ticket holds released", func() bool { return !w.ChunkLoaded(world.ChunkPos{X: 50, Z: 40}) })
}

// vanilla: ServerChunkCache.addTicketWithRadius, PersistentEntitySectionManager.addEntityWithoutEvent.
func TestPluginChunkTicketWakesSavedEntities(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 33, Z: 33}
	w.Acquire(pos)
	w.MarkDirty(pos)
	w.Release(pos)
	w.DrainSaves()
	identity := uuid.New()
	if err := w.SaveChunkEntities(pos, []nbt.Tag{nbt.Compound{
		"id": nbt.String("minecraft:cow"), "UUID": nbt.UUIDToIntArray(identity),
		"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(528.5), nbt.Double(64), nbt.Double(528.5)}},
		"NoAI": nbt.Byte(1), "PersistenceRequired": nbt.Byte(1),
	}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		run(func() { h.Overworld().AddTicket(plugin.ChunkPos{X: 33, Z: 33}, 1) })
		found := 0
		for _, mob := range srv.MobsInDimension("minecraft:overworld") {
			if mob.UUID == identity.String() {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("%d saved cows after the ticket, want exactly one", found)
		}
	}
}

// vanilla: ChunkLevel.isEntityTicking, ServerLevel.tick (entityTickList).
func TestPluginChunkTicketEntityTicking(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	// A radius 2 ticket at 33 entity-ticks 33 and block-ticks 32..34; radius
	// 1 at 37 and radius 0 at 41 entity-tick nothing.
	cows := map[world.ChunkPos]uuid.UUID{}
	for _, x := range []int32{33, 34, 37, 41} {
		pos := world.ChunkPos{X: x, Z: 33}
		w.Acquire(pos)
		w.MarkDirty(pos)
		w.Release(pos)
		cows[pos] = uuid.New()
	}
	w.DrainSaves()
	for pos, id := range cows {
		if err := w.SaveChunkEntities(pos, []nbt.Tag{nbt.Compound{
			"id": nbt.String("minecraft:cow"), "UUID": nbt.UUIDToIntArray(id),
			"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(float64(pos.X)*16 + 8.5), nbt.Double(120), nbt.Double(536.5)}},
			"PersistenceRequired": nbt.Byte(1),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	servertest.HoldTicks(t, srv)
	run(func() {
		o := h.Overworld()
		o.AddTicket(plugin.ChunkPos{X: 33, Z: 33}, 2)
		o.AddTicket(plugin.ChunkPos{X: 37, Z: 33}, 1)
		o.AddTicket(plugin.ChunkPos{X: 41, Z: 33}, 0)
	})
	servertest.RunTicks(t, srv, 5)
	y := map[string]float64{}
	for _, mob := range srv.MobsInDimension("minecraft:overworld") {
		y[mob.UUID] = mob.Y
	}
	for pos, id := range cows {
		got, ok := y[id.String()]
		if !ok {
			t.Fatalf("cow of chunk %d not woken", pos.X)
		}
		if falls := pos.X == 33; falls != (got < 120) {
			t.Errorf("cow of chunk %d at Y=%v: falling = %v, want %v", pos.X, got, got < 120, falls)
		}
	}
}

// vanilla: ChunkLevel.isBlockTicking, ServerLevel.tickChunk.
func TestPluginChunkTicketRandomTicks(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	w := srv.OverworldWorld()
	copper, ok := registry.FindState("minecraft:copper_block", nil)
	if !ok {
		t.Fatal("no copper block state")
	}
	run(func() { h.Overworld().AddTicket(plugin.ChunkPos{X: 40, Z: 40}, 2) })
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 4096)
	fill := func(cx int32) {
		for x := cx * 16; x < cx*16+16; x++ {
			for z := int32(640); z < 656; z++ {
				srv.SetBlock(x, 64, z, copper)
			}
		}
	}
	// Chunk 40 is a ticking centre, 42 loaded but out of the ticking ring.
	fill(40)
	fill(42)
	servertest.UntilTicks(t, srv, 200, "copper weathering in the ticketed chunk", func() bool {
		for x := int32(640); x < 656; x++ {
			for z := int32(640); z < 656; z++ {
				if w.BlockAt(x, 64, z) != copper {
					return true
				}
			}
		}
		return false
	})
	for x := int32(672); x < 688; x++ {
		for z := int32(640); z < 656; z++ {
			if w.BlockAt(x, 64, z) != copper {
				t.Fatal("a chunk outside the ticking ring took a random tick")
			}
		}
	}
}

// vanilla: ServerChunkCache.removeTicketWithRadius.
func TestPluginChunkTicketsBelongToTheirPlugin(t *testing.T) {
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

// vanilla: ChunkLevel.isEntityTicking, ServerLevel.tick (entityTickList).
func TestPluginChunkTicketHoldsItemsAndTNT(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	// A radius 2 ticket at 33 entity-ticks chunk 33; radius 0 at 41 ticks nothing.
	chunks := []int32{33, 41}
	for _, x := range chunks {
		pos := world.ChunkPos{X: x, Z: 33}
		w.Acquire(pos)
		w.MarkDirty(pos)
		w.Release(pos)
	}
	w.DrainSaves()
	for _, x := range chunks {
		at := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(float64(x)*16 + 8.5), nbt.Double(120), nbt.Double(536.5)}}
		if err := w.SaveChunkEntities(world.ChunkPos{X: x, Z: 33}, []nbt.Tag{
			nbt.Compound{
				"id": nbt.String("minecraft:item"), "UUID": nbt.UUIDToIntArray(uuid.New()), "Pos": at,
				"Item": nbt.Compound{"id": nbt.String("minecraft:dirt"), "count": nbt.Int(1)},
			},
			nbt.Compound{
				"id": nbt.String("minecraft:tnt"), "UUID": nbt.UUIDToIntArray(uuid.New()), "Pos": at,
				"fuse": nbt.Short(80),
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	servertest.HoldTicks(t, srv)
	run(func() {
		o := h.Overworld()
		o.AddTicket(plugin.ChunkPos{X: 33, Z: 33}, 2)
		o.AddTicket(plugin.ChunkPos{X: 41, Z: 33}, 0)
	})
	servertest.RunTicks(t, srv, 5)
	chunkOf := func(x float64) int32 { return int32(x) >> 4 }
	items, tnts := 0, 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		items++
		if still := it.Age == 0 && it.Y > 120; still != (chunkOf(it.X) == 41) {
			t.Errorf("item of chunk %d at Y=%v age %d", chunkOf(it.X), it.Y, it.Age)
		}
	}
	for _, it := range srv.TNTsInDimension("minecraft:overworld") {
		tnts++
		if still := it.Y == 120 && it.Fuse == 80; still != (chunkOf(it.X) == 41) {
			t.Errorf("TNT of chunk %d at Y=%v fuse %d", chunkOf(it.X), it.Y, it.Fuse)
		}
	}
	if items != 2 || tnts != 2 {
		t.Fatalf("woke %d items and %d TNT, want 2 each", items, tnts)
	}
}

// vanilla: ServerLevel.tickNonPassenger gates Entity.handlePortal as well as physics.
func TestPluginChunkTicketHoldsPortalTravel(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 33, Z: 33}
	w.Acquire(pos)
	w.SetBlock(536, 120, 536, registry.BlockEndPortal.DefaultState())
	w.MarkDirty(pos)
	w.Release(pos)
	w.DrainSaves()
	var saved []nbt.Tag
	for _, kind := range []string{"item", "tnt", "cow"} {
		tag := nbt.Compound{
			"id": nbt.String("minecraft:" + kind), "UUID": nbt.UUIDToIntArray(uuid.New()),
			"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(536.5), nbt.Double(120), nbt.Double(536.5)}},
			"Item": nbt.Compound{"id": nbt.String("minecraft:dirt"), "count": nbt.Int(1)},
			"fuse": nbt.Short(80), "PersistenceRequired": nbt.Byte(1),
		}
		saved = append(saved, tag)
	}
	if err := w.SaveChunkEntities(pos, saved); err != nil {
		t.Fatal(err)
	}
	run(func() { h.Overworld().AddTicket(plugin.ChunkPos{X: 33, Z: 33}, 0) })
	servertest.RunTicks(t, srv, 3)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Age != 0 || items[0].X != 536.5 {
		t.Fatalf("dormant item used portal or aged: %+v", items)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 1 {
		t.Fatalf("dormant TNT used portal: %d left", n)
	}
	if n := len(srv.MobsInDimension(world.DimensionName)); n != 1 {
		t.Fatalf("dormant cow used portal: %d left", n)
	}
	if len(srv.ItemsInDimension(world.EndName))+len(srv.TNTsInDimension(world.EndName))+len(srv.MobsInDimension(world.EndName)) != 0 {
		t.Fatal("dormant entities appeared in the End")
	}
	run(func() { h.Overworld().AddTicket(plugin.ChunkPos{X: 33, Z: 33}, 2) })
	servertest.RunTicks(t, srv, 3)
	if len(srv.ItemsInDimension(world.DimensionName))+len(srv.TNTsInDimension(world.DimensionName))+len(srv.MobsInDimension(world.DimensionName)) != 0 {
		t.Fatal("entities did not use portal when entity ticking resumed")
	}
	if len(srv.ItemsInDimension(world.EndName)) != 1 || len(srv.TNTsInDimension(world.EndName)) != 1 || len(srv.MobsInDimension(world.EndName)) != 1 {
		t.Fatal("resumed entities did not arrive in the End")
	}
}

// vanilla: ChunkMap.tick updates pairing independently of entity simulation.
func TestPluginChunkTicketDormantEntitiesTrackViewers(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 4, 2
	srv, h, run := worldPlugin(t, cfg)
	servertest.OpsListing(t, "Viewer")(srv)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.ViewAsk = 4
	c.Join("Viewer")
	servertest.HoldTicks(t, srv)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 33, Z: 33}
	w.Acquire(pos)
	w.MarkDirty(pos)
	w.Release(pos)
	w.DrainSaves()
	at := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(536.5), nbt.Double(120), nbt.Double(536.5)}}
	if err := w.SaveChunkEntities(pos, []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:item"), "UUID": nbt.UUIDToIntArray(uuid.New()), "Pos": at,
			"Item": nbt.Compound{"id": nbt.String("minecraft:dirt"), "count": nbt.Int(1)}},
		nbt.Compound{"id": nbt.String("minecraft:tnt"), "UUID": nbt.UUIDToIntArray(uuid.New()), "Pos": at, "fuse": nbt.Short(80)},
	}); err != nil {
		t.Fatal(err)
	}
	run(func() { h.Overworld().AddTicket(plugin.ChunkPos{X: 33, Z: 33}, 0) })
	items, tnts := srv.ItemsInDimension(world.DimensionName), srv.TNTsInDimension(world.DimensionName)
	if len(items) != 1 || len(tnts) != 1 {
		t.Fatal("ticket did not wake the saved entities")
	}
	want := map[int32]bool{items[0].ID: false, tnts[0].ID: false}
	move := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		c.ExpectWithinTicks(srv, 0, protocol.IDPlaySyncPosition)
	}
	// Chunk 30 is outside simulation distance 2, within view/tracking range.
	move("tp @s 488.5 120 536.5")
	if !c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity {
			if entityID := protocol.NewReader(data).ReadVarInt(); !want[entityID] {
				if _, ok := want[entityID]; ok {
					want[entityID] = true
				}
			}
		}
		return want[items[0].ID] && want[tnts[0].ID]
	}) {
		t.Fatalf("dormant entities not paired: %v", want)
	}
	c.ExpectNoneWithinTicks(srv, 3, func(id int32, data []byte) bool {
		return id == protocol.IDPlaySpawnEntity && (protocol.NewReader(data).ReadVarInt() == items[0].ID || protocol.NewReader(data).ReadVarInt() == tnts[0].ID)
	})
	move("tp @s 424.5 120 536.5")
	removed := map[int32]bool{}
	if !c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRemoveEntities {
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0; n-- {
				removed[r.ReadVarInt()] = true
			}
		}
		return removed[items[0].ID] && removed[tnts[0].ID]
	}) {
		t.Fatalf("dormant entities not unpaired: %v", removed)
	}
	if after := srv.ItemsInDimension(world.DimensionName); len(after) != 1 || after[0].Age != items[0].Age || after[0].Y != items[0].Y {
		t.Fatalf("tracking advanced dormant item: %+v", after)
	}
	if after := srv.TNTsInDimension(world.DimensionName); len(after) != 1 || after[0].Fuse != tnts[0].Fuse || after[0].Y != tnts[0].Y {
		t.Fatalf("tracking advanced dormant TNT: %+v", after)
	}
}
