package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func testSpawnBiomeSources(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	positions := []ChunkPos{{X: -1, Z: -1}, {X: -1}, {Z: -1}, {}}
	for _, pos := range positions {
		w.Acquire(pos)
	}
	defer func() {
		for _, pos := range positions {
			w.Release(pos)
		}
	}()
	s, _ := w.HoldSpawnChunk(ChunkPos{})
	defer s.Close()
	// Find an edge selection in the held origin with an unselected neighbour.
	var x, z int32
	found := false
	for xx := int32(0); xx <= 2 && !found; xx++ {
		for zz := int32(0); zz <= 2; zz++ {
			qx, _, qz := levelgen.ZoomedQuart(w.zoomSeed, xx, 64, zz)
			px, _, pz := levelgen.ZoomParent(xx, 64, zz)
			if qx>>2 == 0 && qz>>2 == 0 && (px < 0 || pz < 0) {
				x, z, found = xx, zz, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no edge fixture")
	}
	qx, qy, qz := levelgen.ZoomedQuart(w.zoomSeed, x, 64, z)
	px, _, pz := levelgen.ZoomParent(x, 64, z)
	missing := ChunkPos{X: px >> 2, Z: pz >> 2}
	check := func(want registry.Biome) {
		t.Helper()
		s.ResetSpawnStep()
		if a, b := w.Biome(x, 64, z), s.Biome(x, 64, z); a != want || b != want {
			t.Fatalf("public/held %v/%v, want %v", a, b, want)
		}
	}
	fill := func(b registry.Biome) {
		t.Helper()
		_, _, ok := w.FillBiomes(BiomeBox{MinX: 0, MaxX: 15, MinY: 48, MaxY: 80, MinZ: 0, MaxZ: 15}, int32(b), nil)
		if !ok {
			t.Fatal("fill failed")
		}
	}
	fill(registry.BiomeSwamp)
	check(registry.BiomeSwamp)
	w.Release(missing)
	w.FlushSaves()
	check(registry.BiomeSwamp) // missing unselected source does not affect selection
	w.Acquire(missing)
	check(registry.BiomeSwamp)
	// A synchronous sink may edit the same raw cells before the next step.
	func(sink func()) { s.Biome(x, 64, z); sink(); s.ResetSpawnStep() }(func() { fill(registry.BiomeTaiga) })
	check(registry.BiomeTaiga)
	fill(registry.NoBiome)
	check(registry.NoBiome) // all source columns loaded: preserve stored sentinel
	w.Release(missing)
	w.FlushSaves()
	want, _ := registry.BiomeByName(w.gen.(BiomeSampler).NewBiomeProbe()(qx, qy, qz))
	check(want) // missing unselected column: historical generator fallback
	w.Acquire(missing)
	check(registry.NoBiome)
	// Uniform sentinel shortcut keeps its result without generator fallback.
	s.ResetSpawnStep()
	if a, b := w.Biome(6, 70, 6), s.Biome(6, 70, 6); a != registry.NoBiome || b != registry.NoBiome {
		t.Fatalf("uniform sentinel %v/%v", a, b)
	}
	// The selected neighbour is initially absent, then loaded, unloaded and reloaded.
	w.Release(ChunkPos{X: -1, Z: -1})
	w.FlushSaves()
	for range 2 {
		for _, loaded := range []bool{false, true} {
			if loaded {
				w.Acquire(ChunkPos{X: -1, Z: -1})
			}
			for xx := int32(-2); xx <= 1; xx++ {
				for zz := int32(-2); zz <= 1; zz++ {
					a, b, c := levelgen.ZoomedQuart(w.zoomSeed, xx, 64, zz)
					want := w.NoiseBiome(a*4, b*4, c*4)
					if want == registry.NoBiome {
						want, _ = registry.BiomeByName(w.gen.(BiomeSampler).NewBiomeProbe()(a, b, c))
					}
					// The origin still contains sentinels; use non-sentinel sources here.
					if a>>2 == 0 && c>>2 == 0 {
						continue
					}
					s.ResetSpawnStep()
					if w.Biome(xx, 64, zz) != want || s.Biome(xx, 64, zz) != want {
						t.Fatal("selected load/reload")
					}
				}
			}
		}
		w.Release(ChunkPos{X: -1, Z: -1})
		w.FlushSaves()
	}
	w.Acquire(ChunkPos{X: -1, Z: -1})
}
