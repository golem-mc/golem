package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// checkTickMask compares every bit of a built mask with live storage.
func checkTickMask(t *testing.T, s *section) {
	t.Helper()
	m := s.tickMask.Load()
	if m == nil {
		return
	}
	for i := range blocksPerSection {
		if m.ticks(i) != randomlyTicking(s.at(i)) {
			t.Fatalf("cell %d: mask=%v state=%d", i, m.ticks(i), s.at(i))
		}
	}
}

// builtMask builds the mask under the section's read-lock contract.
func builtMask(t *testing.T, s *section) *randomTickMask {
	t.Helper()
	m := s.tickCells(s.blocks.Load())
	checkTickMask(t, s)
	return m
}

func TestRandomTickMaskLifecycle(t *testing.T) {
	leaf := registry.BlockOakLeaves.DefaultState()

	// Uniform, empty and full sections build nothing.
	for _, state := range []uint16{airState, leaf} {
		var s section
		s.setAll(state)
		if builtMask(t, &s) != nil {
			t.Fatal("uniform section built a mask")
		}
	}

	var s section
	s.setAll(airState)
	s.set(0, leaf, false)
	s.set(4095, leaf, false)
	s.set(2048, stoneState, false)
	if builtMask(t, &s) == nil {
		t.Fatal("mixed section built no mask")
	}
	// Same-predicate and toggling writes, then growth through every width.
	s.set(1, stoneState, false)
	checkTickMask(t, &s)
	s.set(5, leaf, false)
	s.set(0, airState, false)
	checkTickMask(t, &s)
	first := s.tickMask.Load()
	for i := range 300 {
		s.set(100+i, uint16(i+1), true)
		checkTickMask(t, &s)
	}
	if s.blocks.Load().bits != directBits {
		t.Fatal("fixture did not reach direct storage")
	}
	if s.tickMask.Load() != first {
		t.Fatal("palette growth dropped the mask")
	}
	s.set(7, leaf, true)
	checkTickMask(t, &s)

	// A rebuild at each width agrees with storage, padding and end cells included.
	for width := 4; width <= 8; width++ {
		var w section
		w.setAll(airState)
		n := 1 << width
		if width == 4 {
			n = 12
		}
		for i := range n {
			w.set(i*(blocksPerSection-1)/n, uint16(i), false)
		}
		w.set(blocksPerSection-1, leaf, false)
		w.recalcCounts()
		if w.tickMask.Load() != nil {
			t.Fatal("recalc kept the mask")
		}
		builtMask(t, &w)
	}

	// setAll and recount reset.
	s.setAll(leaf)
	if s.tickMask.Load() != nil {
		t.Fatal("setAll kept the mask")
	}

	// Shaped-terrain restore and NBT decode give independent chunks.
	c := newChunk(0, 0)
	c.SetBlock(1, 80, 1, leaf)
	si := (80 - worldMinY) / 16
	builtMask(t, &c.sections[si])
	saved := new(shapedTerrain)
	saved.keep(c, true)
	a, b := newChunk(0, 0), newChunk(0, 0)
	a.SetBlock(2, 80, 2, leaf)
	builtMask(t, &a.sections[si])
	saved.restore(a)
	saved.restore(b)
	if a.sections[si].tickMask.Load() != nil || b.sections[si].tickMask.Load() != nil {
		t.Fatal("restore kept a mask")
	}
	builtMask(t, &a.sections[si])
	builtMask(t, &b.sections[si])
	a.SetBlock(3, 80, 3, leaf)
	checkTickMask(t, &a.sections[si])
	checkTickMask(t, &b.sections[si])
	if b.sections[si].tickMask.Load().ticks(3<<8 | 3<<4 | 3&15) {
		t.Fatal("write escaped into a sibling mask")
	}
	root, err := encodeChunkNBT(b)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range loaded.sections {
		if loaded.sections[i].tickMask.Load() != nil {
			t.Fatal("decode built a mask")
		}
		builtMask(t, &loaded.sections[i])
	}
}

func TestRandomTickMaskConcurrentReads(t *testing.T) {
	leaf := registry.BlockOakLeaves.DefaultState()
	var mu sync.RWMutex
	var s section
	s.setAll(airState)
	for i := 0; i < blocksPerSection; i += 7 {
		s.set(i, leaf, false)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				mu.RLock()
				s.tickCells(s.blocks.Load())
				checkTickMask(t, &s)
				mu.RUnlock()
			}
		})
	}
	wg.Go(func() {
		for i := range 200 {
			mu.Lock()
			state := uint16(airState)
			if i%2 == 0 {
				state = leaf
			}
			s.set(i*3%blocksPerSection, state, false)
			mu.Unlock()
		}
	})
	wg.Wait()
	mu.RLock()
	checkTickMask(t, &s)
	mu.RUnlock()
}
