package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

func BenchmarkTickTNTQueued(b *testing.B) {
	const chunks, count, limit, fuse = 5, 5000, 100, 1 << 30
	s, w := newBlockBenchServer(b, chunks)
	s.Config.MaxTNTPerTick = limit
	for x := range int32(chunks) {
		for z := range int32(chunks) {
			b.Cleanup(w.HoldEntityTicking(world.ChunkPos{X: x, Z: z}))
		}
	}
	for i := range count {
		// Interleave chunks so the first hundred TNT also span the whole area.
		chunk, cell := i%(chunks*chunks), i/(chunks*chunks)
		x, z := chunk%chunks*16+cell%16, chunk/chunks*16+cell/16
		s.spawnTNT(w, float64(x)+0.5, world.GroundLevel, float64(z)+0.5, fuse, 0, "", uuid.Nil, false)
	}
	b.ReportAllocs()
	for b.Loop() {
		s.tickTNT()
	}
	if len(s.tnts) != count || s.tnts[limit-1].tnt.Fuse != fuse-b.N || s.tnts[limit].tnt.Fuse != fuse {
		b.Fatal("expected only the first 100 TNT to tick throughout the run")
	}
}

// BenchmarkTickTNTInHerd ticks one primed TNT beside a herd with no shulker
// or happy ghast, so the collider snapshot has nothing to walk.
func BenchmarkTickTNTInHerd(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(strconv.Itoa(cows), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			dim := s.allDims()[0]
			for i := range cows {
				x, z := float64(i%50)*2-49.5, float64(i/50)*2-39.5
				s.spawnMobInDim(dim, entity.TypeCow, x, world.GroundLevel, z, int64(i+1), nil)
			}
			s.spawnTNT(w, 40, world.GroundLevel, 40, 1<<30, 0, "", uuid.Nil, false)
			for range 40 {
				s.tickTNT()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickTNT()
			}
		})
	}
}
