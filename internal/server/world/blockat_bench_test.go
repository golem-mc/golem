package world

import (
	"math/rand"
	"testing"
)

func BenchmarkChunkLoaded(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	first, second := ChunkPos{}, ChunkPos{X: nearSlots}
	w.Acquire(first)
	w.Acquire(second)
	w.BlockAt(0, 64, 0) // Keep first in the slot shared by both loaded chunks.

	b.Run("Loaded", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if !w.ChunkLoaded(first) {
				b.Fatal("loaded chunk reported missing")
			}
		}
	})
	b.Run("Missing", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if w.ChunkLoaded(ChunkPos{X: 1}) {
				b.Fatal("missing chunk reported loaded")
			}
		}
	})
	b.Run("Parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if !w.ChunkLoaded(first) {
					b.Error("loaded chunk reported missing")
					return
				}
			}
		})
	})
	b.Run("Colliding", func(b *testing.B) {
		positions := [2]ChunkPos{first, second}
		b.ReportAllocs()
		for i := range b.N {
			if !w.ChunkLoaded(positions[i&1]) {
				b.Fatal("loaded chunk reported missing")
			}
		}
	})
}

// BenchmarkBlockAtManyChunks is a block read at a random cell of a world
// holding as many chunks as sixteen players at view distance ten keep
// loaded: 84x84 flat chunks, 7,056 of them.
func BenchmarkBlockAtManyChunks(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	for x := int32(-42); x < 42; x++ {
		for z := int32(-42); z < 42; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
	rng := rand.New(rand.NewSource(1))
	pts := make([][3]int32, 4096)
	for i := range pts {
		// Bodies cluster: runs of reads in one chunk, as a mob's tick makes.
		cx, cz := int32(rng.Intn(84)-42), int32(rng.Intn(84)-42)
		pts[i] = [3]int32{cx*16 + int32(rng.Intn(16)), GroundLevel - 1 + int32(rng.Intn(3)), cz*16 + int32(rng.Intn(16))}
	}
	b.ResetTimer()
	for i := range b.N {
		p := pts[i&4095]
		w.BlockAt(p[0], p[1], p[2])
	}
}

// BenchmarkBlockAtNeighbours is the read pattern of a redstone step: a cell
// and its six neighbours, walking one loaded chunk.
func BenchmarkBlockAtNeighbours(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	b.ReportAllocs()
	var sink uint16
	for i := range b.N {
		x, z := int32(1+i%14), int32(1+(i/14)%14)
		y := int32(GroundLevel)
		sink += w.BlockAt(x, y, z) + w.BlockAt(x+1, y, z) + w.BlockAt(x-1, y, z) +
			w.BlockAt(x, y+1, z) + w.BlockAt(x, y-1, z) + w.BlockAt(x, y, z+1) + w.BlockAt(x, y, z-1)
	}
	_ = sink
}

// distantPlayersWorld loads the 21x21 chunks around each of two players
// nearSlots chunks apart, so every chunk of one shares its near slot with a
// chunk of the other, and returns read points in bursts within one chunk, as a
// mob's tick makes, the bursts alternating between the players.
func distantPlayersWorld(b *testing.B) (*World, [][3]int32) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	const radius = 10
	for _, cx := range []int32{0, nearSlots} {
		for x := cx - radius; x <= cx+radius; x++ {
			for z := int32(-radius); z <= radius; z++ {
				w.Acquire(ChunkPos{X: x, Z: z})
			}
		}
	}
	rng := rand.New(rand.NewSource(1))
	pts := make([][3]int32, 4096)
	var cx, cz int32
	for i := range pts {
		if i%16 == 0 {
			cx, cz = int32(rng.Intn(2*radius+1)-radius), int32(rng.Intn(2*radius+1)-radius)
			if i/16%2 == 1 {
				cx += nearSlots
			}
		}
		pts[i] = [3]int32{cx*16 + int32(rng.Intn(16)), GroundLevel - 1 + int32(rng.Intn(3)), cz*16 + int32(rng.Intn(16))}
	}
	return w, pts
}

// BenchmarkBlockAtTwoDistantPlayers is a block read around two players
// nearSlots chunks apart, on one goroutine.
func BenchmarkBlockAtTwoDistantPlayers(b *testing.B) {
	w, pts := distantPlayersWorld(b)
	b.ResetTimer()
	for i := range b.N {
		p := pts[i&4095]
		w.BlockAt(p[0], p[1], p[2])
	}
}

// BenchmarkBlockAtTwoDistantPlayersParallel is the same read on every core
// at once, where the locked path's shared reader count is contended.
func BenchmarkBlockAtTwoDistantPlayersParallel(b *testing.B) {
	w, pts := distantPlayersWorld(b)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			p := pts[i&4095]
			w.BlockAt(p[0], p[1], p[2])
			i++
		}
	})
}
