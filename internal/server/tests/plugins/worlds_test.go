package plugins_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// flatGen fills y 0..3 with stone and spawns at (8, 4, 8). Populate writes
// one block inside the chunk and one 16 blocks away, which is dropped.
type flatGen struct {
	populated    atomic.Int32
	panicOnChunk atomic.Bool
}

var stone = plugin.DefaultState(registry.BlockStone)
var gold = plugin.DefaultState(registry.BlockGoldBlock)

func (g *flatGen) Generate(c plugin.ChunkWriter, rng plugin.Random) {
	for x := range 16 {
		for z := range 16 {
			for y := range 4 {
				c.Set(x, y, z, stone)
			}
		}
	}
	if g.panicOnChunk.Load() {
		panic("generator failure")
	}
}

func (g *flatGen) Populate(r plugin.RegionWriter, pos plugin.ChunkPos, rng plugin.Random) {
	g.populated.Add(1)
	r.Set(plugin.BlockPos{X: pos.X * 16, Y: 10, Z: pos.Z * 16}, gold)
	r.Set(plugin.BlockPos{X: pos.X*16 + 16, Y: 11, Z: pos.Z * 16}, gold)
}

func (g *flatGen) SpawnPoint(plugin.World) plugin.Location {
	return plugin.Location{Pos: plugin.Vec3{X: 8, Y: 4, Z: 8}}
}

var flatKey = plugin.Key{Namespace: "test", Path: "flat"}

func flatOptions(g plugin.Generator) plugin.WorldOptions {
	return plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Seed: 99, Generator: g}
}

func createFlat(t *testing.T, run func(func()), h plugin.Server, g plugin.Generator) (w plugin.World) {
	t.Helper()
	run(func() {
		var err error
		if w, err = h.CreateWorld(flatOptions(g)); err != nil {
			t.Errorf("CreateWorld: %v", err)
		}
	})
	if w == nil {
		t.FailNow()
	}
	return w
}

func unloadFlat(t *testing.T, run func(func()), h plugin.Server, w plugin.World, save bool) (err error) {
	t.Helper()
	run(func() { err = h.UnloadWorld(w, save) })
	return err
}

// vanilla: MinecraftServer.createLevels
func TestPluginCreateWorldFlat(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	g := &flatGen{}
	w := createFlat(t, run, h, g)
	run(func() {
		if w.Key() != flatKey || w.Dimension() != plugin.MinecraftKey("overworld") || w.Seed() != 99 {
			t.Errorf("world = %v %v seed %d", w.Key(), w.Dimension(), w.Seed())
		}
		worlds := h.Worlds()
		if len(worlds) != 4 || worlds[3].Key() != flatKey {
			t.Errorf("Worlds() = %d entries, last %v", len(worlds), worlds[len(worlds)-1].Key())
		}
		if found, ok := h.World(flatKey); !ok || found.Key() != flatKey {
			t.Error("World(key) did not find the new world")
		}
		if sp := w.SpawnPoint(); sp.Pos.Block() != (plugin.BlockPos{X: 8, Y: 4, Z: 8}) {
			t.Errorf("spawn = %v", sp.Pos)
		}
		if again, err := h.CreateWorld(flatOptions(nil)); err != nil || again.Key() != flatKey {
			t.Errorf("same key: %v %v", again, err)
		}
		if _, err := h.CreateWorld(plugin.WorldOptions{Key: plugin.MinecraftKey("overworld"), Dimension: plugin.MinecraftKey("overworld"), Generator: g}); err == nil {
			t.Error("a custom generator for the overworld key was accepted")
		}
		if _, err := h.CreateWorld(plugin.WorldOptions{Key: plugin.Key{Namespace: "test", Path: "odd"}, Dimension: plugin.Key{Namespace: "test", Path: "nothing"}}); err == nil {
			t.Error("an unknown dimension type was accepted")
		}
	})
}

