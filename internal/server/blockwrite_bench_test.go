package server

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkBlockWriteWake compares the shared writer with and without a physics wake.
func BenchmarkBlockWriteWake(b *testing.B) {
	for _, tc := range []struct {
		name   string
		policy blockWritePolicy
	}{{"storage", 0}, {"wake", writeWake}} {
		b.Run(tc.name, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			states := [2]uint16{registry.BlockStone.DefaultState(), registry.BlockGlass.DefaultState()}
			w.SetBlock(5, 90, 6, registry.BlockOakFence.DefaultState())
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				if !s.writeBlock(w, 6, 90, 6, states[i&1], nil, tc.policy) {
					b.Fatal("block did not change")
				}
			}
		})
	}
}

// BenchmarkBlockWriteFrames writes a cell that supported item frames hang on,
// the case the per-write frame survival check used to cost.
func BenchmarkBlockWriteFrames(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	states := [2]uint16{registry.BlockStone.DefaultState(), registry.BlockAndesite.DefaultState()}
	w.SetBlock(6, 90, 6, states[1])
	hung := 0
	for dir := range int32(6) {
		for _, at := range [][3]int32{{6, 90, 7}, {6, 90, 5}, {7, 90, 6}, {5, 90, 6}, {6, 91, 6}, {6, 89, 6}} {
			if rules.ItemFrameSupportCell(at[0], at[1], at[2], dir) == [3]int32{6, 90, 6} {
				s.registerFrame(newFrame(s.nextEntity(), uuid.New(), w, at[0], at[1], at[2], dir, false))
				hung++
			}
		}
	}
	if hung == 0 {
		b.Fatal("no frame hangs on the written cell")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if !s.writeBlock(w, 6, 90, 6, states[i&1], nil, writeWake) {
			b.Fatal("block did not change")
		}
	}
	b.StopTimer()
	if len(s.frames) != hung {
		b.Fatalf("%d frames left of %d", len(s.frames), hung)
	}
}
