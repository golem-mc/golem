package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func checkRandomTickCount(t *testing.T, s *section) {
	t.Helper()
	count := 0
	for i := range blocksPerSection {
		if randomlyTicking(s.at(i)) {
			count++
		}
	}
	if count != s.ticking {
		t.Fatalf("count=%d actual=%d", s.ticking, count)
	}
}

func TestRandomTickCountsThroughWrites(t *testing.T) {
	leaf := registry.BlockOakLeaves.DefaultState()
	for _, state := range []uint16{airState, leaf} {
		var s section
		s.setAll(state)
		checkRandomTickCount(t, &s)
		s.set(0, state, false)
		other := leaf
		if state == leaf {
			other = airState
		}
		s.set(4095, other, false)
		checkRandomTickCount(t, &s)
		s.set(4095, state, false)
		checkRandomTickCount(t, &s)
		s.setAll(state)
	}
	var s section
	s.setAll(airState)
	s.set(0, stoneState, false)
	s.set(4095, leaf, false)
	for i := range 300 {
		s.set(i, uint16(i), true)
		checkRandomTickCount(t, &s)
	}
	if s.blocks.Load().bits != directBits {
		t.Fatal("fixture did not reach direct storage")
	}
	s.recalcCounts()
	checkRandomTickCount(t, &s)
}

func TestRandomTickCountsThroughTerrainAndReload(t *testing.T) {
	c := newChunk(0, 0)
	leaf := registry.BlockOakLeaves.DefaultState()
	c.SetBlock(1, 80, 1, leaf)
	saved := new(shapedTerrain)
	saved.keep(c, true)
	a, b := newChunk(0, 0), newChunk(0, 0)
	a.SetBlock(2, 80, 2, leaf)
	b.SetBlock(3, 80, 3, leaf)
	saved.restore(a)
	saved.restore(b)
	si := (80 - worldMinY) / 16
	for _, chunk := range []*Chunk{c, a, b} {
		checkRandomTickCount(t, &chunk.sections[si])
	}
	a.SetBlock(1, 80, 1, airState)
	checkRandomTickCount(t, &a.sections[si])
	checkRandomTickCount(t, &b.sections[si])
	if b.BlockAt(1, 80, 1) != leaf {
		t.Fatal("mutation escaped restored chunk")
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
		checkRandomTickCount(t, &loaded.sections[i])
	}
}
