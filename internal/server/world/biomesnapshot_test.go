package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func TestSpawnBiomeSnapshots(t *testing.T) {
	t.Run("sources", testSpawnBiomeSources)
	w := newTestWorld(t, NewFlatGenerator())
	positions := []ChunkPos{{X: -1, Z: -1}, {X: -1}, {Z: -1}, {}}
	for _, pos := range positions {
		w.Acquire(pos)
		defer w.Release(pos)
	}
	s, ok := w.HoldSpawnChunk(ChunkPos{})
	if !ok {
		t.Fatal("missing origin")
	}
	defer s.Close()
	// Match the original selected-quart read independently of the grid shortcut.
	check := func() {
		t.Helper()
		for x := int32(-18); x <= 18; x++ {
			for z := int32(-18); z <= 18; z++ {
				for _, y := range []int32{-80, -64, -63, -33, -32, -31, -1, 0, 15, 16, 63, 64, 65, 79, 95, 96, 255, 256, 319, 320, 340} {
					qx, qy, qz := levelgen.ZoomedQuart(w.zoomSeed, x, y, z)
					qy = min(max(qy, w.MinY()>>2), (w.MinY()+w.Height()-1)>>2)
					want := w.NoiseBiome(qx*4, qy*4, qz*4)
					if want == registry.NoBiome {
						want, _ = registry.BiomeByName(w.gen.(BiomeSampler).NewBiomeProbe()(qx, qy, qz))
					}
					s.ResetSpawnStep()
					if got := s.Biome(x, y, z); got != want {
						t.Fatalf("held biome(%d,%d,%d)=%v, want %v", x, y, z, got, want)
					}
					if got := w.Biome(x, y, z); got != want {
						t.Fatalf("biome(%d,%d,%d)=%v, want %v", x, y, z, got, want)
					}
				}
			}
		}
	}
	check()
	// Both parent quarts on all axes share a section here. Exercise the
	// uniform shortcut and then the same section's full mixed-grid zoom.
	for _, biome := range []registry.Biome{registry.BiomeSwamp, registry.BiomePlains} {
		w.mu.Lock()
		w.chunks[ChunkPos{}].chunk.SetCellBiome(4, 68, 4, int32(biome))
		w.mu.Unlock()
		qx, qy, qz := levelgen.ZoomedQuart(w.zoomSeed, 6, 70, 6)
		if got, want := w.Biome(6, 70, 6), w.NoiseBiome(qx*4, qy*4, qz*4); got != want {
			t.Fatalf("same-section biome %v, want %v", got, want)
		}
	}
	origin := w.chunks[ChunkPos{}].chunk
	sec := &origin.sections[(64-worldMinY)/16]
	original := w.rawBiomeGrid(sec)
	w.SetBlock(4, 64, 4, registry.BlockStone.DefaultState())
	if w.rawBiomeGrid(sec) != original {
		t.Fatal("block write invalidated biome grid")
	}
	var mixed *biomeSnapshot
	for _, b := range []registry.Biome{registry.BiomeSwamp, registry.BiomePlains} {
		w.mu.Lock()
		origin.SetCellBiome(4, 64, 4, int32(b))
		w.mu.Unlock()
		check()
		if mixed != nil && mixed.at(1, 16, 1) != int32(registry.BiomeSwamp) {
			t.Fatal("mixed snapshot aliases live cells")
		}
		grid := w.rawBiomeGrid(sec)
		if b == registry.BiomeSwamp {
			mixed = grid
		}
		if original.at(1, 16, 1) != int32(registry.BiomePlains) {
			t.Fatal("published grid mutated")
		}
		w.mu.Lock()
		for i := range origin.sections {
			origin.sections[i].settleBiomes()
		}
		w.mu.Unlock()
		check()
		if b == registry.BiomePlains && w.rawBiomeGrid(sec).cells != nil {
			t.Fatal("uniform grid kept cells")
		}
		if b == registry.BiomeSwamp && grid.cells == nil {
			t.Fatal("mixed grid lost cells")
		}
	}
	w.mu.Lock()
	origin.SetBiome(int32(registry.BiomeTaiga))
	for _, y := range []int32{-64, -32, 95, 255, 319} {
		origin.SetCellBiome(0, y, 0, int32(registry.BiomeSwamp))
	}
	w.mu.Unlock()
	check()
	// A near-slot collision must find and republish both chunks, while an
	// unloaded source must become visible as soon as it loads.
	w.Acquire(ChunkPos{X: 64})
	defer w.Release(ChunkPos{X: 64})
	for range 2 {
		w.Biome(1028, 64, 4)
		check()
	}
	w.Acquire(ChunkPos{X: 1})
	defer w.Release(ChunkPos{X: 1})
	check()
	for _, shape := range []dimensionShape{{minY: 0, height: 256}, {minY: -32, height: 128}} {
		w.dim = shape
		check()
	}
	// Writers use the same lock as FillBiomes; readers must never retain or
	// inspect the mutable section slice, even while a new grid is published.
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			w.mu.Lock()
			origin.SetCellBiome(4, 64, 4, int32(registry.Biome(i%2)))
			if i%3 == 0 {
				origin.SetBiome(int32(registry.BiomePlains))
			}
			w.mu.Unlock()
		}
	})
	for range 200 {
		w.Biome(4, 64, 4)
		w.Biome(0, 63, 0)
		s.ResetSpawnStep()
		s.Biome(0, 63, 0)
	}
	wg.Wait()
}

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
