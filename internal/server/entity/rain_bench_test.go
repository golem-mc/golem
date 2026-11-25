package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type rainBenchWorld struct {
	testWorld
	terrain *world.World
}

func (w *rainBenchWorld) RainingAt(x, y, z int32) bool {
	return w.terrain.RainingAt(x, y, z)
}

func BenchmarkRainOnItem(b *testing.B) {
	terrain, err := world.NewWorld(b.TempDir(), world.NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = terrain.Close() })
	terrain.Acquire(world.ChunkPos{})
	terrain.SetBlock(8, 68, 8, registry.MustState("minecraft:glass", nil))
	terrain.Relight(8, 8)
	terrain.SetWeather(true, false, 0, 0, 0)
	for range 21 {
		terrain.TickWeather()
	}
	// Glass lets the lookup reach the motion-blocking height check.
	if terrain.SkyLightAt(8, 64, 8) != 15 || !terrain.RainingAt(8, 69, 8) {
		b.Fatal("expected full skylight under the glass and rain above it")
	}

	w := &rainBenchWorld{terrain: terrain}
	for _, tc := range []struct {
		name string
		feet float64
	}{
		{"same_cell", 64.1},
		{"crossing_cells", 64.9},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if rainLandsOn(w, 8.5, tc.feet, 8.5, 0.25) {
					b.Fatal("rain reached the item under glass")
				}
			}
		})
	}
}
