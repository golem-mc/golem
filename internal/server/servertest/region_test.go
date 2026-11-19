package servertest

import (
	"os"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestMain(m *testing.M) {
	os.Exit(RunMain(m))
}

func TestRegionFixtureReadersAreIndependent(t *testing.T) {
	t.Parallel()
	for _, nether := range []bool{false, true} {
		name := "overworld"
		if nether {
			name = "nether"
		}
		t.Run(name, func(t *testing.T) {
			fixture := RegionFixture{Seed: 12, Nether: nether}
			open := func() *world.World {
				var source world.Generator = world.NewOverworldGenerator(fixture.Seed)
				if nether {
					source = world.NewNetherGenerator(fixture.Seed)
				}
				gen := &regionGenerator{Generator: source}
				dir := fixture.Copy(t)
				var w *world.World
				if nether {
					w = world.NewDimensionWorld(dir, true, gen)
				} else {
					var err error
					w, err = world.NewWorld(dir, gen)
					if err != nil {
						t.Fatal(err)
					}
				}
				t.Cleanup(func() { _ = w.Close() })
				pos := world.ChunkPos{}
				w.Acquire(pos)
				t.Cleanup(func() { w.Release(pos) })
				if gen.generated.Load() != 0 {
					t.Fatal("fixture chunk was regenerated")
				}
				return w
			}
			a, b := open(), open()
			before := b.BlockAt(0, 200, 0)
			if before != registry.BlockAir.DefaultState() {
				t.Fatal("expected air above the fixture's terrain")
			}
			a.SetBlock(0, 200, 0, registry.BlockStone.DefaultState())
			a.Save()
			a.FlushSaves()
			if b.BlockAt(0, 200, 0) != before || open().BlockAt(0, 200, 0) != before {
				t.Fatal("reader edits reached another copy or the cached source")
			}
		})
	}
}

func TestRegionFixtureSeparatesSeedsAndBounds(t *testing.T) {
	t.Parallel()
	for _, fixture := range []RegionFixture{
		{Seed: 12},
		{Seed: 13},
		{Seed: 12, Max: world.ChunkPos{X: 1}},
	} {
		gen := &regionGenerator{Generator: world.NewOverworldGenerator(fixture.Seed)}
		w, err := world.NewWorld(fixture.Copy(t), gen)
		if err != nil {
			t.Fatal(err)
		}
		w.Acquire(fixture.Max)
		w.Release(fixture.Max)
		w.Close()
		if w.SeedValue() != fixture.Seed || gen.generated.Load() != 0 {
			t.Fatalf("fixture %+v loaded seed %d and regenerated %d chunks", fixture, w.SeedValue(), gen.generated.Load())
		}
	}
}

type regionGenerator struct {
	world.Generator
	generated atomic.Int32
}

func (g *regionGenerator) Generate(c *world.Chunk) {
	g.generated.Add(1)
	g.Generator.Generate(c)
}

func BenchmarkCopyGeneratedRegion(b *testing.B) {
	// The water-spawning tests join at view and simulation distance four.
	fixture := RegionFixture{
		Seed: 12,
		Min:  world.ChunkPos{X: -4, Z: -4}, Max: world.ChunkPos{X: 4, Z: 4},
	}
	fixture.Copy(b) // Measure readers after the shared generation has finished.
	for b.Loop() {
		fixture.Copy(b)
	}
}
