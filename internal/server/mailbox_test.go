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

func fillMailbox(m *mailbox, n int) {
	m.mu.Lock()
	for range n {
		m.queue = append(m.queue, outbound{id: 0x20})
	}
	m.mu.Unlock()
	m.writeBatch()
}

func testMailbox() *mailbox {
	cl := &client{conn: protocol.NewConn(&countingConn{}), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return &mailbox{c: cl, done: make(chan struct{})}
}

func TestWriteBatchKeepsBusySpareAndReleasesIdle(t *testing.T) {
	m := testMailbox()
	fillMailbox(m, 2048)
	if cap(m.spare) < 2048 {
		t.Fatalf("busy batch dropped, spare cap %d", cap(m.spare))
	}
	for range 2*spareSlackFlushes + 2 { // queue and spare alternate
		fillMailbox(m, 1)
	}
	if cap(m.spare) > spareMin {
		t.Fatalf("idle spare kept, cap %d", cap(m.spare))
	}
}

func TestWriteBatchBusySliceDoesNotResetLightSlice(t *testing.T) {
	m := testMailbox()
	m.queue = make([]outbound, 0, 2048) // light: dropped on its 8th flush
	m.spare = make([]outbound, 0, 4096) // busy: refilled past a quarter each time
	for i := 1; i < 2*spareSlackFlushes; i++ {
		if i%2 == 1 {
			fillMailbox(m, 1)
		} else {
			fillMailbox(m, 1100)
		}
		if i == 2*spareSlackFlushes-3 && cap(m.spare) != 2048 {
			t.Fatalf("light slice dropped early, spare cap %d", cap(m.spare))
		}
	}
	if cap(m.spare) != 0 || cap(m.queue) != 4096 {
		t.Fatalf("spare %d, queue %d; want light dropped, busy kept", cap(m.spare), cap(m.queue))
	}
}

func TestWriteBatchCountsSlackPerSlice(t *testing.T) {
	// A busy batch alternating with a small one must not reset or advance
	// the other buffer's count.
	m := testMailbox()
	fillMailbox(m, 2048)
	for range 2 * spareSlackFlushes {
		fillMailbox(m, 1)
	}
	if cap(m.spare) > spareMin || cap(m.queue) > spareMin {
		t.Fatalf("oversized buffer kept: spare %d, queue %d", cap(m.spare), cap(m.queue))
	}

	// Two lightly used oversized buffers each need their own flushes.
	m = testMailbox()
	m.spare = make([]outbound, 0, 2048)
	m.queue = make([]outbound, 0, 2048)
	for range 2*spareSlackFlushes - 1 {
		fillMailbox(m, 1)
	}
	if cap(m.spare) <= spareMin && cap(m.queue) <= spareMin {
		t.Fatal("both buffers dropped before 8 own flushes each")
	}
	fillMailbox(m, 1)
	if cap(m.spare) > spareMin || cap(m.queue) > spareMin {
		t.Fatalf("oversized buffer kept: spare %d, queue %d", cap(m.spare), cap(m.queue))
	}
}

func TestDirectDrainResetsQueueSlack(t *testing.T) {
	c, _ := mailboxClient()
	m := c.out
	m.queue = make([]outbound, 0, 2048)
	m.queueSlack = spareSlackFlushes - 1
	release, err := c.holdWriter()
	release()
	if err != nil || m.queueSlack != 0 {
		t.Fatalf("err %v, queueSlack %d", err, m.queueSlack)
	}
}
