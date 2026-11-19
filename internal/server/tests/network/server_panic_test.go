package network

import (
	"bytes"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// panickingConn is a server-side connection whose reads panic once armed:
// a bug on the connection's goroutine, wherever in its reading it is.
type panickingConn struct {
	net.Conn
	armed atomic.Bool
}

func (c *panickingConn) Read(p []byte) (int, error) {
	if c.armed.Load() {
		panic("test panic")
	}
	return c.Conn.Read(p)
}

// TestAPanicDropsOnlyItsConnection pins that a panic on one connection's
// goroutine drops that client alone: it is sent vanilla's disconnect for an
// exception in its pipeline, it leaves the player list, and the server goes
// on taking joins.
func TestAPanicDropsOnlyItsConnection(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv := servertest.NewTestServer(t, cfg)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	accepted := make(chan *panickingConn, 1)
	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		wrapped := &panickingConn{Conn: conn}
		accepted <- wrapped
		srv.HandleConnection(wrapped)
	}()
	raw, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		raw.Close()
		<-served
	})
	c := &servertest.Client{T: t, Conn: protocol.NewConn(raw), Raw: raw, Served: served, ViewAsk: 4}
	conn := <-accepted

	c.Join("Testificate")
	c.Settle()

	conn.armed.Store(true)
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With:      []protocol.Text{{Text: "Internal Exception: test panic"}},
	})
	for {
		id, data, err := c.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if err != nil {
			t.Fatalf("the panic brought no Disconnect: %v", err)
		}
		if id != protocol.IDPlayDisconnect {
			continue
		}
		if !bytes.Equal(data, want.Bytes()) {
			t.Errorf("disconnect reason % X, want % X", data, want.Bytes())
		}
		break
	}
	c.AwaitServed()
	if n := srv.PlayerCount(); n != 0 {
		t.Errorf("%d players online after the panicking one was dropped, want 0", n)
	}

	next := servertest.DialServer(t, srv).Join("Alex")
	next.Settle()
	if n := srv.PlayerCount(); n != 1 {
		t.Errorf("%d players online after the next join, want 1", n)
	}
	next.Disconnect()
}
