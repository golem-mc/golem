package plugins_test

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Level.setBlock, LevelChunk.setBlockState.
func TestPluginBlockWriteBoundsAndIdentity(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	p := world.ChunkPos{X: -2, Z: 3}
	w.Acquire(p)
	defer w.Release(p)
	run(func() {
		pw := h.Overworld()
		pos := plugin.BlockPos{X: -27, Y: 100, Z: 53}
		stone := plugin.DefaultState(registry.BlockStone)
		if !pw.Block(pos).Set(stone) || pw.BlockState(pos) != stone {
			t.Error("Block.Set did not write the loaded cell")
		}
		if pw.SetBlockState(pos, stone, plugin.UpdateAll) {
			t.Error("unchanged write succeeded")
		}
		for _, bad := range []plugin.BlockPos{
			{X: pos.X + 1<<32, Y: pos.Y, Z: pos.Z},
			{X: pos.X, Y: pw.MinY() - 1, Z: pos.Z},
			{X: pos.X, Y: pw.MinY() + pw.Height(), Z: pos.Z},
			{X: 30000000, Y: 100}, {X: -30000001, Y: 100},
			{X: 2000000, Y: 100, Z: 2000000},
		} {
			if pw.SetBlockState(bad, stone, plugin.UpdateAll) {
				t.Errorf("invalid write at %v succeeded", bad)
			}
		}
		if pw.SetBlockState(pos, plugin.BlockState(registry.StateCount), plugin.UpdateAll) {
			t.Error("invalid state succeeded")
		}
		if w.ChunkLoaded(world.ChunkPos{X: 125000, Z: 125000}) {
			t.Error("write loaded missing chunk")
		}
		// A property edit retains the live block entity and its private data.
		sign := plugin.DefaultState(registry.BlockOakSign)
		if !pw.SetBlockState(pos, sign, plugin.UpdateClients|plugin.UpdateKnownShape|plugin.UpdateSkipOnPlace) {
			t.Error("sign write failed")
		}
		be, ok := pw.BlockEntity(pos)
		if !ok {
			t.Error("write did not create block entity")
			return
		}
		key := plugin.Key{Namespace: "test", Path: "write"}
		be.Data().Set(key, int64(42))
		rotated, _ := sign.With(registry.PropRotation, "1")
		pw.SetBlockState(pos, rotated, plugin.UpdateClients|plugin.UpdateKnownShape|plugin.UpdateSkipOnPlace)
		if got, ok := pw.BlockEntity(pos); !ok || got != be || !be.IsValid() {
			t.Error("property edit replaced block entity")
		}
		pw.Block(pos).Set(stone)
		if be.IsValid() {
			t.Error("replacement retained old block entity")
		}
		if _, ok := pw.BlockEntity(pos); ok {
			t.Error("stone retained block entity")
		}
		// LevelChunk also creates a missing block entity on a property write.
		missing := pos.Offset(plugin.North, 3)
		w.SetBlock(int32(missing.X), int32(missing.Y), int32(missing.Z), uint16(sign))
		pw.SetBlockState(missing, rotated, plugin.UpdateKnownShape|plugin.UpdateSkipOnPlace)
		if _, ok := pw.BlockEntity(missing); !ok {
			t.Error("property write did not create missing block entity")
		}
		for _, blocks := range [][2]registry.Block{{registry.BlockCopperChest, registry.BlockExposedCopperChest}, {registry.BlockCopperGolemStatue, registry.BlockWaxedCopperGolemStatue}} {
			pw.SetBlockState(pos, plugin.DefaultState(blocks[0]), plugin.UpdateAll)
			be, ok := pw.BlockEntity(pos)
			if !ok {
				t.Errorf("no block entity for %v", blocks[0])
				continue
			}
			be.Data().Set(key, int64(9))
			pw.SetBlockState(pos, plugin.DefaultState(blocks[1]), plugin.UpdateAll)
			after, ok := pw.BlockEntity(pos)
			if !ok || after != be || !be.IsValid() {
				t.Error("compatible copper variant replaced its block entity")
			}
			if value, ok := plugin.GetData[int64](be.Data(), key); !ok || value != 9 {
				t.Error("copper variant lost private data")
			}
		}
	})
}

