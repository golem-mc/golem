package servertest

import (
	"os"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestMain(m *testing.M) {
	os.Exit(RunMain(m))
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
