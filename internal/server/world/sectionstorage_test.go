package world

import (
	"fmt"
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
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

func TestSectionPackedConcurrentReads(t *testing.T) {
	t.Parallel()
	var s section
	var readers sync.WaitGroup
	started := make(chan struct{}, 4)
	done := make(chan struct{})
	for range 4 {
		readers.Go(func() {
			started <- struct{}{}
			for {
				for i := range blocksPerSection {
					got := s.load(i)
					if got != 0 && got != uint16(i%300+1) {
						t.Errorf("cell %d read %d", i, got)
						return
					}
				}
				select {
				case <-done:
					return
				default:
				}
			}
		})
	}
	for range 4 {
		<-started
	}
	for i := range blocksPerSection {
		s.set(i, uint16(i%300+1), true)
	}
	close(done)
	readers.Wait()
}

func TestSectionPackedDecodePaletteWidth(t *testing.T) {
	t.Parallel()
	palette := nbt.List{Elem: nbt.TypeCompound}
	for i := range 16 {
		palette.Items = append(palette.Items, blockPaletteEntry(uint16(i+1)))
	}
	indices := make([]uint16, blocksPerSection)
	for i := range indices {
		indices[i] = uint16(i % 16)
	}
	var s section
	if err := decodeSectionBlocks(&s, nbt.Compound{"palette": palette, "data": packLongs(indices, 4)}); err != nil {
		t.Fatal(err)
	}
	if b := s.blocks.Load(); b.bits != 4 || b.used != 16 {
		t.Fatalf("decoded palette uses %d bits and %d slots", b.bits, b.used)
	}
	for i, index := range indices {
		if s.at(i) != index+1 {
			t.Fatalf("cell %d decoded as %d", i, s.at(i))
		}
	}
}

func TestSectionPaletteLookupLifecycle(t *testing.T) {
	t.Parallel()
	for _, count := range []int{16, 17, 33, 65, 129, 256} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b := newBlockWords(0)
			// Multiples of 257 include high IDs and hash collisions. Replacing
			// one cell leaves the earlier palette entries unused but reusable.
			for i := 1; i < count; i++ {
				b = b.put(0, uint16(i*257), false)
			}
			clone := b.clone()
			for i := range count {
				b = b.put(i, uint16(i*257), true)
				clone = clone.put(i, uint16(i*257), false)
			}
			if b.used != count || clone.used != count {
				t.Fatal("reusing unused entries grew the palette")
			}
			b = b.put(count, 60000, true)
			clone = clone.put(count, 60001, false)
			if b.get(count) != 60000 || clone.get(count) != 60001 {
				t.Fatal("clone did not insert an independent palette entry")
			}
			b = b.put(count+1, 60001, true)
			clone = clone.put(count+1, 60000, false)
			for i := range count {
				want := uint16(i * 257)
				if b.get(i) != want || clone.get(i) != want {
					t.Fatalf("growth changed cell %d", i)
				}
			}
			if b.get(count+1) != 60001 || clone.get(count+1) != 60000 {
				t.Fatal("clone shares its writer lookup")
			}
		})
	}
}

func TestSpawnPaletteOldReaders(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	s, _ := w.HoldSpawnChunk(ChunkPos{})
	defer s.Close()
	sec := &s.origin.chunk.sections[(80-worldMinY)/16]
	// Retain old layouts through palette appends and replacements.
	w.SetBlock(0, 80, 0, 1)
	for i := 2; i <= 300; i++ {
		s.InitialSpawnBlocked(0, 80, 0)
		old := sec.blocks.Load()
		clone := old.clone()
		before := make([]uint16, blocksPerSection)
		for j := range before {
			before[j] = old.load(j)
		}
		w.mu.Lock()
		sec.set(i, uint16(i), true)
		w.mu.Unlock()
		for j := 0; j <= i; j++ {
			want := before[j]
			if sec.blocks.Load() == old && j == i {
				want = uint16(i)
			}
			if got := old.load(j); got != want {
				t.Fatalf("old %d-bit layout at %d after slot %d: %d want %d", old.bits, j, i, got, want)
			}
			x, y, z := int32(j&15), int32(80+j/256), int32((j/16)&15)
			if got, want := s.InitialSpawnBlocked(x, y, z), registry.IsRedstoneConductorState(w.BlockAt(x, y, z)); got != want {
				t.Fatalf("cell %d after slot %d", j, i)
			}
		}
		if clone.load(i) != before[i] {
			t.Fatal("clone shares appended words")
		}
	}
	// An empty indirect palette and direct storage initialize only queried words.
	for _, width := range []int{4, directBits} {
		w.mu.Lock()
		sec.blocks.Store(makeBlockWords(width))
		sec.spawn.Store(nil)
		w.mu.Unlock()
		if s.InitialSpawnBlocked(15, 95, 15) {
			t.Fatal("empty storage predicate")
		}
		cache := sec.spawn.Load()
		for word := range cache {
			if (cache[word].Load() != 0) != (word == 127) {
				t.Fatalf("width %d initialized word %d", width, word)
			}
		}
	}
}