func TestPluginCreateWorldSeed(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var overworld int64
	var w plugin.World
	run(func() {
		overworld = h.Overworld().Seed()
		w, _ = h.CreateWorld(plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Generator: &flatGen{}})
		if w.Seed() != overworld {
			t.Errorf("seed 0 gave %d, want the overworld's %d", w.Seed(), overworld)
		}
	})
}

// vanilla: ServerLevel.save
// Not parallel: it flakes under the CPU contention of the parallel tests.
func TestPluginWorldSaveReload(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	w := createFlat(t, run, h, &flatGen{})
	var loaded atomic.Bool
	run(func() {
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) { loaded.Store(true) })
	})
	awaitPluginIO(t, srv, "flat chunk", loaded.Load)
	if got := srv.LoadBlockInDimension("test:flat", 1, 3, 1); got != uint16(registry.BlockStone.DefaultState()) {
		t.Fatalf("y=3 = %d, want stone", got)
	}
	if got := srv.LoadBlockInDimension("test:flat", 1, 4, 1); got != uint16(registry.BlockAir.DefaultState()) {
		t.Fatalf("y=4 = %d, want air", got)
	}
	if got := srv.LoadBlockInDimension("test:flat", 0, 10, 0); got != uint16(registry.BlockGoldBlock.DefaultState()) {
		t.Fatalf("populated block = %d, want gold inside the chunk", got)
	}
	if got := srv.LoadBlockInDimension("test:flat", 16, 11, 0); got == uint16(registry.BlockGoldBlock.DefaultState()) {
		t.Fatal("a populate write outside the chunk stuck")
	}
	run(func() {
		w.AddTicket(plugin.ChunkPos{}, 0)
		if !w.SetBlockState(plugin.BlockPos{X: 2, Y: 20, Z: 2}, gold, plugin.UpdateAll) {
			t.Error("the chunk is not loaded")
		}
		w.SetSpawnPoint(plugin.Location{World: w, Pos: plugin.Vec3{X: 3, Y: 5, Z: 3}, Yaw: 45, Pitch: -10})
	})
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	run(func() {
		if _, ok := h.World(flatKey); ok {
			t.Error("the world is still loaded after UnloadWorld")
		}
	})
	if _, err := os.Stat(filepath.Join(cfg.LevelName, "dimensions", "test", "flat", "region")); err != nil {
		t.Errorf("region folder: %v", err)
	}
	g := &flatGen{}
	w = createFlat(t, run, h, g)
	run(func() {
		if sp := w.SpawnPoint(); sp.Pos.Block() != (plugin.BlockPos{X: 3, Y: 5, Z: 3}) {
			t.Errorf("reloaded spawn = %v", sp.Pos)
		} else if sp.Yaw != 45 || sp.Pitch != -10 {
			t.Errorf("reloaded spawn angles = %v, %v", sp.Yaw, sp.Pitch)
		}
		if w.Seed() != 99 {
			t.Errorf("reloaded seed = %d", w.Seed())
		}
	})
	if got := srv.LoadBlockInDimension("test:flat", 2, 20, 2); got != uint16(registry.BlockGoldBlock.DefaultState()) {
		t.Fatalf("saved block = %d, want gold", got)
	}
	if g.populated.Load() != 0 {
		t.Error("a saved chunk was populated again")
	}
}

