package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkSnifferEggTick visits an egg on its scheduled tick: the crack and
// the booking of the next stage.
func BenchmarkSnifferEggTick(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	egg := registry.MustState("minecraft:sniffer_egg", map[string]string{"hatch": "0"})
	w.SetBlock(8, GroundLevel, 8, egg)
	var changes []BlockChange
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		changes, p.effects = changes[:0], p.effects[:0]
		p.snifferEggTick(8, GroundLevel, 8, egg, &changes)
	}
}
