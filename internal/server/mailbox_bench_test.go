package server

import (
	"github.com/google/uuid"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// countingConn counts Write calls and discards the bytes.
type countingConn struct {
	net.Conn
	writes int
}

func (c *countingConn) Write(b []byte) (int, error)      { c.writes++; return len(b), nil }
func (c *countingConn) SetWriteDeadline(time.Time) error { return nil }

func BenchmarkMailboxBatch(b *testing.B) {
	for _, n := range []int{100, 2048} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			cc := &countingConn{}
			cl := &client{conn: protocol.NewConn(cc), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			m := &mailbox{c: cl, done: make(chan struct{})}
			payload := make([]byte, 24)
			b.ReportAllocs()
			for b.Loop() {
				m.mu.Lock()
				for range n {
					m.queue = append(m.queue, outbound{id: 0x20, data: payload})
				}
				m.mu.Unlock()
				m.writeBatch()
			}
			b.ReportMetric(float64(cc.writes)/float64(b.N), "writes/batch")
		})
	}
}

func BenchmarkMailboxSend(b *testing.B) {
	for _, tc := range []struct {
		name string
		id   int32
	}{
		{"packet", protocol.IDPlayEntityPosition},
		{"system", protocol.IDPlaySystemChat},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cc := &countingConn{}
			c := &client{srv: &Server{}, conn: protocol.NewConn(cc), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			c.out = &mailbox{c: c, wake: make(chan struct{}, 1), done: make(chan struct{})}
			var payload protocol.Buffer
			payload.PutText(protocol.Text{Text: "message"})
			payload.PutBool(false)
			data := payload.Bytes()
			b.ReportAllocs()
			for b.Loop() {
				for range 100 {
					c.send(tc.id, data)
				}
				c.out.writeBatch()
			}
		})
	}
}

// BenchmarkBroadcast50KBTo50 includes enqueueing and draining every recipient.
func BenchmarkBroadcast50KBTo50(b *testing.B) {
	s := &Server{clients: make(map[uuid.UUID]*client)}
	s.deferWakes.Store(true)
	clients := make([]*client, 50)
	for i := range clients {
		c := &client{srv: s, conn: protocol.NewConn(&countingConn{})}
		c.conn.EnableCompression(256)
		c.out = &mailbox{c: c, done: make(chan struct{})}
		s.assignPlayerSlot(c)
		s.clients[uuid.UUID{byte(i)}] = c
		clients[i] = c
	}
	payload := make([]byte, 50<<10)
	rng := rand.New(rand.NewPCG(6393, 0))
	for i := range payload {
		payload[i] = byte(rng.IntN(32))
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload) * len(clients)))
	for b.Loop() {
		s.broadcast(protocol.IDPlayMapData, payload)
		for _, c := range clients {
			if !c.out.writeBatch() {
				b.Fatal("write failed")
			}
		}
	}
}
