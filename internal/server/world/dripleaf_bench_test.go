package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkDripleafProjectileAndReset tips a leaf with a projectile and runs
// its scheduled reset, including the physics effects returned to the server.
func BenchmarkDripleafProjectileAndReset(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	w.SetBlock(8, GroundLevel, 8, registry.BlockBigDripleaf.DefaultState())
	p := NewPhysics(w)
	var clock int64
	b.ReportAllocs()
	for b.Loop() {
		p.DripleafProjectiled(8, GroundLevel, 8)
		clock += 100
		p.Tick(clock)
	}
}
