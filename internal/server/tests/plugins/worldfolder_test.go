package plugins_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

const folderSeed = 4242

var folderKey = plugin.Key{Namespace: "test", Path: "folder"}

// writeFixtureLevel writes a level.dat holding the seed and spawn.
func writeFixtureLevel(t *testing.T, dir string) {
	t.Helper()
	data := nbt.Compound{
		"SpawnX": nbt.Int(40), "SpawnY": nbt.Int(120), "SpawnZ": nbt.Int(-24),
		"WorldGenSettings": nbt.Compound{"seed": nbt.Long(folderSeed)},
	}
	encoded, err := nbt.Encode("", nbt.Compound{"Data": data})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(encoded)
	gz.Close()
	if err := os.WriteFile(filepath.Join(dir, "level.dat"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func loadChunkAt(t *testing.T, srv *server.Server, run func(func()), w plugin.World, pos plugin.ChunkPos) {
	t.Helper()
	var loaded atomic.Bool
	run(func() { w.LoadChunk(pos, true, func(plugin.Chunk) { loaded.Store(true) }) })
	awaitPluginIO(t, srv, "chunk", loaded.Load)
}

func column(srv *server.Server, dim string, x, z int32) (col [8]uint16) {
	for i := range col {
		col[i] = srv.LoadBlockInDimension(dim, x, int32(60+i*8), z)
	}
	return col
}

// vanilla: LevelStorageSource.createAccess, PrimaryLevelData
func TestPluginCreateWorldFromFolderCurrentLayout(t *testing.T) {
	testWorldFromFolder(t, filepath.Join("dimensions", "minecraft", "overworld"))
}

// vanilla: LevelStorageSource.createAccess, DimensionStorageFileFix
func TestPluginCreateWorldFromFolderLegacyLayout(t *testing.T) {
	testWorldFromFolder(t, "")
}

func testWorldFromFolder(t *testing.T, layout string) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	goldState := uint16(registry.BlockGoldBlock.DefaultState())

	// The fixture's chunk comes from a vanilla world of the same seed.
	srcKey := plugin.Key{Namespace: "test", Path: "src"}
	var src plugin.World
	run(func() {
		var err error
		src, err = h.CreateWorld(plugin.WorldOptions{Key: srcKey, Dimension: plugin.MinecraftKey("overworld"), Seed: folderSeed})
		if err != nil {
			t.Error(err)
		}
	})
	if src == nil {
		t.FailNow()
	}
	loadChunkAt(t, srv, run, src, plugin.ChunkPos{})
	run(func() {
		src.AddTicket(plugin.ChunkPos{}, 0)
		src.SetBlockState(plugin.BlockPos{X: 2, Y: 200, Z: 2}, gold, plugin.UpdateAll)
	})
	if err := unloadFlat(t, run, h, src, true); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(cfg.LevelName, "dimensions", "test", "src")

	// A reference of the seed's terrain far from the fixture.
	refKey := plugin.Key{Namespace: "test", Path: "ref"}
	var ref plugin.World
	run(func() {
		ref, _ = h.CreateWorld(plugin.WorldOptions{Key: refKey, Dimension: plugin.MinecraftKey("overworld"), Seed: folderSeed})
	})
	far := plugin.ChunkPos{X: 40, Z: 40}
	loadChunkAt(t, srv, run, ref, far)
	want := column(srv, refKey.String(), 40*16+3, 40*16+5)

	folder := t.TempDir()
	copyTree(t, filepath.Join(srcDir, "region"), filepath.Join(folder, layout, "region"))
	writeFixtureLevel(t, folder)
	key := folderKey
	var w plugin.World
	run(func() {
		var err error
		w, err = h.CreateWorld(plugin.WorldOptions{Key: key, Dimension: plugin.MinecraftKey("overworld"), Folder: folder})
		if err != nil {
			t.Error(err)
		}
	})
	if w == nil {
		t.FailNow()
	}
	loadChunkAt(t, srv, run, w, plugin.ChunkPos{})
	if got := srv.LoadBlockInDimension(key.String(), 2, 200, 2); got != goldState {
		t.Errorf("fixture block = %d, want gold", got)
	}
	run(func() {
		if w.Seed() != folderSeed {
			t.Errorf("seed = %d, want %d", w.Seed(), folderSeed)
		}
		if sp := w.SpawnPoint().Pos.Block(); sp != (plugin.BlockPos{X: 40, Y: 120, Z: -24}) {
			t.Errorf("spawn = %v", sp)
		}
		if got := h.WorldFolder(key); got != folder {
			t.Errorf("WorldFolder = %q, want %q", got, folder)
		}
	})
	loadChunkAt(t, srv, run, w, far)
	if got := column(srv, key.String(), 40*16+3, 40*16+5); got != want {
		t.Errorf("terrain outside the fixture = %v, want the seed's %v", got, want)
	}
	run(func() {
		w.AddTicket(plugin.ChunkPos{}, 0)
		w.SetBlockState(plugin.BlockPos{X: 3, Y: 201, Z: 3}, gold, plugin.UpdateAll)
	})
	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "dimensions", "minecraft", "overworld", "data", "minecraft", "plugin_world.dat")); err != nil {
		t.Errorf("save did not write into the folder: %v", err)
	}
	run(func() {
		w, _ = h.CreateWorld(plugin.WorldOptions{Key: key, Dimension: plugin.MinecraftKey("overworld"), Folder: folder})
	})
	loadChunkAt(t, srv, run, w, plugin.ChunkPos{})
	if got := srv.LoadBlockInDimension(key.String(), 3, 201, 3); got != goldState {
		t.Errorf("block saved into the folder = %d, want gold", got)
	}
	if err := unloadFlat(t, run, h, w, false); err != nil {
		t.Fatal(err)
	}
}
