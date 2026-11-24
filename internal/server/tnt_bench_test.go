package server

import (
	"testing"

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