func TestPluginBlockWriteClientFlags(t *testing.T) {
	t.Parallel()
	srv, h, run, c := worldEffectsPlugin(t)
	p := srv.PlayerPosition("Effects")
	x, z := int(p.X), int(p.Z)
	// Keep all writes in the chunk the client has actually received.
	x, z = x&^15|7, z&^15|7
	var visible = map[plugin.BlockPos]plugin.BlockState{}
	var hidden = map[plugin.BlockPos]bool{}
	run(func() {
		for i, flags := range []plugin.UpdateFlags{
			0, plugin.UpdateInvisible, plugin.UpdateImmediate,
			plugin.UpdateClients, plugin.UpdateClients | plugin.UpdateInvisible,
			plugin.UpdateClients | plugin.UpdateImmediate, plugin.UpdateAll,
		} {
			pos := plugin.BlockPos{X: x, Y: 80 + i*2, Z: z}
			state := plugin.DefaultState(registry.BlockStone)
			if !h.Overworld().SetBlockState(pos, state, flags) {
				t.Errorf("flags %d failed", flags)
			}
			if h.Overworld().BlockState(pos) != state {
				t.Errorf("flags %d did not write", flags)
			}
			if flags&plugin.UpdateClients != 0 {
				visible[pos] = state
			} else {
				hidden[pos] = true
			}
		}
	})
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		for _, ch := range servertest.BlockChangesIn(t, id, data) {
			pos := plugin.BlockPos{X: int(ch.X), Y: int(ch.Y), Z: int(ch.Z)}
			if hidden[pos] {
				t.Errorf("hidden write at %v reached client", pos)
			}
			if want, ok := visible[pos]; ok {
				if plugin.BlockState(ch.State) != want {
					t.Errorf("wrong state at %v", pos)
				}
				delete(visible, pos)
			}
		}
		return false
	})
	if len(visible) != 0 {
		t.Errorf("missing client updates: %v", visible)
	}
}

func TestPluginBlockWritePlacementAndWireFlags(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	run(func() {
		pw := h.Overworld()
		for i, flags := range []plugin.UpdateFlags{plugin.UpdateKnownShape, plugin.UpdateKnownShape | plugin.UpdateSkipOnPlace} {
			y := int32(90 + i*4)
			w.SetBlock(6, y, 6, registry.BlockRedstoneBlock.DefaultState())
			pos := plugin.BlockPos{X: 7, Y: int(y), Z: 6}
			pw.SetBlockState(pos, plugin.DefaultState(registry.BlockHopper), flags)
			if got := registry.StateBool(uint16(pw.BlockState(pos)), registry.PropEnabled); got != (flags&plugin.UpdateSkipOnPlace != 0) {
				t.Errorf("flags %d hopper enabled=%v", flags, got)
			}
		}
		// Skip-on-place does not disable neighborChanged for adjacent blocks.
		w.SetBlock(7, 100, 6, registry.BlockRedstoneLamp.DefaultState())
		pw.SetBlockState(plugin.BlockPos{X: 6, Y: 100, Z: 6}, plugin.DefaultState(registry.BlockRedstoneBlock), plugin.UpdateAll|plugin.UpdateSkipOnPlace|plugin.UpdateKnownShape)
		if !registry.StateBool(w.BlockAt(7, 100, 6), registry.PropLit) {
			t.Error("skip-on-place suppressed neighborChanged")
		}
		for i, flags := range []plugin.UpdateFlags{0, plugin.UpdateSkipShapeUpdateOnWire} {
			y := int32(110 + i*4)
			w.SetBlock(6, y-1, 6, registry.BlockStone.DefaultState())
			w.SetBlock(6, y, 6, registry.BlockRedstoneWire.DefaultState())
			pw.SetBlockState(plugin.BlockPos{X: 6, Y: int(y - 1), Z: 6}, plugin.DefaultState(registry.BlockAir), flags)
			wire := registry.BlockOf(w.BlockAt(6, y, 6)) == registry.BlockRedstoneWire
			if wire != (flags&plugin.UpdateSkipShapeUpdateOnWire != 0) {
				t.Errorf("flags %d kept unsupported wire=%v", flags, wire)
			}
		}
	})
}

