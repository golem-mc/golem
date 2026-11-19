package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkRandomTickGenerated is one random tick pass over 9x9 generated
// overworld chunks at the default speed of three dice per section.
func BenchmarkRandomTickGenerated(b *testing.B) {
	dir := b.TempDir()
	w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
	b.Cleanup(func() { _ = w.Close() })
	var chunks []ChunkPos
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
			chunks = append(chunks, ChunkPos{X: x, Z: z})
		}
	}
	p := NewPhysics(w)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		p.RandomTick(int64(i), chunks, 3, 1)
	}
}

// BenchmarkRandomTickGroundCover is one random tick pass over 3x3 flat chunks
// whose top layer alternates grass, crimson nylium and warped nylium, a bottom
// slab over every other column, so the grass seal test and the nylium revert
// both run, with dirt beside the grass for the spread.
func BenchmarkRandomTickGroundCover(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	var chunks []ChunkPos
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
			chunks = append(chunks, ChunkPos{X: cx, Z: cz})
		}
	}
	ground := []uint16{
		registry.BlockGrassBlock.DefaultState(),
		registry.BlockCrimsonNylium.DefaultState(),
		registry.BlockWarpedNylium.DefaultState(),
		dirtState,
	}
	slab := registry.BlockStoneSlab.DefaultState()
	for x := int32(-16); x < 32; x++ {
		for z := int32(-16); z < 32; z++ {
			w.SetBlock(x, GroundLevel, z, ground[(x+z)&3])
			if (x+z)&1 == 0 {
				w.SetBlock(x, GroundLevel+1, z, slab)
			}
		}
	}
	p := NewPhysics(w)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		p.RandomTick(int64(i), chunks, 3, 1)
	}
}
