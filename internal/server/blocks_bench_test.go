package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTellSectionBlocks measures batch preparation and lookup across
// eight loaded sections, without network writes or chunk generation.
func BenchmarkTellSectionBlocks(b *testing.B) {
	w, err := world.NewWorld(b.TempDir(), world.NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := w.Close(); err != nil {
			b.Error(err)
		}
	})
	for x := range int32(2) {
		for z := range int32(2) {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			b.Cleanup(func() { w.Release(pos) })
		}
	}
	w.FlushLighting()
	s := new(Server)
	states := [2]uint16{registry.BlockStone.DefaultState(), registry.BlockRedstoneWire.DefaultState()}
	var changes [256]world.BlockChange
	for i := range changes {
		section, cell := int32(i%8), int32(i/8)
		changes[i] = world.BlockChange{
			X:     (section&1)*16 + cell%16,
			Y:     64 + (section>>2)*16,
			Z:     ((section>>1)&1)*16 + cell/16,
			State: states[cell%2],
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		s.tellSectionBlocks(w, changes[:])
	}
}
