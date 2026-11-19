package worldgen

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestNetherCrimsonFungusAtBiomeEdge checks the seed-12345 fungus straddling
// chunks (-1,7) and (0,7), including its stem, cap, shroomlights and vines.
func TestNetherCrimsonFungusAtBiomeEdge(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelSeed = "12345"
	cfg.LevelType = "minecraft:flat" // Keep this seed rather than the harness's shared Nether.
	cfg.GeneratorSettings = `{"layers":[{"block":"minecraft:bedrock","height":1},{"block":"minecraft:dirt","height":2},{"block":"minecraft:grass_block","height":1}],"biome":"minecraft:plains"}`
	srv, _ := servertest.Join(t, cfg, "Explorer")
	release := srv.HoldChunkInDimension(world.NetherName, 0, 112)
	defer release()
	releaseWest := srv.HoldChunkInDimension(world.NetherName, -16, 112)
	defer releaseWest()
	for _, p := range []struct {
		x, y, z int32
		block   registry.Block
	}{
		{0, 67, 121, registry.BlockCrimsonStem},
		{-2, 70, 122, registry.BlockNetherWartBlock},
		{0, 70, 120, registry.BlockShroomlight},
		{2, 64, 123, registry.BlockWeepingVines},
	} {
		if got := registry.BlockOf(srv.BlockInDimension(world.NetherName, p.x, p.y, p.z)); got != p.block {
			t.Errorf("block at (%d,%d,%d) = %s, want %s", p.x, p.y, p.z, got.Name(), p.block.Name())
		}
	}
}
