package server

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/google/uuid"
)

type recordingMailboxConn struct {
	countingConn
	bytes.Buffer
}

func (c *recordingMailboxConn) Write(b []byte) (int, error) {
	c.writes++
	return c.Buffer.Write(b)
}

func (c *recordingMailboxConn) Read(b []byte) (int, error) { return c.Buffer.Read(b) }

func mailboxClient() (*client, *recordingMailboxConn) {
	cc := &recordingMailboxConn{}
	s := &Server{clients: make(map[uuid.UUID]*client)}
	c := &client{srv: s, conn: protocol.NewConn(cc)}
	c.out = &mailbox{c: c, wake: make(chan struct{}, 1), ready: make(chan struct{})}
	c.out.release()
	<-c.out.wake
	s.assignPlayerSlot(c)
	s.clients[uuid.Nil] = c
	return c, cc
}

func TestWriteBatchKeepsBusySpareAndReleasesIdle(t *testing.T) {
	cl := &client{conn: protocol.NewConn(&countingConn{}), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	m := &mailbox{c: cl, done: make(chan struct{})}
	fill := func(n int) {
		m.mu.Lock()
		for range n {
			m.queue = append(m.queue, outbound{id: 0x20})
		}
		m.mu.Unlock()
		m.writeBatch()
	}
	fill(2048)
	if cap(m.spare) < 2048 {
		t.Fatalf("busy batch dropped, spare cap %d", cap(m.spare))
	}
	for range 2*spareSlackFlushes + 2 { // queue and spare alternate
		fill(1)
	}
	if cap(m.spare) > spareMin {
		t.Fatalf("idle spare kept, cap %d", cap(m.spare))
	}
}
