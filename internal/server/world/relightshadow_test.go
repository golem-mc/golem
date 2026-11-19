package world

import (
	"bytes"
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// relightInPlace is relightArea's work done on the world's own arrays under
// the write lock, with no shadow: the light a shadow's relight must match.
func relightInPlace(w *World, minX, minZ, maxX, maxZ int32) []LightChange {
	w.mu.Lock()
	defer w.mu.Unlock()
	wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ := minX-lightRadius, minZ-lightRadius, maxX+lightRadius, maxZ+lightRadius
	if !w.lightDirtyIn(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ, false) {
		return nil
	}
	// The in-place writes mutate the arrays, so the run is compared with a
	// deep copy taken before it.
	type light struct {
		sky, block [lightSections]*[lightArrayLen]byte
	}
	before := make(map[ChunkPos]light, len(w.chunks))
	for pos, loaded := range w.chunks {
		var l light
		for si := range lightSections {
			for layer, dst := range []*[lightSections]*[lightArrayLen]byte{&l.sky, &l.block} {
				if a := loaded.chunk.lightLayer(layer)[si]; a != nil {
					cp := *a
					dst[si] = &cp
				}
			}
		}
		before[pos] = l
	}
	if cells, ok := w.dirtyCells(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ); ok {
		w.lightDirtyIn(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ, true)
		w.relightCells(cells)
	} else {
		w.lightDirtyIn(minX, minZ, maxX, maxZ, true)
		w.relightBox(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ)
	}
	var want []LightChange
	for pos, loaded := range w.chunks {
		c, was := loaded.chunk, before[pos]
		ch := LightChange{Pos: pos,
			Sky:   changedLightSections(&was.sky, &c.skyLight),
			Block: changedLightSections(&was.block, &c.blockLight)}
		if ch.Sky|ch.Block != 0 {
			want = append(want, ch)
		}
	}
	return want
}

// liveLight is every loaded chunk's light as its arrays hold it.
func liveLight(w *World) map[ChunkPos][]byte {
	w.mu.RLock()
	defer w.mu.RUnlock()
	light := make(map[ChunkPos][]byte, len(w.chunks))
	for pos, loaded := range w.chunks {
		light[pos] = loaded.chunk.encodeLight()
	}
	return light
}

// TestShadowRelightMatchesInPlaceRelight runs the writes that relight most
// (a torch, a shaft under the sky, a lava flow, explosion craters small and
// past the cell cap, a region written during every compute) on two copies of
// generated terrain. One relights through shadows, the other on its own
// arrays: after each step the light and the sections reported changed must be
// the same, and the shadow's compute must leave the live light as it was.
// Both end at a full relight's light.
func TestShadowRelightMatchesInPlaceRelight(t *testing.T) {
	t.Parallel()
	load := func() *World {
		w := newTestWorld(t, testTerrain(31, false))
		for cx := int32(-2); cx <= 2; cx++ {
			for cz := int32(-2); cz <= 2; cz++ {
				w.Acquire(ChunkPos{cx, cz})
			}
		}
		return w
	}
	w, ref := load(), load()
	lava := registry.MustState("minecraft:lava", nil)
	andesite := registry.MustState("minecraft:andesite", nil)
	surface := func(x, z int32) int32 { return w.SurfaceHeight(x, z) }
	crater := func(cx, cy, cz, r int32) func(*World) {
		return func(w *World) {
			var changes []BlockChange
			for x := -r; x <= r; x++ {
				for y := -r; y <= r; y++ {
					for z := -r; z <= r; z++ {
						if x*x+y*y+z*z <= r*r {
							changes = append(changes, BlockChange{X: cx + x, Y: cy + y, Z: cz + z, State: airState})
						}
					}
				}
			}
			w.SetBlocks(changes)
		}
	}
	type step struct {
		name  string
		write func(*World)
		box   [4]int32 // the relit columns: minX, minZ, maxX, maxZ
		busy  bool     // every off-lock compute sees a write land, so the last try holds the lock
		whole bool     // more writes than a chunk keeps cells for, so the box is wiped (relightBox)
	}
	torchY := surface(8, 8) + 1
	shaftY := surface(-10, 12)
	busyY := surface(-4, -20) + 1
	steps := []step{
		{name: "torch placed", write: func(w *World) { w.SetBlock(8, torchY, 8, torchState) }, box: [4]int32{8, 8, 8, 8}},
		{name: "glowstone underground", write: func(w *World) { w.SetBlock(20, 10, 4, glowstoneState) }, box: [4]int32{20, 4, 20, 4}},
		{name: "shaft under the sky", write: func(w *World) {
			for dy := range int32(24) {
				w.SetBlock(-10, shaftY-dy, 12, airState)
			}
		}, box: [4]int32{-10, 12, -10, 12}},
		{name: "shaft roofed", write: func(w *World) { w.SetBlock(-10, shaftY+1, 12, stoneState) }, box: [4]int32{-10, 12, -10, 12}},
		{name: "crater", write: crater(24, surface(24, 24), 24, 4), box: [4]int32{20, 20, 28, 28}},
		{name: "crater past the cell cap", write: crater(-24, surface(-24, -24), -24, 8), box: [4]int32{-32, -32, -16, -16}, whole: true},
		{name: "torch removed", write: func(w *World) { w.SetBlock(8, torchY, 8, airState) }, box: [4]int32{8, 8, 8, 8}},
		{name: "torch in a busy region", write: func(w *World) { w.SetBlock(-4, busyY, -20, torchState) },
			box: [4]int32{-4, -20, -4, -20}, busy: true},
	}
	for i := range int32(10) {
		x, y := 4+i, surface(4+i, -6)+1
		steps = append(steps, step{name: "lava flows", write: func(w *World) { w.SetBlock(x, y, -6, lava) }, box: [4]int32{x, -6, x, -6}})
	}
	for i := int32(9); i >= 0; i-- {
		x, y := 4+i, surface(4+i, -6)+1
		steps = append(steps, step{name: "lava drains", write: func(w *World) { w.SetBlock(x, y, -6, airState) }, box: [4]int32{x, -6, x, -6}})
	}

	for _, st := range steps {
		st.write(w)
		st.write(ref)
		w.mu.RLock()
		_, incremental := w.dirtyCells(st.box[0]-lightRadius, st.box[1]-lightRadius, st.box[2]+lightRadius, st.box[3]+lightRadius)
		w.mu.RUnlock()
		if incremental == st.whole {
			t.Errorf("%s: relit from its cells %v, want %v", st.name, incremental, !st.whole)
		}
		before := liveLight(w)
		computes := 0
		w.relightComputed = func() {
			for pos, light := range liveLight(w) {
				if !bytes.Equal(light, before[pos]) {
					t.Errorf("%s: chunk %v's live light changed before the install", st.name, pos)
				}
			}
			if st.busy {
				// A write the light does not see, which still fails the
				// currency check.
				x, y, z := st.box[0]+int32(computes), int32(worldMinY+3), st.box[1]
				if !registry.SameLight(w.BlockAt(x, y, z), andesite) {
					t.Fatalf("%s: the busy write at (%d,%d,%d) would change the light", st.name, x, y, z)
				}
				w.SetBlock(x, y, z, andesite)
				ref.SetBlock(x, y, z, andesite)
			}
			computes++
		}
		got := w.relightArea(st.box[0], st.box[1], st.box[2], st.box[3])
		w.relightComputed = nil
		want := relightInPlace(ref, st.box[0], st.box[1], st.box[2], st.box[3])
		if st.busy && computes != relightTries {
			t.Errorf("%s: %d off-lock computes, want %d before the locked one", st.name, computes, relightTries)
		}
		cmpChange := func(a, b LightChange) int { return cmpChunkPos(a.Pos, b.Pos) }
		slices.SortFunc(got, cmpChange)
		slices.SortFunc(want, cmpChange)
		if !slices.Equal(got, want) {
			t.Errorf("%s: the shadow relight changed the light of %v, the in-place one %v", st.name, got, want)
		}
		assertSameLight(t, w, ref)
	}
	ref.mu.Lock()
	ref.relightBox(-32, -32, 47, 47)
	ref.mu.Unlock()
	assertSameLight(t, w, ref)
}
