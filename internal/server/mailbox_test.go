package server

import (
	"bytes"
	"sync"
	"testing"
	"time"

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

func TestMailboxReleasePublishesJoin(t *testing.T) {
	t.Parallel()
	c := &client{}
	if c.joined() {
		t.Fatal("client without a mailbox has joined")
	}
	m := &mailbox{ready: make(chan struct{}), wake: make(chan struct{}, 1)}
	c.out = m
	if c.joined() {
		t.Fatal("client joined before its world was sent")
	}
	m.release()
	if !c.joined() {
		t.Fatal("released client has not joined")
	}
	select {
	case <-m.ready:
	default:
		t.Fatal("release did not unblock the writer")
	}
	<-m.wake
	m.release()
	if !c.joined() || len(m.wake) != 0 {
		t.Fatal("repeated release changed the join state or woke the writer")
	}
}

func TestAMailboxQueuedDuringATickWakesAtItsEnd(t *testing.T) {
	t.Parallel()
	c, _ := mailboxClient()
	s, m := c.srv, c.out
	s.deferWakes.Store(true)
	if !c.send(1, []byte{1}) || len(m.wake) != 0 {
		t.Fatal("a packet queued during the tick must wait without waking the writer")
	}
	s.wakeMailboxes()
	if s.deferWakes.Load() || len(m.wake) != 1 {
		t.Fatal("tick end must enable wakes and wake the queued writer")
	}
	<-m.wake
	if !c.send(2, []byte{2}) || len(m.wake) != 1 {
		t.Fatal("a packet queued between ticks must wake the writer")
	}
	<-m.wake
	m.queue, m.queued = nil, 0
	s.wakeMailboxes()
	if len(m.wake) != 0 {
		t.Fatal("an empty mailbox must not wake the writer")
	}

	s.deferWakes.Store(true)
	c.send(3, nil)
	if !c.sendImmediate(4, nil) || len(m.wake) != 1 {
		t.Fatal("an immediate reply must wake the writer during a tick")
	}
	if len(m.queue) != 2 || m.queue[0].id != 3 || m.queue[1].id != 4 {
		t.Fatal("an immediate reply must follow the packets already queued")
	}
	<-m.wake

	t.Run("join release and closing always wake", func(t *testing.T) {
		m.ready = make(chan struct{})
		m.released.Store(false)
		m.readyOnce = sync.Once{}
		s.deferWakes.Store(true)
		c.send(3, nil)
		m.release()
		if !c.joined() || len(m.wake) != 1 {
			t.Fatal("join release must wake packets queued during a tick")
		}
		<-m.wake
		m.closeAfterDrain()
		if !m.closing || len(m.wake) != 1 {
			t.Fatal("closing must wake the writer even during a tick")
		}
	})
}

func TestADirectWriteFollowsTheQueuedPackets(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"joined", "held", "joining"} {
		t.Run(mode, func(t *testing.T) {
			c, cc := mailboxClient()
			m := c.out
			switch mode {
			case "held":
				release, err := c.holdWriter()
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			case "joining":
				m.ready = make(chan struct{})
				m.released.Store(false)
				m.readyOnce = sync.Once{}
			}
			c.srv.deferWakes.Store(true)
			c.send(1, []byte{1})
			c.send(2, []byte{2})
			written := make(chan error, 1)
			go func() { written <- c.writePacket(3, "third packet", []byte{3}) }()
			select {
			case err := <-written:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("direct write blocked")
			}
			want := []int32{1, 2, 3}
			if mode == "joined" {
				if len(m.queue) != 0 || m.queued != 0 {
					t.Fatal("direct write left queued packets")
				}
			} else {
				want = []int32{3}
				if len(m.queue) != 2 || m.queued != 2 || m.queue[0].id != 1 || m.queue[1].id != 2 {
					t.Fatal("direct write took packets from a held or unreleased mailbox")
				}
			}
			if cc.writes != 1 {
				t.Fatalf("writes = %d, want 1", cc.writes)
			}
			reader := protocol.NewConn(cc)
			for _, id := range want {
				gotID, data, err := reader.ReadPacket()
				if err != nil {
					t.Fatal(err)
				}
				if gotID != id || !bytes.Equal(data, []byte{byte(id)}) {
					t.Fatalf("packet = %d %v, want ID and payload %d", gotID, data, id)
				}
			}
			if cc.Len() != 0 {
				t.Fatal("unexpected bytes after the direct write")
			}
		})
	}
}
