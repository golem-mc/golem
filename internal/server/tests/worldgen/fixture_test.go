package worldgen

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// copyRegion generates each seed's 5x5 once per process. Each reader gets its
// own files, world and generator, so ticks, saves and unloads cannot change
// another test's input. Order/restart tests deliberately build their own worlds.
// These fixtures check natural features, so structures are disabled.
func copyRegion(t *testing.T, seed int64) string {
	t.Helper()
	return originRegion(seed).Copy(t)
}

func originRegion(seed int64) servertest.RegionFixture {
	return servertest.RegionFixture{
		Seed: seed,
		Min:  world.ChunkPos{X: -2, Z: -2}, Max: world.ChunkPos{X: 2, Z: 2},
	}
}

// surfaceRegion covers the two taigas, dark forest and stony shore used by
// the biome and surface comparisons, and the tall plants growing on them.
var surfaceRegion = servertest.RegionFixture{
	Seed: pipelineSeed,
	Min:  world.ChunkPos{X: 13, Z: 5}, Max: world.ChunkPos{X: 18, Z: 7},
}

// check is one former top-level test of a shared server, run as a subtest
// named after it.
type check struct {
	name string
	run  func(*testing.T, *server.Server)
}

// runParallel runs read-only checks of one server side by side.
func runParallel(t *testing.T, srv *server.Server, checks ...check) {
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.run(t, srv)
		})
	}
}

// TestOverworldOfSeed12345 reads seed 12345's noise overworld, structures
// on, through one server whose loop is stopped. The fluid checks tick it by
// hand one after the other; Go runs them before the parallel scans start.
func TestOverworldOfSeed12345(t *testing.T) {
	t.Parallel()
	srv := startFluidWorld(t)
	t.Run("GeneratedLavaStaysPut", func(t *testing.T) { overworldGeneratedLavaStaysPut(t, srv) })
	t.Run("SpringsFlowWhenTheirChunkTicks", func(t *testing.T) { overworldSpringsFlowWhenTheirChunkTicks(t, srv) })
	runParallel(t, srv,
		check{"EmeraldOnlyInMountainBiomes", overworldEmeraldOnlyInMountainBiomes},
	)
}

// TestOverworldOresOfTheOriginRegion scans the structure-free 5x5 chunks of
// seed 12345 around the origin through one server.
func TestOverworldOresOfTheOriginRegion(t *testing.T) {
	t.Parallel()
	srv := servertest.StartOreWorld(t, func(cfg *config.Config) {
		cfg.GenerateStructures = false
		cfg.LevelName = copyRegion(t, pipelineSeed)
	})
	runParallel(t, srv,
		check{"GrowsTheOtherOres", overworldGrowsTheOtherOres},
		check{"OresGetTheDeepslateVariant", overworldOresGetTheDeepslateVariant},
		check{"ScattersStoneBlobs", overworldScattersStoneBlobs},
	)
}

// TestOverworldSurfaceRegion serves surfaceRegion, without structures
// covering the ground, to the biome and surface checks.
func TestOverworldSurfaceRegion(t *testing.T) {
	t.Parallel()
	srv := servertest.StartOreWorld(t, func(cfg *config.Config) {
		cfg.GenerateStructures = false
		cfg.LevelName = surfaceRegion.Copy(t)
	})
	runParallel(t, srv,
		check{"BiomesAreTheClimateBiomes", overworldBiomesAreTheClimateBiomes},
		check{"SurfaceFollowsBiome", overworldSurfaceFollowsBiome},
	)
}

// TestTheNetherOfSeed12345 sweeps seed 12345's nether through one server.
func TestTheNetherOfSeed12345(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelSeed = "12345"
	srv, _ := servertest.Start(t, cfg)
	runParallel(t, srv,
		check{"GrowsVanillaOres", theNetherGrowsVanillaOres},
		check{"BuriesAncientDebris", theNetherBuriesAncientDebris},
	)
}

func sharedVegetationWorld(t *testing.T, region servertest.RegionFixture) *world.World {
	t.Helper()
	gen := world.NewOverworldGenerator(region.Seed)
	gen.DisableStructures()
	w, err := world.NewWorld(region.Copy(t), gen)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

type fixtureGenerator struct {
	world.Generator
	generated atomic.Int32
}

func (g *fixtureGenerator) Generate(c *world.Chunk) {
	g.generated.Add(1)
	g.Generator.Generate(c)
}
