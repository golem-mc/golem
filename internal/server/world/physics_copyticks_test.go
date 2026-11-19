package world

import (
	"testing"
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
