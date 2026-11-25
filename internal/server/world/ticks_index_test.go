package world

import (
	"cmp"
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// linearChunkTicks is chunkTicks without the index: every heap entry in slice
// order, then the chunk's parked ones, in booking order.
func linearChunkTicks(p *Physics, pos ChunkPos) chunkTicks {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out chunkTicks
	for _, t := range p.queue {
		p.collectTick(&out, pos, t)
	}
	for _, t := range p.parked[pos] {
		p.collectTick(&out, pos, t)
	}
	bySeq := func(a, b savedTick) int { return cmp.Compare(a.seq, b.seq) }
	slices.SortFunc(out.block, bySeq)
	slices.SortFunc(out.fluid, bySeq)
	return out
}

func TestChunkTicksIndexMatchesLinearScan(t *testing.T) {
	w, p := newFluidBookingWorld(t)
	a, b, c := ChunkPos{}, ChunkPos{X: 1}, ChunkPos{X: 2, Z: 1}
	chunks := []ChunkPos{a, b, c, {X: 5, Z: 5}}
	w.Acquire(b)
	w.Acquire(c)
	stone := registry.BlockStone.DefaultState()
	for _, pos := range []blockKey{{3, 63, 0}, {4, 63, 1}, {18, 63, 0}, {19, 63, 0}, {34, 63, 17}} {
		w.SetBlock(pos.x, pos.y, pos.z, stone)
	}
	water := fluidTypeOf(fluidHolding(waterByLevel[0]))
	p.mu.Lock()
	for _, e := range []struct {
		pos   blockKey
		fluid bool
		due   int64
	}{
		{blockKey{3, 63, 0}, false, 5}, {blockKey{1, 64, 0}, true, 5}, {blockKey{4, 63, 1}, false, 20}, {blockKey{2, 64, 2}, true, 20},
		{blockKey{17, 64, 0}, true, 7}, {blockKey{18, 63, 0}, false, 20},
		{blockKey{33, 64, 16}, true, 20}, {blockKey{34, 63, 17}, false, 7},
	} {
		if e.fluid {
			p.insertTick(e.pos, water, e.due, tickPriorityNormal)
		} else {
			p.insertTick(e.pos, 0, e.due, tickPriorityNormal)
		}
	}
	p.schedulePistonFinalize(19, 63, 0, 20)
	p.mu.Unlock()

	// check compares every chunk with the linear scan over several snapshots
	// (the first scans, the later ones build and use the index) and the counts
	// of blocks and fluids saved in a, b, c with want.
	check := func(step string, want [3][2]int) {
		t.Helper()
		for round := range 3 {
			for i, cp := range chunks {
				got, lin := p.chunkTicks(cp), linearChunkTicks(p, cp)
				if !reflect.DeepEqual(got, lin) {
					t.Fatalf("%s, round %d, chunk %v: got %+v, linear %+v", step, round, cp, got, lin)
				}
				if i < 3 && [2]int{len(got.block), len(got.fluid)} != want[i] {
					t.Fatalf("%s, chunk %v: %d block, %d fluid entries, want %v", step, cp, len(got.block), len(got.fluid), want[i])
				}
			}
		}
		if p.chunkIdx.byChunk == nil {
			t.Fatalf("%s: index not built after the snapshots", step)
		}
	}
	// mutate runs f on a warmed index and checks it is dropped, or kept when
	// f leaves the heap alone.
	mutate := func(step string, dropped bool, f func()) {
		t.Helper()
		if p.chunkIdx.byChunk == nil {
			t.Fatalf("%s: index cold before the mutation", step)
		}
		f()
		if (p.chunkIdx.byChunk == nil) != dropped {
			t.Fatalf("%s: index dropped = %v, want %v", step, p.chunkIdx.byChunk == nil, dropped)
		}
	}
	locked := func(f func()) func() {
		return func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			f()
		}
	}
	check("booked", [3][2]int{{2, 2}, {2, 1}, {1, 1}})
	mutate("insertion", true, locked(func() { p.insertTick(blockKey{5, 64, 3}, water, 20, tickPriorityNormal) }))
	check("inserted", [3][2]int{{2, 3}, {2, 1}, {1, 1}})
	mutate("block replaced", false, func() { w.SetBlock(4, 63, 1, airState) })
	check("block replaced", [3][2]int{{2, 3}, {2, 1}, {1, 1}})
	found := false
	for _, e := range p.chunkTicks(a).block {
		if e.x == 4 {
			found = true
			if e.id != registry.BlockStone.Name() {
				t.Fatalf("saved id %q of the replaced cell, want the booked type", e.id)
			}
		}
	}
	if !found {
		t.Fatal("the replaced cell's tick is not saved")
	}
	mutate("popped", true, func() { p.Tick(5) })
	check("popped", [3][2]int{{1, 2}, {2, 1}, {1, 1}})
	p.SetTickFilter(func(cp ChunkPos) bool { return cp != b })
	mutate("parked", true, func() { p.Tick(7) })
	check("parked", [3][2]int{{1, 2}, {2, 1}, {0, 1}})
	if len(p.parked[b]) == 0 {
		t.Fatal("no entry parked for the filtered chunk")
	}
	mutate("cancelled", false, locked(func() { p.cancelPistonFinalize(19, 63, 0) }))
	check("cancelled", [3][2]int{{1, 2}, {1, 1}, {0, 1}})
	p.SetTickFilter(nil)
	mutate("reactivated", true, func() { p.Tick(8) })
	check("reactivated", [3][2]int{{1, 2}, {1, 0}, {0, 1}})
	mutate("cleared", true, func() { p.ClearTicksIn(0, 60, 0, 15, 70, 15) })
	check("cleared", [3][2]int{{0, 0}, {1, 0}, {0, 1}})
	mutate("unloaded", true, locked(func() { p.adoptTicks(nil, []ChunkPos{c}) }))
	check("unloaded", [3][2]int{{0, 0}, {1, 0}, {0, 0}})
}
