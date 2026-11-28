package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

func sendListClient(s *Server) *client {
	c := &client{srv: s, conn: protocol.NewConn(&countingConn{}), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.out = &mailbox{c: c, done: make(chan struct{})}
	s.assignPlayerSlot(c)
	return c
}

func TestSendListGroupsByRecipientInAddOrder(t *testing.T) {
	s := &Server{}
	s.deferWakes.Store(true)
	a, b := sendListClient(s), sendListClient(s)
	gone, here := new(world.World), new(world.World)
	a.dim.Store(&dimension{w: here})
	b.dim.Store(&dimension{w: here})

	var l sendList
	l.add(a, outbound{id: 1})
	l.add(b, outbound{id: 11})
	l.addIn(a, gone, outbound{id: 2})
	l.add(b, outbound{id: 12})
	l.addIn(b, here, outbound{id: 13})
	l.add(a, outbound{id: 3})
	l.send()

	ids := func(c *client) (got []int32) {
		for _, p := range c.out.queue {
			got = append(got, p.id)
		}
		return got
	}
	if got := ids(a); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("a got %v, want [1 3]", got)
	}
	if got := ids(b); len(got) != 3 || got[0] != 11 || got[1] != 12 || got[2] != 13 {
		t.Fatalf("b got %v, want [11 12 13]", got)
	}
}

func TestSendListAddAllPreservesOrderAndWorldFilter(t *testing.T) {
	s := &Server{}
	s.deferWakes.Store(true)
	a, b := sendListClient(s), sendListClient(s)
	gone, here := new(world.World), new(world.World)
	a.dim.Store(&dimension{w: here})
	b.dim.Store(&dimension{w: here})

	var l sendList
	l.addAll(a, nil, nil)
	l.addAll(a, nil, []outbound{{id: 1}, {id: 2}})
	l.addAll(b, here, []outbound{{id: 11}, {id: 12}})
	l.addAll(a, gone, []outbound{{id: 3}, {id: 4}})
	l.addAll(a, here, []outbound{{id: 5}})
	l.send()

	ids := func(c *client) (got []int32) {
		for _, p := range c.out.queue {
			got = append(got, p.id)
		}
		return got
	}
	if got := ids(a); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 5 {
		t.Fatalf("a got %v, want [1 2 5]", got)
	}
	if got := ids(b); len(got) != 2 || got[0] != 11 || got[1] != 12 {
		t.Fatalf("b got %v, want [11 12]", got)
	}
}

func TestClientSetSwapRemoveAndSlotReuse(t *testing.T) {
	s := &Server{}
	a, b, c := sendListClient(s), sendListClient(s), sendListClient(s)

	var set clientSet
	if !set.add(a) || !set.add(b) || !set.add(c) {
		t.Fatal("initial adds failed")
	}
	if set.add(b) {
		t.Fatal("duplicate add reported true")
	}
	if !set.delete(a) || set.has(a) || !set.has(b) || !set.has(c) || len(set.list) != 2 {
		t.Fatalf("after deleting a: len=%d has(a)=%v has(b)=%v has(c)=%v",
			len(set.list), set.has(a), set.has(b), set.has(c))
	}

	// Reuse a's slot for a new connection; old a must not evict or re-add over it.
	s.freePlayerSlots = append(s.freePlayerSlots, a.slot)
	a2 := sendListClient(s)
	if a2.slot != a.slot {
		t.Fatalf("reused slot %d, want %d", a2.slot, a.slot)
	}
	if !set.add(a2) || !set.has(a2) || set.has(a) {
		t.Fatal("reused slot add failed")
	}
	if set.delete(a) || !set.has(a2) {
		t.Fatal("stale connection deleted reused slot")
	}
	if set.add(a) {
		t.Fatal("stale connection re-added over newer generation")
	}

	// Reused slot replacing a stale entry that was still in the set.
	s.freePlayerSlots = append(s.freePlayerSlots, b.slot)
	b2 := sendListClient(s)
	if !set.add(b2) || !set.has(b2) || set.has(b) || len(set.list) != 3 {
		t.Fatalf("in-place slot replacement failed: len=%d has(b2)=%v has(b)=%v",
			len(set.list), set.has(b2), set.has(b))
	}

	set.clear()
	if len(set.list) != 0 || set.has(a2) || set.has(b2) || set.has(c) {
		t.Fatal("clear left entries behind")
	}
	if set.add(a) {
		t.Fatal("stale connection re-added after clear")
	}
}

func BenchmarkSendListMoves(b *testing.B) {
	s := &Server{}
	s.deferWakes.Store(true)
	clients := make([]*client, 100)
	for i := range clients {
		clients[i] = sendListClient(s)
	}
	data := make([]byte, 20)
	b.ReportAllocs()
	for b.Loop() {
		var l sendList
		for _, from := range clients {
			for _, to := range clients {
				if from != to {
					l.add(to, outbound{id: protocol.IDPlayEntityPosition, data: data})
				}
			}
		}
		l.send()
		for _, c := range clients {
			clear(c.out.queue)
			c.out.queue, c.out.queued = c.out.queue[:0], 0
		}
	}
}

func BenchmarkSendListAddAll(b *testing.B) {
	s := &Server{}
	s.deferWakes.Store(true)
	clients := make([]*client, 100)
	for i := range clients {
		clients[i] = sendListClient(s)
	}
	packets := []outbound{
		{id: protocol.IDPlayEntityPositionRot, data: make([]byte, 20)},
		{id: protocol.IDPlayHeadRotation, data: make([]byte, 6)},
	}
	b.ReportAllocs()
	for b.Loop() {
		var l sendList
		for _, from := range clients {
			for _, to := range clients {
				if from != to {
					l.addAll(to, nil, packets)
				}
			}
		}
		l.send()
		for _, c := range clients {
			clear(c.out.queue)
			c.out.queue, c.out.queued = c.out.queue[:0], 0
		}
	}
}
