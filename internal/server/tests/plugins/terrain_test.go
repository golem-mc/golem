package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Level.getBlockState, Heightmap.Types, LevelChunk.getBlockEntities.
func TestPluginTerrainReads(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	pos := world.ChunkPos{X: -2, Z: 3}
	w := srv.OverworldWorld()
	w.Acquire(pos)
	defer w.Release(pos)
	x, z := pos.X*16+5, pos.Z*16+5
	w.SetBlock(x, 100, z, registry.BlockStone.DefaultState())
	w.SetBlock(x, 101, z, registry.BlockWater.DefaultState())
	leaves, _ := registry.WithBool(registry.BlockOakLeaves.DefaultState(), registry.PropPersistent, true)
	w.SetBlock(x, 102, z, leaves)
	w.SetBlock(x, 103, z, registry.BlockTorch.DefaultState())
	w.Relight(x, z)
	run(func() {
		pw := h.Overworld()
		p := plugin.BlockPos{X: int(x), Y: 100, Z: int(z)}
		if got := pw.BlockState(p); got != plugin.DefaultState(registry.BlockStone) {
			t.Errorf("block=%v", got)
		}
		b := pw.Block(p)
		if b.World != pw || b.Pos != p || b.State() != pw.BlockState(p) {
			t.Error("block handle")
		}
		if b.Relative(plugin.Up, 3).State() != plugin.DefaultState(registry.BlockTorch) {
			t.Error("relative block")
		}
		for hm, want := range map[plugin.Heightmap]int{plugin.WorldSurface: 103, plugin.MotionBlocking: 102, plugin.MotionBlockingNoLeaves: 101, plugin.OceanFloor: 102} {
			if got := pw.HighestBlockY(int(x), int(z), hm); got != want {
				t.Errorf("heightmap %v=%v, want %v", hm, got, want)
			}
		}
		if got := pw.Biome(p); got != w.Biome(x, 100, z) {
			t.Errorf("biome=%v", got)
		}
		lightPos := plugin.BlockPos{X: int(x), Y: 104, Z: int(z)}
		sky, block := pw.Light(lightPos)
		if sky != 15 || block != 13 {
			t.Error("light")
		}
		wrapped := plugin.BlockPos{X: int(x) + (1 << 32), Y: 100, Z: int(z)}
		if pw.BlockState(wrapped) != plugin.DefaultState(registry.BlockAir) || pw.Biome(wrapped) != registry.NoBiome {
			t.Error("coordinate wrapped into a loaded chunk")
		}
		if sky, block := pw.Light(wrapped); sky != 0 || block != 0 {
			t.Error("wrapped light")
		}
		if _, ok := pw.BlockEntity(wrapped); ok {
			t.Error("wrapped entity")
		}
		if _, ok := pw.Chunk(plugin.ChunkPos{X: int(pos.X) + (1 << 32), Z: int(pos.Z)}); ok {
			t.Error("wrapped chunk")
		}
		if pw.HighestBlockY(wrapped.X, wrapped.Z, plugin.WorldSurface) != pw.MinY()-1 {
			t.Error("wrapped heightmap")
		}
		unloaded := plugin.BlockPos{X: 2000000, Y: 100, Z: 2000000}
		if pw.BlockState(unloaded) != plugin.DefaultState(registry.BlockAir) {
			t.Error("unloaded block")
		}
		if _, ok := pw.Chunk(plugin.ChunkPos{X: 125000, Z: 125000}); ok {
			t.Error("unloaded chunk")
		}
		if _, ok := pw.BlockEntity(unloaded); ok {
			t.Error("unloaded block entity")
		}
		if pw.HighestBlockY(unloaded.X, unloaded.Z, plugin.WorldSurface) != pw.MinY()-1 {
			t.Error("unloaded heightmap")
		}
		if w.ChunkLoaded(world.ChunkPos{X: 125000, Z: 125000}) {
			t.Error("query generated a chunk")
		}
	})
}
