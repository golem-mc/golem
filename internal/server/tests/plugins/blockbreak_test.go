package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Level.destroyBlock.
func TestPluginBreakBlock(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	run(func() {
		pw := h.Overworld()
		air := plugin.DefaultState(registry.BlockAir)
		stone := plugin.DefaultState(registry.BlockStone)
		a, b := plugin.BlockPos{X: 1, Y: 80, Z: 1}, plugin.BlockPos{X: 3, Y: 80, Z: 1}
		pw.SetBlockState(a, stone, plugin.UpdateAll)
		pw.SetBlockState(b, stone, plugin.UpdateAll)
		if !pw.BreakBlock(a, false) || pw.BlockState(a) != air {
			t.Error("BreakBlock(false) did not clear the block")
		}
		if n := len(srv.ItemsInDimension(world.DimensionName)); n != 0 {
			t.Errorf("drops=false spawned %d items", n)
		}
		if pw.BreakBlock(a, true) {
			t.Error("breaking air succeeded")
		}
		if !pw.Block(b).BreakNaturally() || pw.BlockState(b) != air {
			t.Error("BreakNaturally did not clear the block")
		}
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 || items[0].Item != "minecraft:cobblestone" {
			t.Errorf("stone drops = %v, want one cobblestone", items)
		}
	})
}

// vanilla: Level.destroyBlock, Containers.dropContents.
func TestPluginBreakBlockSpillsChest(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	placeContainerEntity(w, 2, 80, 2, registry.BlockChest)
	run(func() {
		pw := h.Overworld()
		p := plugin.BlockPos{X: 2, Y: 80, Z: 2}
		containerHandle(t, pw, p).Inventory().SetItem(0, plugin.NewStack(registry.ItemDiamond, 3))
		if !pw.BreakBlock(p, false) {
			t.Fatal("chest did not break")
		}
		var diamonds, chests int
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			switch it.Item {
			case "minecraft:diamond":
				diamonds += int(it.Count)
			case "minecraft:chest":
				chests++
			}
		}
		if diamonds != 3 || chests != 0 {
			t.Errorf("spilled %d diamonds and %d chests, want 3 and 0", diamonds, chests)
		}
	})
}

func TestPluginBreakBlockUnloaded(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	run(func() {
		if h.Overworld().BreakBlock(plugin.BlockPos{X: 2000000, Y: 80, Z: 2000000}, true) {
			t.Error("break in an unloaded chunk succeeded")
		}
	})
}
