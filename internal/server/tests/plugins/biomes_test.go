package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ChunkMap.resendBiomesForChunks; a regenerated chunk is the
// generator's output for the same seed.
func TestPluginSetBiomeAndRegenerate(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	native := world.ChunkPos{X: 30, Z: 30}
	w.Acquire(native)
	defer w.Release(native)
	pos := plugin.ChunkPos{X: 30, Z: 30}
	base := plugin.BlockPos{X: 480, Y: 60, Z: 480}
	run(func() {
		ow := h.Overworld()
		before := ow.Biome(base)
		if before == registry.BiomeDesert {
			t.Fatal("fixture already desert")
		}
		cells := [][3]int{}
		for x := 0; x < 16; x += 4 {
			for z := 0; z < 16; z += 4 {
				for y := 56; y < 72; y += 4 {
					cells = append(cells, [3]int{x, y, z})
				}
			}
		}
		for _, c := range cells {
			if !ow.SetBiome(plugin.BlockPos{X: 480 + c[0], Y: c[1], Z: 480 + c[2]}, registry.BiomeDesert) {
				t.Fatal("SetBiome failed in a loaded chunk")
			}
		}
		if got := ow.Biome(plugin.BlockPos{X: 488, Y: 64, Z: 488}); got != registry.BiomeDesert {
			t.Errorf("Biome after SetBiome=%v", got)
		}
		if ow.SetBiome(plugin.BlockPos{X: 4800, Y: 64, Z: 4800}, registry.BiomeDesert) {
			t.Error("SetBiome succeeded in an unloaded chunk")
		}
		if ow.Regenerate(plugin.ChunkPos{X: 300, Z: 300}) {
			t.Error("Regenerate succeeded in an unloaded chunk")
		}
		type cell struct {
			p plugin.BlockPos
			s plugin.BlockState
		}
		var want []cell
		for y := 40; y < 80; y++ {
			p := plugin.BlockPos{X: 485, Y: y, Z: 490}
			want = append(want, cell{p, ow.BlockState(p)})
		}
		gold := plugin.DefaultState(registry.BlockGoldBlock)
		for _, c := range want {
			ow.SetBlockState(c.p, gold, plugin.UpdateClients)
		}
		if !ow.Regenerate(pos) {
			t.Fatal("Regenerate failed in a loaded chunk")
		}
		for _, c := range want {
			if got := ow.BlockState(c.p); got != c.s {
				t.Errorf("block at %v=%v, generated %v", c.p, got, c.s)
			}
		}
		if got := ow.Biome(plugin.BlockPos{X: 488, Y: 64, Z: 488}); got != before {
			t.Errorf("Biome after Regenerate=%v, generated %v", got, before)
		}
	})
}
