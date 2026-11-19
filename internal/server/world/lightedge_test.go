package world

import (
	"math"
	"testing"
)

// TestLightAtTheInt32Edge lights the chunks at both corners of the int32
// block range: a box ending on math.MaxInt32 or starting on math.MinInt32 is
// walked to its edge and stops there, rather than wrapping round to the far
// side of the range.
func TestLightAtTheInt32Edge(t *testing.T) {
	t.Parallel()
	for _, corner := range []struct {
		name string
		x, z int32 // a block of the corner chunk, on the edge of the range
	}{
		{"max", math.MaxInt32, math.MaxInt32},
		{"min", math.MinInt32, math.MinInt32},
	} {
		t.Run(corner.name, func(t *testing.T) {
			t.Parallel()
			w := newTestWorld(t, NewFlatGenerator())
			pos := ChunkPos{X: corner.x >> 4, Z: corner.z >> 4}
			_, _ = w.Acquire(pos)
			defer w.Release(pos)

			if got := w.SkyLightAt(corner.x, 100, corner.z); got != 15 {
				t.Fatalf("sky light at the edge column = %d, want 15", got)
			}
			w.SetBlock(corner.x, 64, corner.z, torchState)
			w.Relight(corner.x, corner.z)
			if got := w.BlockLightAt(corner.x, 64, corner.z); got != 14 {
				t.Errorf("torch block light at the edge = %d, want 14", got)
			}

			// The whole-box recompute, over a box reaching the edge.
			w.mu.Lock()
			w.relightBox(pos.X*16, pos.Z*16, pos.X*16+15, pos.Z*16+15)
			w.mu.Unlock()
			if got := w.BlockLightAt(corner.x, 64, corner.z); got != 14 {
				t.Errorf("torch block light after the box recompute = %d, want 14", got)
			}
		})
	}
}
