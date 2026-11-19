package server

import (
	"bytes"

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