// vanilla: ServerPlayer.teleport, PlayerList.respawn
func TestPluginWorldTravelAndUnload(t *testing.T) {
	t.Parallel()
	srv, h, run, c := worldEffectsPlugin(t)
	w := createFlat(t, run, h, &flatGen{})
	typeID := int32(-1)
	if r, ok := registry.Get("minecraft:dimension_type"); ok {
		typeID = r.ID("minecraft:overworld")
	}
	expectRespawn := func(name string) {
		t.Helper()
		servertest.ReleaseTicks(t, srv)
		defer servertest.HoldTicks(t, srv)
		data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn)
		if !ok {
			t.Fatalf("no Respawn for %s", name)
		}
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != typeID {
			t.Errorf("type id = %d, want %d", got, typeID)
		}
		if got := r.ReadString(32767); got != name {
			t.Errorf("dimension = %q, want %q", got, name)
		}
	}
	srv.MovePlayerToDimension("Effects", "test:flat", 8.5, 4, 8.5)
	expectRespawn("test:flat")
	if got := srv.PlayerDimension("Effects"); got != "test:flat" {
		t.Fatalf("player is in %q", got)
	}
	srv.MovePlayerToDimension("Effects", "minecraft:overworld", 0.5, 70, 0.5)
	expectRespawn("minecraft:overworld")
	srv.MovePlayerToDimension("Effects", "test:flat", 8.5, 4, 8.5)
	expectRespawn("test:flat")

	run(func() {
		if err := h.UnloadWorld(h.Overworld(), true); err == nil {
			t.Error("the overworld unloaded")
		}
		nether, _ := h.World(plugin.MinecraftKey("the_nether"))
		if err := h.UnloadWorld(nether, true); err == nil {
			t.Error("the nether unloaded")
		}
	})
	var cancelled atomic.Bool
	cancelled.Store(true)
	var l plugin.Listener
	run(func() {
		l = plugin.Listen(h, func(e *plugin.WorldUnloadEvent) { e.SetCancelled(cancelled.Load()) })
	})
	if err := unloadFlat(t, run, h, w, true); err == nil {
		t.Fatal("a cancelled unload succeeded")
	}
	run(func() {
		if _, ok := h.World(flatKey); !ok {
			t.Error("the world went away with the unload cancelled")
		}
		l.Unregister()
	})
	cancelled.Store(false)
	run(func() {
		l = plugin.Listen(h, func(e *plugin.WorldUnloadEvent) {
			if e.World.Key() != flatKey {
				t.Errorf("unload event for %v", e.World.Key())
			}
		})
	})
	if err := unloadFlat(t, run, h, w, false); err != nil {
		t.Fatal(err)
	}
	expectRespawn("minecraft:overworld")
	if got := srv.PlayerDimension("Effects"); got != "minecraft:overworld" {
		t.Errorf("player is in %q after the unload", got)
	}
}

func keepInventoryRule(t *testing.T, h plugin.Server) plugin.GameRule {
	t.Helper()
	id, ok := h.Registries().GameRule(plugin.MinecraftKey("keep_inventory"))
	if !ok {
		t.Fatal("no keep_inventory rule")
	}
	return id
}

// A rule set on a plugin world is that world's alone, and survives an unload.
// vanilla: GameRules (per-world table here, copied from the overworld on creation)
func TestPluginWorldOwnGameRules(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	rule := keepInventoryRule(t, h)
	w := createFlat(t, run, h, &flatGen{})
	run(func() {
		if w.GameRule(rule) != 0 {
			t.Error("a new world did not copy the overworld's keep_inventory")
		}
		w.SetGameRule(rule, 1)
		if w.GameRule(rule) != 1 {
			t.Error("rule not set on the plugin world")
		}
		if h.Overworld().GameRule(rule) != 0 {
			t.Error("the overworld saw the plugin world's rule")
		}
		nether, _ := h.World(plugin.MinecraftKey("the_nether"))
		if nether.GameRule(rule) != 0 {
			t.Error("the nether saw the plugin world's rule")
		}
		h.Overworld().SetGameRule(rule, 1)
		if nether.GameRule(rule) != 1 {
			t.Error("the nether does not share the level's rules")
		}
		h.Overworld().SetGameRule(rule, 0)
		w.SetGameRule(rule, 1)
	})
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	w = createFlat(t, run, h, &flatGen{})
	run(func() {
		if w.GameRule(rule) != 1 {
			t.Error("the rule did not survive UnloadWorld and CreateWorld")
		}
	})
}