func TestPluginBlockWriteRemovalEffects(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	for i, flags := range []plugin.UpdateFlags{0, plugin.UpdateSuppressDrops, plugin.UpdateSkipBlockEntitySideEffects} {
		y := 90 + i*4
		srv.RunLine(fmt.Sprintf(`setblock 6 %d 6 barrel{Items:[{Slot:0b,id:"minecraft:diamond",count:3}]}`, y))
		before := len(srv.ItemsInDimension("minecraft:overworld"))
		run(func() {
			pos := plugin.BlockPos{X: 6, Y: y, Z: 6}
			be, ok := h.Overworld().BlockEntity(pos)
			if !ok {
				t.Error("no barrel handle")
				return
			}
			if !h.Overworld().SetBlockState(pos, plugin.DefaultState(registry.BlockStone), flags) {
				t.Error("replacement failed")
			}
			if be.IsValid() {
				t.Error("barrel handle survived replacement")
			}
		})
		items := srv.ItemsInDimension("minecraft:overworld")[before:]
		count := 0
		for _, it := range items {
			if registry.Item(it.Stack.ItemID) == registry.ItemDiamond {
				count += int(it.Count)
			}
		}
		want := 3
		if flags&plugin.UpdateSkipBlockEntitySideEffects != 0 {
			want = 0
		}
		if count != want {
			t.Errorf("flags %d spilled %d diamonds, want %d", flags, count, want)
		}
	}
	// Flag 32 is cleared before a neighbor shape's destroyBlock, so the
	// unsupported torch still drops. No loot drops for the replaced stone.
	run(func() {
		w.SetBlock(10, 100, 10, registry.BlockStone.DefaultState())
		w.SetBlock(10, 101, 10, registry.BlockTorch.DefaultState())
		h.Overworld().SetBlockState(plugin.BlockPos{X: 10, Y: 100, Z: 10}, plugin.DefaultState(registry.BlockAir), plugin.UpdateSuppressDrops)
		if w.BlockAt(10, 101, 10) != registry.BlockAir.DefaultState() {
			t.Error("shape update kept unsupported torch")
		}
	})
	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if registry.Item(it.Stack.ItemID) == registry.ItemTorch {
			found = true
		}
	}
	if !found {
		t.Error("suppress-drops leaked into neighbor destruction")
	}
}

func TestPluginBlockWriteSaveReloadAndLight(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: -2, Z: 3}
	w.Acquire(pos)
	p := plugin.BlockPos{X: -27, Y: 100, Z: 53}
	key := plugin.Key{Namespace: "test", Path: "written"}
	run(func() {
		pw := h.Overworld()
		pw.SetBlockState(p, plugin.DefaultState(registry.BlockOakSign), plugin.UpdateAll)
		be, ok := pw.BlockEntity(p)
		if !ok {
			t.Error("no sign")
			return
		}
		be.Data().Set(key, int64(42))
		pw.SetBlockState(p.Offset(plugin.East, 1), plugin.DefaultState(registry.BlockGlowstone), 0)
		pw.Save(true)
	})
	w.FlushLighting()
	run(func() {
		if _, block := h.Overworld().Light(p.Offset(plugin.Up, 1)); block != 13 {
			t.Errorf("silent block light=%d, want 13", block)
		}
	})
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	defer w.Release(pos)
	run(func() {
		pw := h.Overworld()
		be, ok := pw.BlockEntity(p)
		if !ok {
			t.Error("sign lost on reload")
			return
		}
		if v, ok := plugin.GetData[int64](be.Data(), key); !ok || v != 42 {
			t.Error("new block entity data lost on reload")
		}
		if pw.BlockState(p).Block() != registry.BlockOakSign || pw.BlockState(p.Offset(plugin.East, 1)).Block() != registry.BlockGlowstone {
			t.Error("block writes lost on reload")
		}
	})
}

