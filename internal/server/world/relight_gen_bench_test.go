package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkRelightGenerated is one relight after a light-changing write in
// generated overworld terrain: lava and stone in turn at (8, y, 8), deep in
// the caves' band and at the surface. The write is off the chunk grid's
// corner, so the wipe box covers partial chunks, the common case.
func BenchmarkRelightGenerated(b *testing.B) {
	for _, tc := range []struct {
		name string
		y    int32
	}{{"deep", -40}, {"surface", 0}} {
		b.Run(tc.name, func(b *testing.B) {
			dir := b.TempDir()
			w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
			b.Cleanup(func() { _ = w.Close() })
			for x := int32(-3); x <= 3; x++ {
				for z := int32(-3); z <= 3; z++ {
					w.Acquire(ChunkPos{X: x, Z: z})
				}
			}
			y := tc.y
			if tc.name == "surface" {
				y = w.SurfaceHeight(8, 8) + 1
			}
			lava := registry.MustState("minecraft:lava", nil)
			stone := registry.MustState("minecraft:stone", nil)
			state := lava
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.SetBlock(8, y, 8, state)
				w.Relight(8, 8)
				state ^= lava ^ stone
			}
		})
	}
}

// BenchmarkRelightLavaFlow is one step of a lava flow over generated
// terrain through the relight lane: lava spreads cell by cell along a
// 16-block run on the surface and drains back, each write relit on its own
// as a fluid tick queues it.
func BenchmarkRelightLavaFlow(b *testing.B) {
	dir := b.TempDir()
	w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
	b.Cleanup(func() { _ = w.Close() })
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
	const run = 16
	var ys [run]int32
	for i := range ys {
		ys[i] = w.SurfaceHeight(int32(i), 8) + 1
	}
	lava := registry.MustState("minecraft:lava", nil)
	air := registry.MustState("minecraft:air", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		step := i % (2 * run)
		x, state := int32(step), lava
		if step >= run {
			x, state = int32(2*run-1-step), air
		}
		w.SetBlock(x, ys[x], 8, state)
		<-w.RelightAsync(x, 8)
	}
}