// A death in a plugin world follows that world's keep_inventory.
// vanilla: ServerPlayer.die
func TestPluginWorldDeathKeepsOwnRule(t *testing.T) {
	t.Parallel()
	var h plugin.Server
	var got *plugin.PlayerDeathEvent
	cfg := servertest.SurvivalTestConfig()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "world", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		plugin.Listen(s, func(e *plugin.PlayerDeathEvent) { got = e })
		return nil
	}}}})
	servertest.HoldTicks(t, srv)
	run := func(f func()) {
		t.Helper()
		h.Scheduler().RunTask(f)
		servertest.RunTicks(t, srv, 1)
	}
	c := servertest.DialServer(t, srv)
	c.Join("Victim")
	c.Settle()
	w := createFlat(t, run, h, &flatGen{})
	rule := keepInventoryRule(t, h)
	run(func() { w.SetGameRule(rule, 1) })
	srv.MovePlayerToDimension("Victim", "test:flat", 8.5, 4, 8.5)
	srv.SetHealth("Victim", 5)
	if !srv.Hurt("Victim", 50) || got == nil {
		t.Fatal("no death")
	}
	if !got.KeepInventory || !got.KeepLevel {
		t.Errorf("death in the plugin world: keep inventory %v, level %v; want both", got.KeepInventory, got.KeepLevel)
	}
}

// /gamerule and execute-in target a plugin world's own table, reach only its
// players, and dimension switches sync the three client-side rule flags.
// vanilla: GameRuleCommand, MinecraftServer.onGameRuleChanged
func TestPluginWorldGameRuleCommandAndClientFlags(t *testing.T) {
	t.Parallel()
	var h plugin.Server
	cfg := servertest.BlockTestConfig()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "world", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		return nil
	}}}}, servertest.OpsListing(t, "Op"))
	servertest.HoldTicks(t, srv)
	run := func(f func()) {
		t.Helper()
		h.Scheduler().RunTask(f)
		servertest.RunTicks(t, srv, 1)
	}

	op := servertest.DialServer(t, srv)
	op.Join("Op")
	over := servertest.DialServer(t, srv)
	over.Join("Over")
	op.Settle()
	over.Settle()

	w := createFlat(t, run, h, &flatGen{})

	// execute in <plugin world> reads and writes that world's rules alone.
	op.ChatCommand("execute in test:flat run gamerule keep_inventory true")
	servertest.AwaitCommandFeedback(t, op, "commands.gamerule.set", "keep_inventory", "true")
	op.ChatCommand("execute in test:flat run gamerule keep_inventory")
	servertest.AwaitCommandFeedback(t, op, "commands.gamerule.query", "keep_inventory", "true")
	op.ChatCommand("gamerule keep_inventory")
	servertest.AwaitCommandFeedback(t, op, "commands.gamerule.query", "keep_inventory", "false")

	// Set all three client-side rules on the plugin world while both players
	// are in the overworld, then move Op into the plugin world.
	run(func() {
		for _, name := range []string{"immediate_respawn", "limited_crafting", "reduced_debug_info"} {
			id, ok := h.Registries().GameRule(plugin.MinecraftKey(name))
			if !ok {
				t.Fatalf("missing rule %s", name)
			}
			w.SetGameRule(id, 1)
		}
	})
	over.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayGameEvent || id == protocol.IDPlayEntityEvent {
			t.Errorf("overworld player received rule packet %d for plugin world change", id)
		}
	})

	srv.MovePlayerToDimension("Op", "test:flat", 8.5, 4, 8.5)
	var gotRespawn, gotCrafting, gotDebug bool
	op.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayGameEvent:
			r := protocol.NewReader(data)
			ev, val := r.ReadU8(), r.ReadF32()
			if ev == protocol.GameEventImmediateRespawn && val == 1 {
				gotRespawn = true
			}
			if ev == protocol.GameEventLimitedCrafting && val == 1 {
				gotCrafting = true
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			_ = r.ReadI32()
			if r.ReadU8() == protocol.EntityStatusReducedDebugOn {
				gotDebug = true
			}
		}
	})
	if !gotRespawn || !gotCrafting || !gotDebug {
		t.Errorf("entering plugin world flags: immediate_respawn=%v limited_crafting=%v reduced_debug=%v; want all true",
			gotRespawn, gotCrafting, gotDebug)
	}

	// /gamerule run inside the plugin world updates its own clock and audience.
	op.ChatCommand("gamerule advance_time false")
	op.ExpectPlay(protocol.IDPlayUpdateTime)
	servertest.AwaitCommandFeedback(t, op, "commands.gamerule.set", "advance_time", "false")
	over.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(servertest.SettlePing) })
	for id, data := range over.Packets(servertest.ExpectTimeout) {
		if servertest.IsPingAnswer(id, data, servertest.SettlePing) {
			break
		}
		if id == protocol.IDPlayUpdateTime {
			t.Error("overworld player received Update Time for plugin world's advance_time change")
		}
	}

	op.ChatCommand("gamerule immediate_respawn false")
	var clearedRespawn bool
	op.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayGameEvent {
			r := protocol.NewReader(data)
			if r.ReadU8() == protocol.GameEventImmediateRespawn && r.ReadF32() == 0 {
				clearedRespawn = true
			}
		}
	})
	if !clearedRespawn {
		t.Error("immediate_respawn=false in plugin world did not notify its player")
	}
	over.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayGameEvent {
			t.Error("overworld player received Game Event for plugin world's immediate_respawn change")
		}
	})

	// Returning to the overworld resets the two flags that still differ and
	// skips immediate_respawn, which now matches.
	srv.MovePlayerToDimension("Op", "minecraft:overworld", 0.5, 4, 0.5)
	var resetRespawn, resetCrafting, resetDebug bool
	op.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayGameEvent:
			r := protocol.NewReader(data)
			ev, val := r.ReadU8(), r.ReadF32()
			if ev == protocol.GameEventImmediateRespawn {
				resetRespawn = true
			}
			if ev == protocol.GameEventLimitedCrafting && val == 0 {
				resetCrafting = true
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			_ = r.ReadI32()
			if r.ReadU8() == protocol.EntityStatusReducedDebugOff {
				resetDebug = true
			}
		}
	})
	if resetRespawn {
		t.Error("returning to overworld sent immediate_respawn even though both worlds are false")
	}
	if !resetCrafting || !resetDebug {
		t.Errorf("returning to overworld reset flags: limited_crafting=%v reduced_debug=%v; want both true",
			resetCrafting, resetDebug)
	}
}