// A running loop relights a plugin's writes at the tick's flush.
// vanilla: LevelChunk.setBlockState, ThreadedLevelLightEngine.runUpdate.
func TestPluginBlockWriteRelightsAtFlush(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	p := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(p)
	defer w.Release(p)
	cell := plugin.BlockPos{X: 8, Y: 200, Z: 8}
	roof := func(state plugin.BlockState) {
		pw := h.Overworld()
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				pw.SetBlockState(plugin.BlockPos{X: cell.X + dx, Y: cell.Y + 1, Z: cell.Z + dz}, state, plugin.UpdateAll)
			}
		}
	}
	sky := func() (s int) {
		run(func() { s, _ = h.Overworld().Light(cell) })
		return s
	}
	if got := sky(); got != 15 {
		t.Fatalf("initial sky light %d, want 15", got)
	}
	// The write callback submits no relight; the flush at the tick's end
	// submits one for the chunk.
	set := func(what string, state plugin.BlockState) {
		t.Helper()
		before := srv.RelightsRun()
		run(func() {
			roof(state)
			if got := srv.RelightsRun(); got != before {
				t.Errorf("%s: %d relights submitted by the writes", what, got-before)
			}
		})
		if got := srv.RelightsRun() - before; got != 1 {
			t.Errorf("%s: flush submitted %d relights, want 1", what, got)
		}
		srv.WaitRelights()
	}
	set("roof", plugin.DefaultState(registry.BlockStone))
	// The workers' result is flushed by the next tick.
	if got := sky(); got >= 15 {
		t.Errorf("sky light under the roof %d, want below 15", got)
	}
	set("remove", plugin.DefaultState(registry.BlockAir))
	if got := sky(); got != 15 {
		t.Errorf("sky light after removal %d, want 15", got)
	}
}

// An empty, paused server skips the tick's flush, so the pause path books the
// plugin's pending relights itself.
// vanilla: MinecraftServer.tickServer, ThreadedLevelLightEngine.runUpdate
func TestPluginBlockWriteRelightsOnPausedTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.PauseWhenEmptySeconds = 1
	srv, h, run := worldPlugin(t, cfg)
	w := srv.OverworldWorld()
	p := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(p)
	defer w.Release(p)
	servertest.RunTicks(t, srv, 25)
	age := srv.WorldAge()
	servertest.RunTicks(t, srv, 1)
	if srv.WorldAge() != age {
		t.Fatal("the empty server did not pause")
	}
	cell := plugin.BlockPos{X: 8, Y: 200, Z: 8}
	before := srv.RelightsRun()
	run(func() {
		h.Overworld().SetBlockState(cell, plugin.DefaultState(registry.BlockGlowstone), plugin.UpdateAll)
	})
	if got := srv.RelightsRun() - before; got != 1 {
		t.Fatalf("paused tick submitted %d relights, want 1", got)
	}
	srv.WaitRelights()
	run(func() {
		if srv.WorldAge() != age {
			t.Error("the server resumed")
		}
		if _, block := h.Overworld().Light(cell.Offset(plugin.Up, 1)); block != 14 {
			t.Errorf("block light above the glowstone = %d, want 14", block)
		}
	})
}

// A write from the last tick-end event is relit before shutdown waits for the
// workers.
func TestPluginBlockWriteRelightsAtShutdown(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	p := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(p)
	defer w.Release(p)
	cell := plugin.BlockPos{X: 8, Y: 200, Z: 8}
	plugin.Listen(h, func(*plugin.ServerTickEndEvent) {
		h.Overworld().SetBlockState(cell, plugin.DefaultState(registry.BlockGlowstone), plugin.UpdateAll)
	})
	before := srv.RelightsRun()
	run(func() {})
	if got := srv.RelightsRun(); got != before {
		t.Fatalf("%d relights submitted before shutdown", got-before)
	}
	srv.Shutdown()
	if got := srv.RelightsRun() - before; got != 1 {
		t.Errorf("shutdown submitted %d relights, want 1", got)
	}
}
