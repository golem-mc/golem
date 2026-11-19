package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkDaylightDetectorUpdate reads a settled detector during its noon tick.
func BenchmarkDaylightDetectorUpdate(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	w.SetBlock(6, GroundLevel, 6, registry.BlockDaylightDetector.DefaultState())
	w.FlushLighting()
	w.SetClockTime(OverworldClock, 6000)
	p := NewPhysics(w)
	p.DaylightDetectorUpdate(6, GroundLevel, 6)
	b.ReportAllocs()
	for b.Loop() {
		p.DaylightDetectorUpdate(6, GroundLevel, 6)
	}
}
