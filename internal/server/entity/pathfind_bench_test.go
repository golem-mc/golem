package entity

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkZombieOpen16Chunks reads production chunk storage rather than the
// cell-by-cell test world, including the search's section-cache fills.
func BenchmarkZombieOpen16Chunks(b *testing.B) {
	terrain := world.NewDimensionWorld(b.TempDir(), true, world.NewFlatGenerator())
	defer terrain.Close()
	for _, pos := range []world.ChunkPos{{-1, -1}, {-1, 0}, {0, -1}, {0, 0}, {1, -1}, {1, 0}} {
		terrain.Acquire(pos)
		defer terrain.Release(pos)
	}
	w := herdBlockWorld{testWorld: newTestWorld(), terrain: terrain}
	foot := footprintOf(0.6, 1.95)
	from, to := Waypoint{0, 64, 0}, Waypoint{16, 64, 0}
	for _, r := range []int32{16, 35} {
		b.Run(fmt.Sprint(r), func(b *testing.B) {
			if path, reached := findPath(w, foot, from, to, r, 0); !reached || len(path) != 16 {
				b.Fatalf("path %v, reached=%v", path, reached)
			}
			for b.Loop() {
				findPath(w, foot, from, to, r, 0)
			}
		})
	}
}
