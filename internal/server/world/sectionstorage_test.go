package world

import (
	"fmt"
	"testing"
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
