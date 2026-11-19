package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestPhysicsCopyTicksFromKeepsDueAndPriority(t *testing.T) {
	_, p := newOneChunkPhysics(t)
	p.mu.Lock()
	p.schedulePriority(9, 70, 8, p.now+4, tickPriorityNormal)
	p.schedulePriority(8, 70, 8, p.now+4, tickPriorityNormal)
	p.schedulePriority(8, 71, 9, p.now+2, tickPriorityVeryHigh)
	p.schedulePriority(12, 70, 8, p.now+3, tickPriorityNormal) // outside the box
	p.mu.Unlock()

	p.ScheduleBlockTicks(p.BlockTicksIn(8, 70, 8, 9, 71, 9), 0, 0, 4)

	p.mu.Lock()
	defer p.mu.Unlock()
	got := map[blockKey]scheduledTick{}
	for _, tk := range p.queue {
		got[tk.pos] = tk
	}
	for _, want := range []struct {
		pos      blockKey
		due      int64
		priority int
	}{
		{blockKey{9, 70, 12}, p.now + 4, tickPriorityNormal},
		{blockKey{8, 70, 12}, p.now + 4, tickPriorityNormal},
		{blockKey{8, 71, 13}, p.now + 2, tickPriorityVeryHigh},
	} {
		tk, ok := got[want.pos]
		if !ok || !p.pending[want.pos] {
			t.Errorf("no tick pending at %+v after the copy", want.pos)
			continue
		}
		if tk.due != want.due || tk.priority != want.priority {
			t.Errorf("tick at %+v = due %d priority %d, want due %d priority %d",
				want.pos, tk.due, tk.priority, want.due, want.priority)
		}
	}
	// Equal due and priority: the copies keep their sources' order.
	if got[blockKey{9, 70, 12}].seq >= got[blockKey{8, 70, 12}].seq {
		t.Errorf("copies reordered: seq %d then %d", got[blockKey{9, 70, 12}].seq, got[blockKey{8, 70, 12}].seq)
	}
	if _, ok := got[blockKey{12, 70, 12}]; ok {
		t.Error("the tick outside the box was copied")
	}
	if len(p.queue) != 7 {
		t.Errorf("queue holds %d ticks, want 7 (4 scheduled, 3 copied)", len(p.queue))
	}
}

// TestPhysicsCopyTicksTagsBlockTicksAndSkipsFluidTicks: a tagged block tick
// arrives booked as its block's own, a fluid tick stays behind, and a tick
// whose destination holds another block is dropped.
// vanilla: CloneCommands.clone copies only getBlockTicks(); ServerLevel.tickBlock
func TestPhysicsCopyTicksTagsBlockTicksAndSkipsFluidTicks(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	rod := registry.BlockLightningRod.DefaultState()
	water := registry.BlockWater.DefaultState()
	stone := registry.BlockStone.DefaultState()
	for _, c := range []struct {
		x, z  int32
		state uint16
	}{{8, 8, rod}, {8, 12, rod}, {9, 8, water}, {9, 12, water}, {10, 8, stone}} {
		if !w.SetBlock(c.x, 70, c.z, c.state) {
			t.Fatalf("setup: SetBlock(%d, 70, %d) did not take", c.x, c.z)
		}
	}
	p.mu.Lock()
	p.scheduleBooked(p.rodBooked, 8, 70, 8, p.now+8, lightningRodTickEvent)
	p.schedulePriority(9, 70, 8, p.now+5, tickPriorityNormal)
	p.schedulePriority(10, 70, 8, p.now+3, tickPriorityNormal) // lands on air
	p.mu.Unlock()

	p.ScheduleBlockTicks(p.BlockTicksIn(8, 70, 8, 10, 70, 8), 0, 0, 4)

	p.mu.Lock()
	defer p.mu.Unlock()
	var rodTick *scheduledTick
	for i, tk := range p.queue {
		switch tk.pos {
		case blockKey{8, 70, 12}:
			rodTick = &p.queue[i]
		case blockKey{9, 70, 12}:
			t.Error("the water's fluid tick was copied")
		case blockKey{10, 70, 12}:
			t.Error("the stone's tick was copied onto air")
		}
	}
	if rodTick == nil {
		t.Fatal("the lightning rod's tick was not copied")
	}
	if rodTick.event != lightningRodTickEvent || rodTick.due != p.now+8 {
		t.Errorf("rod tick = event %d due %d, want event %d due %d",
			rodTick.event, rodTick.due, lightningRodTickEvent, p.now+8)
	}
	if !p.rodBooked[blockKey{8, 70, 12}] {
		t.Error("the copied rod tick is not booked in rodBooked")
	}
}
