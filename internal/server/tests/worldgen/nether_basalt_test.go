package worldgen

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestNetherBasaltColumnsReplaceFlowingLava pins the two columns in seed 0
// whose bases are flowing lava from a spring placed before the columns.
func TestNetherBasaltColumnsReplaceFlowingLava(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelSeed = "0"
	srv, _ := servertest.Join(t, cfg, "Explorer")
	release := srv.HoldChunkInDimension(world.NetherName, 896, -1232)
	defer release()
	for _, col := range []struct{ x, z, top int32 }{{899, -1222, 35}, {900, -1220, 42}} {
		for y := int32(32); y <= col.top; y++ {
			got := srv.BlockInDimension(world.NetherName, col.x, y, col.z)
			if registry.BlockOf(got) != registry.BlockBasalt || registry.StateValue(got, registry.PropAxis) != "y" {
				t.Errorf("block (%d,%d,%d) = %d, want basalt axis=y", col.x, y, col.z, got)
			}
		}
	}
}