// vanilla: WorldPresets.Bootstrap (FLAT), FlatLevelGeneratorSettings.getDefault
func TestPluginCreateWorldFlatType(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	opts := plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Type: plugin.WorldTypeFlat, Seed: 5}
	var loaded atomic.Bool
	run(func() {
		for _, bad := range []string{"not json", `{"biome":"minecraft:plains"}`} {
			o := opts
			o.GeneratorSettings = bad
			if _, err := h.CreateWorld(o); err == nil {
				t.Errorf("settings %q were accepted", bad)
			}
		}
		w, err := h.CreateWorld(opts)
		if err != nil {
			t.Fatal(err)
		}
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) { loaded.Store(true) })
	})
	awaitPluginIO(t, srv, "flat chunk", loaded.Load)
	for y, want := range map[int32]registry.Block{-64: registry.BlockBedrock, -63: registry.BlockDirt, -62: registry.BlockDirt, -61: registry.BlockGrassBlock, -60: registry.BlockAir} {
		if got := srv.LoadBlockInDimension("test:flat", 0, y, 0); got != uint16(want.DefaultState()) {
			t.Errorf("y=%d = %d, want %v", y, got, want)
		}
	}
}

// vanilla: FlatLevelGeneratorSettings.CODEC
func TestPluginCreateWorldFlatSettings(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var loaded atomic.Bool
	run(func() {
		w, err := h.CreateWorld(plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Type: plugin.WorldTypeFlat, NoStructures: true,
			GeneratorSettings: `{"layers":[{"block":"minecraft:stone","height":2},{"block":"minecraft:sand","height":1}],"biome":"minecraft:desert"}`})
		if err != nil {
			t.Fatal(err)
		}
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) { loaded.Store(true) })
	})
	awaitPluginIO(t, srv, "flat chunk", loaded.Load)
	for y, want := range map[int32]registry.Block{-64: registry.BlockStone, -63: registry.BlockStone, -62: registry.BlockSand, -61: registry.BlockAir} {
		if got := srv.LoadBlockInDimension("test:flat", 0, y, 0); got != uint16(want.DefaultState()) {
			t.Errorf("y=%d = %d, want %v", y, got, want)
		}
	}
}

