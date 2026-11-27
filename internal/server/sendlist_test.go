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
