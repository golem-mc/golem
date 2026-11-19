package world

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// acquireWholeBox acquires pos the way Acquire did before the light was split
// in two: the chunk inserted unlit, then its box recomputed by relightBox
// under the write lock.
func acquireWholeBox(w *World, pos ChunkPos) []ChunkPos {
	w.mu.Lock()
	defer w.mu.Unlock()
	if loaded, ok := w.chunks[pos]; ok {
		loaded.users.Add(1)
		return nil
	}
	c, err := w.loadChunk(pos)
	if err != nil || c == nil {
		c = newChunk(pos.X, pos.Z)
		w.gen.Generate(c)
		w.encodeChunk(c)
	}
	loaded := &loadedChunk{chunk: c, motion: w.primeMotion(c)}
	loaded.users.Store(1)
	w.chunks[pos] = loaded
	w.appendBlockEntityOrder(c.blockEntities()...)
	if !c.lightCorrect {
		c.lightDirty = [sectionSize]uint16{}
		// The chunks the box can reach, as encoded before the relight.
		old := make(map[ChunkPos][]byte)
		for cx := pos.X - 1; cx <= pos.X+1; cx++ {
			for cz := pos.Z - 1; cz <= pos.Z+1; cz++ {
				if l, ok := w.chunks[ChunkPos{cx, cz}]; ok {
					old[ChunkPos{cx, cz}] = l.chunk.lightForm()
				}
			}
		}
		w.relightBox(pos.X*16, pos.Z*16, pos.X*16+15, pos.Z*16+15)
		var changed []ChunkPos
		for p, enc := range old {
			if enc == nil || !bytes.Equal(enc, w.chunks[p].chunk.lightForm()) {
				changed = append(changed, p)
			}
		}
		return changed
	}
	return nil
}

// TestJoinedLightMatchesTheWholeBox lights the same terrain twice, one world
// acquiring through the split light (each chunk lit alone, then across its
// borders) and one through the whole-box recompute, in shuffled orders with
// lights, roofs and releases between, and holds every light array equal.
func TestJoinedLightMatchesTheWholeBox(t *testing.T) {
	t.Parallel()
	// Two seeds cover different terrain while the shuffled 3x3 joins cover
	// interior, edge and corner neighbours.
	for _, seed := range []uint64{0, 3} {
		rng := rand.New(rand.NewPCG(seed, 99))
		// Only the springs are placed: the trees around a chunk depend on
		// which neighbours were decorated first, and a chunk the whole-box
		// world regenerates would not come back as the split world's.
		gen := testTerrain(int64(seed), true)
		split, whole := newTestWorld(t, gen), newTestWorld(t, gen)

		var positions []ChunkPos
		// A 3x3 includes interior, edge and diagonal joins, and enough acquisitions
		// to exercise the release/reload every fifth chunk.
		for cx := int32(-1); cx <= 1; cx++ {
			for cz := int32(-1); cz <= 1; cz++ {
				positions = append(positions, ChunkPos{cx, cz})
			}
		}
		rng.Shuffle(len(positions), func(i, j int) { positions[i], positions[j] = positions[j], positions[i] })

		blocks := []uint16{
			stoneState, torchState, airState,
			registry.BlockStates["minecraft:glowstone"],
			registry.BlockStates["minecraft:oak_leaves"],
			registry.BlockStates["minecraft:glass"],
		}
		edit := func() {
			pos := positions[rng.IntN(len(positions))]
			x, z := pos.X*16+rng.Int32N(16), pos.Z*16+rng.Int32N(16)
			y := 50 + rng.Int32N(40)
			state := blocks[rng.IntN(len(blocks))]
			roof := rng.IntN(3) == 0
			for _, w := range []*World{split, whole} {
				if roof {
					for d := range int32(25) {
						w.SetBlock(x+d%5, y+3, z+d/5, stoneState)
					}
				}
				w.SetBlock(x, y, z, state)
				// Every written column is relit, as the server's relights
				// cover the changes they follow.
				w.relightArea(x, z, x+4, z+4)
			}
		}

		for i, pos := range positions {
			split.Acquire(pos)
			acquireWholeBox(whole, pos)
			for range 3 {
				edit()
			}
			// Now and then a chunk goes and comes back from its region file.
			if i%5 == 4 {
				back := positions[rng.IntN(i+1)]
				split.Release(back)
				whole.Release(back)
				split.FlushSaves()
				whole.FlushSaves()
				split.Acquire(back)
				acquireWholeBox(whole, back)
			}
		}
		compareLight(t, seed, split, whole, positions)
		for _, pos := range positions {
			split.Release(pos)
			whole.Release(pos)
		}
	}
}

// TestJoinedLightChangesMatch holds the chunks a fresh acquire reports with
// changed light equal between the split and the whole-box light.
func TestJoinedLightChangesMatch(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(3, 3))
	gen := testTerrain(3, false)
	split := newTestWorld(t, gen)
	whole := newTestWorld(t, gen)
	var positions []ChunkPos
	// A 3x3 exercises interior and diagonal borders; farther chunks cannot
	// contribute to this chunk's 15-block light reach.
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			positions = append(positions, ChunkPos{cx, cz})
		}
	}
	rng.Shuffle(len(positions), func(i, j int) { positions[i], positions[j] = positions[j], positions[i] })
	for _, pos := range positions {
		_, got := split.Acquire(pos)
		want := acquireWholeBox(whole, pos)
		slices.SortFunc(got, cmpChunkPos)
		slices.SortFunc(want, cmpChunkPos)
		if !slices.Equal(got, want) {
			t.Errorf("acquiring %v changed the light of %v, the whole box changes %v", pos, got, want)
		}
	}
	compareLight(t, 3, split, whole, positions)
}

func cmpChunkPos(a, b ChunkPos) int {
	if a.X != b.X {
		return int(a.X - b.X)
	}
	return int(a.Z - b.Z)
}

// compareLight holds every cell's sky and block light, and each chunk's
// encoded light, equal between the two worlds.
func compareLight(t *testing.T, seed uint64, a, b *World, positions []ChunkPos) {
	t.Helper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, pos := range positions {
		ca, cb := a.chunks[pos].chunk, b.chunks[pos].chunk
		bad := 0
		for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
			for lx := range int32(16) {
				for lz := range int32(16) {
					sa, sb := ca.lightGet(&ca.skyLight, lx, y, lz), cb.lightGet(&cb.skyLight, lx, y, lz)
					ba, bb := ca.lightGet(&ca.blockLight, lx, y, lz), cb.lightGet(&cb.blockLight, lx, y, lz)
					if sa != sb || ba != bb {
						if bad++; bad <= 3 {
							t.Errorf("seed %d chunk %v (%d,%d,%d): split light sky %d block %d, whole box sky %d block %d",
								seed, pos, lx, y, lz, sa, ba, sb, bb)
						}
					}
				}
			}
		}
		if bad == 0 && !bytes.Equal(ca.lightForm(), cb.lightForm()) {
			t.Errorf("seed %d chunk %v: the encoded light differs with every cell equal", seed, pos)
		}
	}
}
