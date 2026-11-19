package world

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

func BenchmarkDoorSoundLookup(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	state := registry.BlockCherryTrapdoor.DefaultState()
	var changes []BlockChange
	var effects []Effect
	b.ReportAllocs()
	for b.Loop() {
		changes = changes[:0]
		effects = effects[:0]
		p.toggleDoor(5, GroundLevel, 5, registry.BlockCherryTrapdoor, "", state, &changes, &effects)
		if len(effects) != 1 {
			b.Fatal(effects)
		}
	}
}
