package world

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestSectionPackedWidths(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, 16, 17, 32, 33, 64, 65, 128, 129, 256, 257, 1100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var s section
			for i := range blocksPerSection {
				s.set(i, uint16(i%n), false)
			}
			width := minIndirectBits
			for 1<<width < n {
				width++
			}
			if width > maxIndirectBits {
				width = directBits
			}
			b := s.blocks.Load()
			if n <= 16 && b.bytes() >= 3000 {
				t.Fatalf("four-bit section uses %d bytes", b.bytes())
			}
			if b.bits != width {
				t.Fatalf("width = %d, want %d", b.bits, width)
			}
			if len(b.words) != (blocksPerSection+64/width-1)/(64/width) {
				t.Fatal("wrong word count")
			}
			var unpacked [blocksPerSection]uint16
			b.unpack(&unpacked)
			for i := range blocksPerSection {
				want := uint16(i % n)
				if s.at(i) != want || s.load(i) != want || unpacked[i] != want {
					t.Fatalf("cell %d: get/load/unpack = %d/%d/%d, want %d", i, s.at(i), s.load(i), unpacked[i], want)
				}
			}
			clone := b.clone()
			for i := range blocksPerSection {
				s.set(i, uint16((i+1)%n), true)
			}
			for i := range blocksPerSection {
				if s.load(i) != uint16((i+1)%n) || clone.get(i) != uint16(i%n) {
					t.Fatalf("overwrite or clone changed cell %d", i)
				}
			}
			if s.blocks.Load() != b {
				t.Fatal("existing palette entries caused a resize")
			}
		})
	}
}

func TestSectionPackedResizeKeepsOldReaders(t *testing.T) {
	t.Parallel()
	var s section
	type snapshot struct {
		blocks *blockWords
		count  int
	}
	var old []snapshot
	for i := range 300 {
		before := s.blocks.Load()
		s.set(i, uint16(i+1), true)
		if before != nil && before != s.blocks.Load() {
			old = append(old, snapshot{before, i})
		}
	}
	if s.blocks.Load().bits != directBits {
		t.Fatal("expected global IDs")
	}
	for i := range blocksPerSection {
		s.set(i, 500, true)
	}
	for _, saved := range old {
		for i := range blocksPerSection {
			var want uint16
			if i < saved.count {
				want = uint16(i + 1)
			}
			if got := saved.blocks.load(i); got != want {
				t.Fatalf("old %d-bit layout cell %d = %d, want %d", saved.blocks.bits, i, got, want)
			}
		}
	}
}

// recountReference is the per-cell classification recalcCounts must equal.
func recountReference(states *[blocksPerSection]uint16) (s section, touches, shapes int32) {
	for i, state := range states {
		s.countState(state, 1, &touches, &shapes)
		if kind := sculkCellKindOf(registry.BlockOf(state)); kind != noSculkCell {
			cells := s.sculkList(kind)
			*cells = append(*cells, uint16(i))
		}
	}
	return s, touches, shapes
}

func TestRecalcCountsMatchesPerCell(t *testing.T) {
	t.Parallel()
	pool := []uint16{
		registry.BlockAir.DefaultState(), registry.BlockStone.DefaultState(),
		registry.BlockSculkSensor.DefaultState(), registry.BlockSculkCatalyst.DefaultState(),
		registry.BlockComparator.DefaultState(), registry.BlockNetherPortal.DefaultState(),
		registry.BlockLightningRod.DefaultState(), registry.BlockOakLeaves.DefaultState(),
		registry.BlockWater.DefaultState(), registry.BlockGlowstone.DefaultState(),
		registry.BlockTurtleEgg.DefaultState(), registry.BlockWheat.DefaultState(),
	}
	check := func(t *testing.T, s *section, states *[blocksPerSection]uint16) {
		t.Helper()
		s.recalcCounts()
		want, touches, shapes := recountReference(states)
		if s.ticking != want.ticking || s.emitting != want.emitting || s.colliding != want.colliding ||
			s.rods != want.rods || s.eggs != want.eggs || s.portals != want.portals ||
			s.pois != want.pois || s.comparators != want.comparators ||
			s.touches.Load() != touches || s.shapes.Load() != shapes {
			t.Fatalf("counts differ: got %+v want %+v", s, want)
		}
		for _, kind := range []sculkCellKind{vibrationListenerCell, sculkCatalystCell} {
			if !slices.Equal(s.sculkCells(kind), want.sculkCells(kind)) {
				t.Fatalf("sculk kind %d: got %v want %v", kind, s.sculkCells(kind), want.sculkCells(kind))
			}
		}
	}
	for _, n := range []int{1, 2, 5, 16, 17, 33, 65, 129, 257, 600} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var s section
			var states [blocksPerSection]uint16
			for i := range states {
				state := pool[i*7%n%len(pool)]
				if n > 12 {
					state = uint16(i * 13 % n)
					if i%5 == 0 {
						state = pool[i/5%len(pool)]
					}
				}
				states[i] = state
				s.set(i, state, false)
			}
			check(t, &s, &states)
		})
	}
	t.Run("uniform", func(t *testing.T) {
		for _, state := range pool {
			var s section
			var states [blocksPerSection]uint16
			for i := range states {
				states[i] = state
			}
			s.setAll(state)
			check(t, &s, &states)
		}
	})
	t.Run("unused slots and padded word", func(t *testing.T) {
		// 40 states then overwrite most: palette keeps unused slots; six bits
		// leave four padding bits in the last word.
		var s section
		var states [blocksPerSection]uint16
		for i := range states {
			s.set(i, uint16(i%40+100), false)
		}
		for i := range states {
			state := pool[i%3]
			if i%4 == 0 {
				state = pool[3]
			}
			states[i] = state
			s.set(i, state, false)
		}
		blocks := s.blocks.Load()
		if blocks.bits != 6 || blocks.used < 40 {
			t.Fatalf("bits=%d used=%d", blocks.bits, blocks.used)
		}
		blocks.words[len(blocks.words)-1] |= ^uint64(0) << ((blocksPerSection - (len(blocks.words)-1)*blocks.perWord) * blocks.bits)
		check(t, &s, &states)
	})
}
