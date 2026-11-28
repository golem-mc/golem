package world

import (
	"cmp"
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

func TestScheduledTickQueueOrder(t *testing.T) {
	for _, initialize := range []bool{false, true} {
		t.Run(map[bool]string{false: "push", true: "init"}[initialize], func(t *testing.T) {
			rng := rand.New(rand.NewSource(3422))
			var q tickQueue
			var want []scheduledTick
			for seq := range int64(2000) {
				tick := scheduledTick{due: rng.Int63n(20), priority: rng.Intn(7) - 3, seq: seq}
				want = append(want, tick)
				if initialize {
					q = append(q, tick)
				} else {
					q.push(tick)
				}
			}
			if initialize {
				q.init()
				q.init() // Heap initialization is idempotent.
			}
			for range 200 {
				i := rng.Intn(len(q))
				removed := q[i]
				if got := q.remove(i); got != removed {
					t.Fatalf("remove(%d) = %+v, want %+v", i, got, removed)
				}
				want = slices.DeleteFunc(want, func(tick scheduledTick) bool { return tick.seq == removed.seq })
			}
			if len(q) != len(want) {
				t.Fatalf("queue length = %d, want %d", len(q), len(want))
			}
			slices.SortFunc(want, func(a, b scheduledTick) int {
				if order := cmp.Compare(a.due, b.due); order != 0 {
					return order
				}
				if order := cmp.Compare(a.priority, b.priority); order != 0 {
					return order
				}
				return cmp.Compare(a.seq, b.seq)
			})
			for i, tick := range want {
				if got := q.pop(); got != tick {
					t.Fatalf("pop %d = %+v, want %+v", i, got, tick)
				}
			}
			q.init()
			last := scheduledTick{seq: 2000}
			q.push(last)
			if got := q.remove(0); got != last || len(q) != 0 {
				t.Fatalf("remove singleton = %+v, len = %d", got, len(q))
			}
		})
	}
}

func TestHasTickDueAtMatchesLinearScan(t *testing.T) {
	const due = 100
	pos := blockKey{1, 64, 1}
	overdue := scheduledTick{pos: pos, due: due - 1}
	future := scheduledTick{pos: pos, due: due + 1}
	water := fluidTypeOf(fluidHolding(waterByLevel[0]))
	fluid := scheduledTick{pos: pos, due: due, fluid: water, seq: 2}
	event := scheduledTick{pos: pos, due: due, event: redstoneTickEvent}
	cancelled := scheduledTick{pos: pos, due: due, priority: tickPriorityFinalize, seq: 3}
	rebooked := scheduledTick{pos: pos, due: due, priority: tickPriorityFinalize, seq: 4}
	other := scheduledTick{pos: blockKey{1, 65, 1}, due: due, seq: 1}

	for _, tc := range []struct {
		name  string
		queue tickQueue
	}{
		{"parked only", nil},
		{"overdue only", tickQueue{overdue}},
		{"future only", tickQueue{future}},
		{"same due at a different position", tickQueue{other}},
		{"cancelled only", tickQueue{cancelled}},
		{"event due now", tickQueue{event}},
		{"fluid under an overdue parent", tickQueue{overdue, other, fluid, future}},
		{"rebooked under a cancelled parent", tickQueue{cancelled, rebooked}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Physics{cancelled: map[int64]struct{}{cancelled.seq: {}}}
			p.setTicks(tc.queue)
			p.park(event)
			want := slices.ContainsFunc(p.queue, func(tick scheduledTick) bool {
				return tick.pos == pos && tick.due == due && !p.tickCancelled(tick)
			})
			if got := p.hasTickDueAt(pos, due); got != want {
				t.Errorf("hasTickDueAt(%v, %d) = %t, linear scan = %t", pos, due, got, want)
			}
		})
	}
}

// BenchmarkHasTickDueAt checks that the lookup does not grow with the queue.
func BenchmarkHasTickDueAt(b *testing.B) {
	for _, n := range []int{100, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			p := &Physics{cancelled: map[int64]struct{}{}}
			ticks := make(tickQueue, n)
			for i := range ticks {
				ticks[i] = scheduledTick{pos: blockKey{int32(i), 64, 0}, due: 10, seq: int64(i)}
			}
			p.setTicks(ticks)
			pos := blockKey{int32(n / 2), 64, 0}
			b.ResetTimer()
			for range b.N {
				if !p.hasTickDueAt(pos, 10) || p.hasTickDueAt(pos, 11) {
					b.Fatal("wrong answer")
				}
			}
		})
	}
}
