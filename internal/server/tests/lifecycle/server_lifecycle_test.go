package lifecycle

import (
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// deadlineConn is a net.Conn that remembers the last read deadline it was
// given, the backstop the play read loop arms.
type deadlineConn struct {
	net.Conn
	mu       sync.Mutex
	deadline time.Time
}

func (c *deadlineConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return c.Conn.SetReadDeadline(t)
}

func (c *deadlineConn) readDeadline() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deadline
}

// dialThrough connects a client whose server side of the connection is
// wrapped by wrap, and returns the wrapped connection the server saw.
func dialThrough[C net.Conn](t *testing.T, srv *server.Server, wrap func(net.Conn) C) (*servertest.Client, C) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	accepted := make(chan C, 1)
	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		wrapped := wrap(conn)
		accepted <- wrapped
		srv.HandleConnection(wrapped)
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
		<-served
	})
	c := &servertest.Client{T: t, Conn: protocol.NewConn(conn), Raw: conn, Served: served, ViewAsk: 4}
	select {
	case wrapped := <-accepted:
		return c, wrapped
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("the server accepted no connection")
		panic("unreachable")
	}
}

// TestPlayerStaysConnected answers the keep-alives of a hundred held ticks and
// checks the player stays, then stops answering and checks the first ping
// left out is the one that drops them.
func TestPlayerStaysConnected(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")
	servertest.HoldKeepAlive(t, srv, servertest.KeepAliveWaitTicks)

	// Answering the keep-alives is all a client has to do to stay in the
	// world; the ping interval is a tick and the timeout forty, so a hundred
	// ticks cover several timeouts.
	var answered int
	reader := servertest.TickReader{T: c.T, Conn: c.Conn, ReadBy: c.ReadBy, Unread: c.Unread}
	reader.AnswerKeepAlive = func(data []byte) {
		answered++
		c.AnswerKeepAlive(data)
	}
	servertest.ReadTicksVia(reader, srv, 100, func(int32, []byte) bool { return false })
	if answered == 0 {
		t.Fatal("server never sent a keep-alive")
	}
	if got := srv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want 1", got)
	}

	// A client that stops answering is dropped.
	servertest.RunTicks(t, srv, servertest.KeepAliveWaitTicks+2)
	servertest.AwaitServed(t, c.Served)
	if got := srv.PlayerCount(); got != 0 {
		t.Errorf("player count = %d, want 0 after the timeout", got)
	}
}

func TestPlayerLeaves(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Configure()
	c.Expect(protocol.IDPlayLogin)

	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("player count = %d, want 1", got)
	}

	c.Conn.Close()
	servertest.AwaitServed(t, c.Served)
	if got := srv.PlayerCount(); got != 0 {
		t.Errorf("player count = %d, want 0 after the leave", got)
	}
}

// TestStopCommandDisconnectsPlayers runs /stop the way the console does and
// checks the player is told the server is closing, the shutdown the stop
// request reaches being the same one a signal reaches.
// vanilla: StopCommand, MinecraftServer.stopServer, PlayerList.removeAll
func TestStopCommandDisconnectsPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, _, alice := servertest.AdminSolo(t, cfg)

	srv.RunLine("stop")
	select {
	case <-srv.StopRequested():
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("the stop was not requested")
	}
	srv.Shutdown()

	// The stop's own feedback, broadcast to the operator, is queued ahead of
	// the Disconnect the shutdown sends behind it.
	alice.ExpectPlay(protocol.IDPlaySystemChat)
	servertest.ExpectDisconnect(t, alice, protocol.Text{Translate: "multiplayer.disconnect.server_shutdown"})
}

// stallingConn is a net.Conn whose writes stop once stall is called and block
// until unblock or the write deadline they were given passes, standing in for
// a client that stopped reading with its socket's send buffer full.
type stallingConn struct {
	net.Conn
	mu       sync.Mutex
	stalled  bool
	deadline time.Time
	release  chan struct{}
	once     sync.Once
}