// vanilla: FixedBiomeSource
func TestPluginCreateWorldBiome(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		w, err := h.CreateWorld(plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Biome: plugin.MinecraftKey("desert")})
		if err != nil {
			t.Fatal(err)
		}
		want, _ := registry.BiomeByName("minecraft:desert")
		if got := w.Biome(plugin.BlockPos{X: 1000, Y: 70, Z: -2000}); got != want {
			t.Errorf("biome = %v, want desert", got)
		}
	})
}

// A reload with plain options rebuilds the world from the saved options.
func TestPluginCreateWorldReloadKeepsOptions(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var w plugin.World
	run(func() {
		var err error
		w, err = h.CreateWorld(plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Type: plugin.WorldTypeFlat, NoStructures: true, Seed: 5,
			GeneratorSettings: `{"layers":[{"block":"minecraft:stone","height":2},{"block":"minecraft:sand","height":1}],"biome":"minecraft:desert"}`})
		if err != nil {
			t.Error(err)
		}
	})
	if w == nil {
		t.FailNow()
	}
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	var loaded atomic.Bool
	run(func() {
		w, err := h.CreateWorld(plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld")})
		if err != nil {
			t.Error(err)
			return
		}
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) { loaded.Store(true) })
	})
	awaitPluginIO(t, srv, "flat chunk", loaded.Load)
	for y, want := range map[int32]registry.Block{-64: registry.BlockStone, -63: registry.BlockStone, -62: registry.BlockSand, -61: registry.BlockAir} {
		if got := srv.LoadBlockInDimension("test:flat", 0, y, 0); got != uint16(want.DefaultState()) {
			t.Errorf("y=%d = %d, want %v", y, got, want)
		}
	}
}

func TestPluginWorldFolder(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	var overworld, flat string
	run(func() {
		overworld = h.WorldFolder(plugin.MinecraftKey("overworld"))
		flat = h.WorldFolder(flatKey)
	})
	if overworld != cfg.LevelName {
		t.Errorf("overworld folder = %q, want %q", overworld, cfg.LevelName)
	}
	if want := filepath.Join(cfg.LevelName, "dimensions", "test", "flat"); flat != want {
		t.Fatalf("flat folder = %q, want %q", flat, want)
	}
	w := createFlat(t, run, h, &flatGen{})
	run(func() { w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) {}) })
	awaitPluginIO(t, srv, "flat chunk", func() bool { return srv.ChunkLoadedInDimension("test:flat", 0, 0) })
	run(func() { w.SetSpawnPoint(plugin.Location{World: w, Pos: plugin.Vec3{X: 3, Y: 5, Z: 3}}) })
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(flat, "region")); err != nil {
		t.Fatalf("region folder: %v", err)
	}
	if err := os.RemoveAll(flat); err != nil {
		t.Fatal(err)
	}
	opts := flatOptions(&flatGen{})
	opts.Seed = 7
	run(func() {
		var err error
		if w, err = h.CreateWorld(opts); err != nil {
			t.Fatalf("CreateWorld: %v", err)
		}
		if w.Seed() != 7 {
			t.Errorf("seed = %d, want 7", w.Seed())
		}
		if sp := w.SpawnPoint(); sp.Pos.Block() == (plugin.BlockPos{X: 3, Y: 5, Z: 3}) {
			t.Error("the removed world's spawn came back")
		}
	})
}
