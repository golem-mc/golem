package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func TestBiomeReadsZoomedQuartAndNoiseBiomeTheOwnCell(t *testing.T) {
	g := NewOverworldGenerator(0)
	w := newTestWorld(t, g)
	probe := g.NewBiomeProbe()
	const y = 64
	// A block on its chunk's east edge whose zoomed quart is in the next
	// chunk and names another biome than its own cell.
	var bx, bz int32
	found := false
	for x := int32(15); x < 16*40 && !found; x += 16 {
		for z := int32(0); z < 16 && !found; z++ {
			qx, qy, qz := levelgen.ZoomedQuart(w.zoomSeed, x, y, z)
			if qx*4>>4 != x>>4 && probe(qx, qy, qz) != probe(x>>2, y>>2, z>>2) {
				bx, bz, found = x, z, true
			}
		}
	}
	if !found {
		t.Fatal("no block whose zoomed quart is a neighbour's other biome")
	}
	qx, qy, qz := levelgen.ZoomedQuart(w.zoomSeed, bx, y, bz)
	zoomed, own := probe(qx, qy, qz), probe(bx>>2, y>>2, bz>>2)

	load := func(pos ChunkPos) {
		w.Acquire(pos)
	}
	load(ChunkPos{X: bx >> 4, Z: bz >> 4})
	if w.ChunkLoaded(ChunkPos{X: qx * 4 >> 4, Z: qz * 4 >> 4}) {
		t.Fatal("neighbour chunk is loaded")
	}
	check := func(when string) {
		t.Helper()
		if got := w.Biome(bx, y, bz).Name(); got != zoomed {
			t.Fatalf("%s: Biome %q, want zoomed %q", when, got, zoomed)
		}
		if got := w.NoiseBiome(bx, y, bz).Name(); got != own {
			t.Fatalf("%s: NoiseBiome %q, want own cell %q", when, got, own)
		}
	}
	check("neighbour unloaded")
	load(ChunkPos{X: qx * 4 >> 4, Z: qz * 4 >> 4})
	check("neighbour loaded")
	if w.Biome(1<<20, y, 0) == registry.NoBiome {
		t.Fatal("an unloaded chunk should fall back to the generator")
	}
}

// biomeTestWorld is seed 0's overworld with chunks -2..1 on both axes loaded
// and everything else unloaded.
func biomeTestWorld(t testing.TB) (*World, *OverworldGenerator) {
	g := NewOverworldGenerator(0)
	w := newTestWorld(t, g)
	for cx := int32(-2); cx <= 1; cx++ {
		for cz := int32(-2); cz <= 1; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	return w, g
}

func TestBiomeMatchesZoomedQuartNearUnloadedChunks(t *testing.T) {
	w, g := biomeTestWorld(t)
	probe := g.NewBiomeProbe()
	minQY, maxQY := w.MinY()>>2, (w.MinY()+w.Height()-1)>>2
	for x := int32(-40); x < 40; x++ {
		for z := int32(-40); z < 40; z++ {
			for _, y := range []int32{-64, 0, 63, 64, 100, 319} {
				qx, qy, qz := levelgen.ZoomedQuart(w.zoomSeed, x, y, z)
				qy = min(max(qy, minQY), maxQY)
				w.mu.RLock()
				loaded, ok := w.chunks[ChunkPos{X: qx >> 2, Z: qz >> 2}]
				w.mu.RUnlock()
				var want string
				if ok {
					want = registry.Biome(loaded.chunk.BiomeAt((qx*4)&15, qy*4, (qz*4)&15)).Name()
				} else {
					want = probe(qx, qy, qz)
				}
				if got := w.Biome(x, y, z).Name(); got != want {
					t.Fatalf("Biome(%d, %d, %d) = %q, want %q", x, y, z, got, want)
				}
			}
		}
	}
}

func BenchmarkBiome(b *testing.B) {
	w, _ := biomeTestWorld(b)
	for b.Loop() {
		for x := int32(-16); x < 16; x++ {
			for z := int32(-16); z < 16; z++ {
				w.Biome(x, 64, z)
			}
		}
	}
}