func (c *stallingConn) stall() {
	c.mu.Lock()
	c.stalled = true
	c.mu.Unlock()
}

func (c *stallingConn) unblock() { c.once.Do(func() { close(c.release) }) }

func (c *stallingConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return c.Conn.SetWriteDeadline(t)
}

func (c *stallingConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	stalled, deadline := c.stalled, c.deadline
	c.mu.Unlock()
	if !stalled {
		return c.Conn.Write(p)
	}
	var expired <-chan time.Time
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d <= 0 {
			return 0, os.ErrDeadlineExceeded
		}
		timer := time.NewTimer(d)
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-c.release:
		return c.Conn.Write(p)
	case <-expired:
		return 0, os.ErrDeadlineExceeded
	}
}

// TestShutdownWhileAPlayerIsConnected shuts the server down under a player
// who is still connected, and closes the client's connection while the
// shutdown is held on their chunk sender: the shutdown's stop is the first
// to wait, so the client's own deferred stop must wait beside it — one that
// returns at once lets the leave that follows give the chunks up under the
// take still running, and the take's next step lands on the nil world the
// leave leaves behind. Not parallel: it parks a take and a shutdown across
// a leave, whose one-second windows a sibling's share of the machine
// narrows.
func TestShutdownWhileAPlayerIsConnected(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	// A simulation distance above the render distance keeps the sender's
	// paced takes of the queued holds going for a long while after the join,
	// so the shutdown lands mid-fill rather than on a sender that has
	// finished and gone quiet.
	cfg.SimulationDistance = 16

	srv, c := servertest.Join(t, cfg, "Testificate", func(s *server.Server) {
		// No keep-alive or time-sync traffic: packets the read loop answers
		// in the window only give its goroutine more chances to meet the
		// sender's, which is the opposite of what the test is for.
		s.SetKeepAlive(time.Hour, servertest.DefaultKeepAliveTimeout)
		s.SetTimeSync(0)
	})

	// Armed once the join is over: the sender's batches and takes share one
	// beat, so a take held earlier would hold the join's own batches.
	// The sender writes the client's chunk bookkeeping a paced take at a
	// time, outside every lock the leave's release takes. Holding it just
	// after a take's writes is what places one of those writes beside the
	// shutdown on every run, however fast or slow the run is.
	held := make(chan struct{}, 1)
	var taken, drop sync.Once
	release := make(chan struct{})
	srv.HoldChunkTakes(func() {
		taken.Do(func() {
			held <- struct{}{}
			<-release
		})
	})
	t.Cleanup(func() {
		srv.HoldChunkTakes(nil)
		drop.Do(func() { close(release) })
	})

	select {
	case <-held:
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("the chunk sender made no paced take")
	}

	// The shutdown stops the sender of a player who is still connected and
	// waits for it; the seam says when that wait is reached, so the client
	// leaves under it rather than at the scheduler's mercy.
	reached, clientStop := make(chan struct{}), make(chan struct{})
	var stops atomic.Int32
	srv.WatchChunkStops(func() {
		switch stops.Add(1) {
		case 1:
			close(reached)
		case 2:
			close(clientStop)
		}
	})
	t.Cleanup(func() { srv.WatchChunkStops(nil) })

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		srv.Shutdown()
	}()
	select {
	case <-reached:
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("the shutdown never reached the sender's stop")
	}

	// The client leaves while the shutdown still waits on the sender. Its
	// own deferred stop is the second to reach the wait, and only one that
	// waits for the sender holds the leave back; one that returns at once
	// has the leave finished by the time the take is let go on below.
	c.Conn.Close()
	select {
	case <-clientStop:
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("the leaving client's stop never reached the sender's wait")
	}
	select {
	case <-c.Served:
		t.Error("the leave finished under the sender the shutdown still waited on")
	default:
	}
	drop.Do(func() { close(release) })
	<-stopped
	<-c.Served
}
