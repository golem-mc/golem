package world

import (
	"cmp"
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
