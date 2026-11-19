package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

var vaultsBenchLen int

func BenchmarkVaults128(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	w.Acquire(pos)
	b.Cleanup(func() { w.Release(pos) })
	state := registry.BlockVault.DefaultState()
	for x := range int32(16) {
		for z := range int32(8) {
			if !w.SetBlock(x, GroundLevel, z, state) {
				b.Fatalf("vault %d %d did not place", x, z)
			}
			w.SetBlockEntityTag(x, GroundLevel, z, "minecraft:vault", nbt.Compound{})
		}
	}
	if got := len(w.Vaults()); got != 128 {
		b.Fatalf("%d vaults, want 128", got)
	}
	b.ReportAllocs()
	for b.Loop() {
		vaultsBenchLen = len(w.Vaults())
	}
}
