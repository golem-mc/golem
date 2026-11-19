package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkSapling2x2 covers the first and last matching corners and a lone sapling.
func BenchmarkSapling2x2(b *testing.B) {
	for _, scene := range []struct {
		name   string
		corner int32
		square bool
	}{
		{"first_corner", 0, true}, {"last_corner", -1, true}, {"single", 0, false},
	} {
		b.Run(scene.name, func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			sapling := registry.BlockStates["minecraft:spruce_sapling"]
			w.SetBlock(8, GroundLevel, 8, sapling)
			if scene.square {
				for dx := range int32(2) {
					for dz := range int32(2) {
						// Different stages must still count as the same block.
						state, _ := registry.WithInt(sapling, registry.PropStage, int(dx^dz))
						w.SetBlock(8+scene.corner+dx, GroundLevel, 8+scene.corner+dz, state)
					}
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				if p.sameSpeciesSapling2x2(8, GroundLevel, 8) != scene.square {
					b.Fatal("unexpected 2x2 membership")
				}
			}
		})
	}
}

// BenchmarkNoteBlockInstrumentUpdate exercises the vertical neighbor shape pass,
// including both unchanged instruments and writes of a changed instrument.
func BenchmarkNoteBlockInstrumentUpdate(b *testing.B) {
	for _, scene := range []struct {
		name, above, below, instrument string
		changed                        bool
	}{
		{"unchanged", "air", "gold_block", "bell", false},
		{"below", "air", "gold_block", "bell", true},
		{"head_precedence", "skeleton_skull", "gold_block", "skeleton", true},
		{"head_below", "air", "skeleton_skull", "harp", true},
	} {
		b.Run(scene.name, func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			w.SetBlock(8, GroundLevel-1, 8, registry.BlockStates["minecraft:"+scene.below])
			w.SetBlock(8, GroundLevel+1, 8, registry.BlockStates["minecraft:"+scene.above])
			instrument := scene.instrument
			if scene.changed {
				instrument = "bass"
			}
			state := registry.MustState("minecraft:note_block", map[string]string{"instrument": instrument})
			w.SetBlock(8, GroundLevel, 8, state)
			changes := make([]BlockChange, 0, 1)
			b.ReportAllocs()
			for b.Loop() {
				changes = changes[:0]
				// Reset inside the timing so every iteration exercises the write path.
				if scene.changed {
					w.SetBlock(8, GroundLevel, 8, state)
				}
				p.noteBlockShapeUpdate(8, GroundLevel, 8, [3]int32{0, 1, 0}, state, &changes)
			}
			if got := registry.StateValue(w.BlockAt(8, GroundLevel, 8), registry.PropInstrument); got != scene.instrument {
				b.Fatalf("instrument = %s, want %s", got, scene.instrument)
			}
			if (len(changes) != 0) != scene.changed {
				b.Fatalf("changes = %v", changes)
			}
		})
	}
}
