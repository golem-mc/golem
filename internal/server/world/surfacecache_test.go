package world

import (
	"math/rand/v2"
	"testing"
)

// TestTheSurfaceCacheFollowsWrites holds SurfaceHeight, its cache kept by
// the writes, equal to a fresh scan of the column after every write: blocks
// above the surface, air over it, air under it, single and batched.
func TestTheSurfaceCacheFollowsWrites(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, testTerrain(21, false))
	w.Acquire(ChunkPos{0, 0})
	defer w.Release(ChunkPos{0, 0})
	fresh := func(x, z int32) int32 {
		w.mu.Lock()
		w.chunks[ChunkPos{0, 0}].surface.Store(nil)
		w.mu.Unlock()
		return w.SurfaceHeight(x, z)
	}
	rng := rand.New(rand.NewPCG(21, 21))
	for i := range 2000 {
		// Two columns only, so the writes pile onto each other's surface.
		x, z := rng.Int32N(2), int32(5)
		w.SurfaceHeight(x, z) // the cache is built before the write
		top := w.SurfaceHeight(x, z)
		y := top - 3 + rng.Int32N(7)
		state := uint16(stoneState)
		if rng.IntN(2) == 0 {
			state = airState
		}
		if i%3 == 0 {
			w.SetBlocks([]BlockChange{{X: x, Y: y, Z: z, State: state}, {X: x, Y: y + 1, Z: z, State: airState}})
		} else {
			w.SetBlock(x, y, z, state)
		}
		cached := w.SurfaceHeight(x, z)
		if want := fresh(x, z); cached != want {
			t.Fatalf("write %d of %d at (%d,%d,%d): the cached surface is %d, a fresh scan finds %d", i, state, x, y, z, cached, want)
		}
	}
}
