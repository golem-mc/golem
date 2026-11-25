package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkLiveColumnHeight(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{}
	w.Acquire(pos)
	b.Cleanup(func() { w.Release(pos) })

	const x, z = 8, 8
	w.SetBlock(x, 64, z, registry.BlockStone.DefaultState())
	w.SetBlock(x, 65, z, registry.BlockOakLeaves.DefaultState())
	w.SetBlock(x, 66, z, registry.BlockWater.DefaultState())

	for _, query := range []struct {
		name   string
		height func(int32, int32) int32
		want   int32
	}{
		{"NoLeavesHeight", w.NoLeavesHeight, 66},
		{"OceanFloorHeight", w.OceanFloorHeight, 65},
	} {
		b.Run(query.name, func(b *testing.B) {
			if got := query.height(x, z); got != query.want {
				b.Fatalf("height = %d, want %d", got, query.want)
			}
			b.ReportAllocs()
			for b.Loop() {
				query.height(x, z)
			}
		})
	}
}

// BenchmarkEncodeHeightmaps is the three client heightmaps of one flat chunk,
// built each time a chunk is generated, loaded or changed.
func BenchmarkEncodeHeightmaps(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	loaded, _ := w.Acquire(ChunkPos{})
	b.ReportAllocs()
	for range b.N {
		w.encodeHeightmaps(loaded)
	}
}

// BenchmarkVoidChunkHeightmaps is the three client heightmaps of an empty End
// chunk, whose sections are all uniform air.
func BenchmarkVoidChunkHeightmaps(b *testing.B) {
	w := &World{dim: dimensionShapes[EndName]}
	c := newChunk(0, 0)
	b.ReportAllocs()
	for range b.N {
		w.encodeHeightmaps(c)
	}
}
