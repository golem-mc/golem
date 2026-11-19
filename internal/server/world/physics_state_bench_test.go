package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkRepeaterUse cycles a locked repeater through its four delays.
func BenchmarkRepeaterUse(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	w.SetBlock(8, GroundLevel, 8, registry.MustState("minecraft:repeater", map[string]string{"facing": "east", "locked": "true"}))
	p := NewPhysics(w)
	b.ReportAllocs()
	for b.Loop() {
		p.UseBlock(8, GroundLevel, 8, "")
	}
}
