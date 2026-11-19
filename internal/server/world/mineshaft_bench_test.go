package world

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// BenchmarkMineshaftCartGenerateAndSave generates seed 3's two-cart chunk,
// lights it and hands its starting entities to the entity region before unload.
// Each iteration uses a fresh generator and save directory so neither cached
// decoration nor a saved chunk can bypass cart creation.
func BenchmarkMineshaftCartGenerateAndSave(b *testing.B) {
	root := b.TempDir()
	pos := ChunkPos{X: -19, Z: 10}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir := filepath.Join(root, strconv.Itoa(i))
		g := NewOverworldGenerator(3)
		g.Generate(newChunk(1000, 1000)) // shared structure/template initialization
		w, err := NewWorld(dir, g)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		w.Acquire(pos)
		w.Release(pos)
		w.FlushSaves()
		b.StopTimer()
		tags, _, err := w.LoadChunkEntities(pos)
		if err != nil {
			b.Fatal(err)
		}
		carts := 0
		for _, tag := range tags {
			if c, ok := tag.(nbt.Compound); ok && c["id"] == nbt.String("minecraft:chest_minecart") {
				carts++
			}
		}
		b.ReportMetric(float64(carts), "carts/op")
		w.Close()
		if err := os.RemoveAll(dir); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
