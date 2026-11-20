package plugins_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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

// vanilla: MinecraftServer.setInitialSpawn, BonusChestFeature.place
func TestPluginCreateWorldBonusChest(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)

	scanSpawnChunk := func(w plugin.World) (chests []plugin.BlockPos, torches int, loot plugin.Key) {
		t.Helper()
		var sp plugin.BlockPos
		var loaded atomic.Bool
		run(func() {
			sp = w.SpawnPoint().Pos.Block()
			cp := plugin.ChunkPos{X: sp.X >> 4, Z: sp.Z >> 4}
			w.AddTicket(cp, 0)
			w.LoadChunk(cp, true, func(plugin.Chunk) { loaded.Store(true) })
		})
		awaitPluginIO(t, srv, "spawn chunk", loaded.Load)
		run(func() {
			cp := plugin.ChunkPos{X: sp.X >> 4, Z: sp.Z >> 4}
			defer w.RemoveTicket(cp, 0)
			x0, z0 := cp.X*16, cp.Z*16
			for x := x0; x < x0+16; x++ {
				for z := z0; z < z0+16; z++ {
					for y := w.MinY(); y < w.MinY()+128; y++ {
						p := plugin.BlockPos{X: x, Y: y, Z: z}
						switch w.BlockState(p).Block() {
						case registry.BlockChest:
							chests = append(chests, p)
						case registry.BlockTorch:
							torches++
						}
					}
				}
			}
			if len(chests) == 1 {
				if be, ok := w.BlockEntity(chests[0]); ok {
					if c, ok := be.(plugin.Container); ok {
						loot, _ = c.LootTable()
					}
				}
			}
		})
		return chests, torches, loot
	}

	bonusKey := plugin.Key{Namespace: "test", Path: "bonus"}
	var w plugin.World
	run(func() {
		var err error
		w, err = h.CreateWorld(plugin.WorldOptions{
			Key: bonusKey, Dimension: plugin.MinecraftKey("overworld"),
			Type: plugin.WorldTypeFlat, Seed: 11, BonusChest: true,
		})
		if err != nil {
			t.Error(err)
		}
	})
	if w == nil {
		t.FailNow()
	}
	chests, torches, loot := scanSpawnChunk(w)
	if len(chests) != 1 {
		t.Fatalf("bonus world chests = %v, want 1", chests)
	}
	if want := plugin.MinecraftKey("chests/spawn_bonus_chest"); loot != want {
		t.Errorf("bonus chest loot table = %v, want %v", loot, want)
	}
	if torches == 0 {
		t.Error("no torch in spawn chunk, want at least one beside the bonus chest")
	}
	if meta, ok := world.ReadPluginMeta(cfg.LevelName, bonusKey.String()); !ok || !meta.Gen.BonusChest {
		t.Errorf("plugin_world.dat BonusChest = %v (ok=%v), want true", meta.Gen.BonusChest, ok)
	}

	// Reloading an existing world with BonusChest: true must not add a second chest,
	// and reloading with BonusChest: false preserves the saved meta flag.
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	run(func() {
		var err error
		w, err = h.CreateWorld(plugin.WorldOptions{
			Key: bonusKey, Dimension: plugin.MinecraftKey("overworld"), BonusChest: true,
		})
		if err != nil {
			t.Error(err)
		}
	})
	if chests, _, _ = scanSpawnChunk(w); len(chests) != 1 {
		t.Fatalf("reloaded bonus world chests = %v, want 1", chests)
	}
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	run(func() {
		var err error
		w, err = h.CreateWorld(plugin.WorldOptions{
			Key: bonusKey, Dimension: plugin.MinecraftKey("overworld"),
		})
		if err != nil {
			t.Error(err)
		}
	})
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	if meta, ok := world.ReadPluginMeta(cfg.LevelName, bonusKey.String()); !ok || !meta.Gen.BonusChest {
		t.Errorf("plugin_world.dat BonusChest after plain reload = %v (ok=%v), want true", meta.Gen.BonusChest, ok)
	}

	// A new world without BonusChest gets no chest, and reloading it with BonusChest: true adds none.
	plainKey := plugin.Key{Namespace: "test", Path: "nobonus"}
	run(func() {
		var err error
		w, err = h.CreateWorld(plugin.WorldOptions{
			Key: plainKey, Dimension: plugin.MinecraftKey("overworld"),
			Type: plugin.WorldTypeFlat, Seed: 11,
		})
		if err != nil {
			t.Error(err)
		}
	})
	if chests, _, _ = scanSpawnChunk(w); len(chests) != 0 {
		t.Fatalf("non-bonus world chests = %v, want 0", chests)
	}
	if meta, ok := world.ReadPluginMeta(cfg.LevelName, plainKey.String()); !ok || meta.Gen.BonusChest {
		t.Errorf("plugin_world.dat BonusChest = %v (ok=%v), want false", meta.Gen.BonusChest, ok)
	}
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	run(func() {
		var err error
		w, err = h.CreateWorld(plugin.WorldOptions{
			Key: plainKey, Dimension: plugin.MinecraftKey("overworld"), BonusChest: true,
		})
		if err != nil {
			t.Error(err)
		}
	})
	if chests, _, _ = scanSpawnChunk(w); len(chests) != 0 {
		t.Fatalf("reloaded non-bonus world chests = %v, want 0", chests)
	}
}
