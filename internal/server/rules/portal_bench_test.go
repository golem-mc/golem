package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func benchmarkSearchWorld(b *testing.B) *world.World {
	b.Helper()
	w, err := world.NewWorld(b.TempDir(), world.NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { w.Close() })
	w.Acquire(world.ChunkPos{})
	return w
}

func BenchmarkPortalScanChunk(b *testing.B) {
	for _, portal := range []bool{false, true} {
		name := "no_portal"
		if portal {
			name = "one_portal"
		}
		b.Run(name, func(b *testing.B) {
			w := benchmarkSearchWorld(b)
			if portal {
				w.SetBlock(8, 64, 8, NetherPortalX)
			}
			b.ReportAllocs()
			for b.Loop() {
				s := PortalSearch{At: world.SpawnPos{X: 8, Y: 64, Z: 8}, Radius: 128}
				s.ScanChunk(w, world.ChunkPos{}, w.MinY(), w.MaxY()-1)
				if _, found := s.Found(); found != portal {
					b.Fatal("unexpected portal result")
				}
			}
		})
	}
}

func BenchmarkVillageScanChunk(b *testing.B) {
	w := benchmarkSearchWorld(b)
	v := NewVillageIndex(w)
	var tick int64
	b.ReportAllocs()
	for b.Loop() {
		tick += VillageScanTicks
		if len(v.InChunk(world.ChunkPos{}, tick)) != 0 {
			b.Fatal("unexpected POI")
		}
	}
}
