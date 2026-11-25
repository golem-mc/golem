package world

import (
	"reflect"
	"testing"
)

// linearChunkTicks is chunkTicks without the index: every heap entry in slice
// order, then the chunk's parked ones.
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
	return out
}

func TestChunkTicksIndexMatchesLinearScan(t *testing.T) {
	w, p := newFluidBookingWorld(t)
	chunks := []ChunkPos{{}, {X: 1}, {X: 2, Z: 1}, {X: 5, Z: 5}}
	for _, c := range chunks[:3] {
		w.Acquire(c)
	}
	check := func(step string) {
		t.Helper()
		// The first snapshot scans, the later ones use the index.
		for round := range 3 {
			for _, c := range chunks {
				got, want := p.chunkTicks(c), linearChunkTicks(p, c)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s, round %d, chunk %v: got %+v, want %+v", step, round, c, got, want)
				}
			}
		}
	}
	p.mu.Lock()
	for i := range int32(40) {
		x, z := i%24, i/12*16
		p.insertTick(blockKey{x, 64, z}, fluidTypeOf(fluidHolding(waterByLevel[0])), 10+int64(i%7), tickPriorityNormal)
		p.schedulePriority(x, 63, z, 5+int64(i%5), int(i%3)-1)
	}
	p.mu.Unlock()
	check("booked")
	p.mu.Lock()
	p.now += 2
	p.mu.Unlock()
	check("now advanced")
	setBlock(t, w, p, 3, 63, 0, waterByLevel[0])
	check("block changed")
	p.Tick(p.now + 1)
	check("popped")
	p.mu.Lock()
	p.seq++
	p.cancelled = map[int64]struct{}{p.queue[0].seq: {}}
	p.mu.Unlock()
	check("cancelled")
	p.SetTickFilter(func(c ChunkPos) bool { return c.X != 1 })
	p.Tick(p.now + 1)
	p.Tick(p.now + 10)
	check("parked")
	p.SetTickFilter(nil)
	p.Tick(p.now + 1)
	check("reactivated")
	p.ClearTicksIn(0, 60, 0, 20, 70, 8)
	check("cleared")
	p.mu.Lock()
	p.adoptTicks(nil, []ChunkPos{{X: 2, Z: 1}})
	p.mu.Unlock()
	check("unloaded")
}
