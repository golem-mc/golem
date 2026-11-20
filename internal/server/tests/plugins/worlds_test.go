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
	run(func() {
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) {})
	})
	awaitPluginIO(t, srv, "flat chunk", func() bool { return srv.ChunkLoadedInDimension("test:flat", 0, 0) })
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

// vanilla: WorldPresets.Bootstrap (FLAT), FlatLevelGeneratorSettings.getDefault
func TestPluginCreateWorldFlatType(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	opts := plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Type: plugin.WorldTypeFlat, Seed: 5}
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
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) {})
	})
	awaitPluginIO(t, srv, "flat chunk", func() bool { return srv.ChunkLoadedInDimension("test:flat", 0, 0) })
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
	run(func() {
		w, err := h.CreateWorld(plugin.WorldOptions{Key: flatKey, Dimension: plugin.MinecraftKey("overworld"), Type: plugin.WorldTypeFlat, NoStructures: true,
			GeneratorSettings: `{"layers":[{"block":"minecraft:stone","height":2},{"block":"minecraft:sand","height":1}],"biome":"minecraft:desert"}`})
		if err != nil {
			t.Fatal(err)
		}
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) {})
	})
	awaitPluginIO(t, srv, "flat chunk", func() bool { return srv.ChunkLoadedInDimension("test:flat", 0, 0) })
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
