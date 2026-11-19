package server

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/google/uuid"
)

// TestAnUnwrittenKeepAliveStillTimesOut holds a ping in a client that never
// takes it off the queue - a reader too slow to reach it - and checks that
// the timeout runs from the queueing all the same.
func TestAnUnwrittenKeepAliveStillTimesOut(t *testing.T) {
	t.Parallel()
	k := &keepAliveTicker{}
	send := func(int64) {} // handed off, never written

	t0 := time.Now()
	if err := k.tick(t0.Add(time.Hour), time.Second, 2*time.Second, send); err != nil || k.pending != 0 {
		t.Fatalf("before start: err %v, pending %d; want no ping", err, k.pending)
	}
	k.start(t0)
	if err := k.tick(t0.Add(time.Second), time.Second, 2*time.Second, send); err != nil || k.pending == 0 {
		t.Fatalf("at the interval: err %v, pending %d; want a ping out", err, k.pending)
	}
	if err := k.tick(t0.Add(3*time.Second-tickInterval), time.Second, 2*time.Second, send); err != nil {
		t.Fatalf("a tick before the timeout: %v", err)
	}
	if err := k.tick(t0.Add(3*time.Second), time.Second, 2*time.Second, send); err == nil {
		t.Fatal("a ping queued for the whole timeout did not drop the client")
	}
}

// TestADroppedClientIsToldOnce drops a client that has stopped reading - the
// peer of a net.Pipe takes nothing - and checks the drop queues one
// Disconnect: a client still reading takes the connection down before the
// next tick could queue another, so the repeat only shows while the writer
// is stalled.
func TestADroppedClientIsToldOnce(t *testing.T) {
	t.Parallel()
	s := &Server{Config: config.Default(), clients: make(map[uuid.UUID]*client)}
	s.keepAliveInterval.Store(int64(time.Millisecond))
	s.keepAliveTimeout.Store(int64(20 * time.Millisecond))

	serverEnd, peer := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); peer.Close() })
	c := &client{
		srv:  s,
		conn: protocol.NewConn(serverEnd),
		log:  slog.New(slog.DiscardHandler),
	}
	c.out = c.startMailbox()
	c.out.release()
	c.keepAlive = &keepAliveTicker{}
	s.mu.Lock()
	s.assignPlayerSlot(c)
	s.clients[uuid.New()] = c
	s.mu.Unlock()
	t.Cleanup(c.out.stop)

	// One tick a simulated millisecond apart: the ping goes out on the
	// first, and the timeout runs out well inside the stall.
	t0 := time.Now()
	c.keepAlive.start(t0)
	for i := 1; i <= 40; i++ {
		s.tickKeepAlives(t0.Add(time.Duration(i) * time.Millisecond))
	}

	// The peer now reads everything queued for it, to the end the drain's
	// close puts on the stream.
	peerConn := protocol.NewConn(peer)
	if err := peerConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	disconnects := 0
	for {
		id, _, err := peerConn.ReadPacket()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatal("the connection did not end once everything queued was read")
			}
			break
		}
		switch id {
		case protocol.IDPlayKeepAlive:
		case protocol.IDPlayDisconnect:
			disconnects++
		default:
			t.Fatalf("unexpected packet 0x%02X", id)
		}
	}
	if disconnects != 1 {
		t.Fatalf("the stalled client was queued %d Disconnects, want one", disconnects)
	}
}

// TestNoConfigurationPingOutlivesThePhase hands a configuration ping to its
// writer and ends the phase at once, with the peer reading nothing until the
// end has returned or is blocked on the ping's write: a ping written before
// the end is read while it waits, and none may arrive once it has returned,
// where play would read the configuration KeepAlive as a different packet.
func TestNoConfigurationPingOutlivesThePhase(t *testing.T) {
	t.Parallel()
	s := &Server{Config: config.Default()}
	s.keepAliveInterval.Store(int64(time.Second))
	s.keepAliveTimeout.Store(int64(time.Hour))

	serverEnd, peer := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); peer.Close() })
	c := &client{srv: s, conn: protocol.NewConn(serverEnd), log: slog.New(slog.DiscardHandler)}
	c.conn.State = protocol.StateConfiguration
	c.keepAlive = &keepAliveTicker{}
	t0 := time.Now()
	c.keepAlive.start(t0)
	stop := c.startConfigKeepAlives()
	s.tickConfigKeepAlives(t0.Add(time.Second))

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		stop()
	}()
	peerConn := protocol.NewConn(peer)
	select {
	case <-stopped:
	case <-time.After(200 * time.Millisecond):
		// The end waits on a ping being written; taking it lets both finish.
		if err := peerConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if id, _, err := peerConn.ReadPacket(); err != nil || id != protocol.IDConfigKeepAlive {
			t.Fatalf("the ping the end waited on = 0x%02X, %v; want a configuration KeepAlive", id, err)
		}
		<-stopped
	}
	c.conn.State = protocol.StatePlay

	if err := peerConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if id, _, err := peerConn.ReadPacket(); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("after the phase ended the peer read 0x%02X, %v; want nothing", id, err)
	}
}
