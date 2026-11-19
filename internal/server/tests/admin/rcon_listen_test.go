package admin

import (
	"errors"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
)

// freeRconPort finds a TCP port free for the RCON listener to bind. The tests
// need a real port: RconPort 0 is one of the settings the listener refuses.
// It takes ports below every OS's ephemeral range (Linux 32768+, macOS and
// Windows 49152+): each test connection's listener binds port 0, and a port
// released from that range can go to one of them before the check dials it.
func freeRconPort(t *testing.T) int {
	t.Helper()
	rconPorts.Lock()
	defer rconPorts.Unlock()
	if rconPorts.next == 0 {
		// Spread by process, so two test binaries at once start apart.
		rconPorts.next = rconPortLow + os.Getpid()%(rconPortHigh-rconPortLow)
	}
	for range rconPortHigh - rconPortLow {
		port := rconPorts.next
		rconPorts.next++
		if rconPorts.next >= rconPortHigh {
			rconPorts.next = rconPortLow
		}
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		l.Close()
		return port
	}
	t.Fatal("no free port below the ephemeral range")
	return 0
}

const rconPortLow, rconPortHigh = 20000, 32768

// rconPorts hands each test its own port, so parallel tests never share one.
var rconPorts struct {
	sync.Mutex
	next int
}

// dialRconPort connects to a port the RCON listener may not have bound yet,
// retrying until it does or the wait runs out.
func dialRconPort(t *testing.T, port int) net.Conn {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			t.Cleanup(func() { conn.Close() })
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial rcon %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestListenRconServesUntilStop starts a real server listener with RCON on and
// checks the wiring in Listen: the listener serves a command, and a stop ends
// Listen, closes the open connection and closes the listener.
// vanilla: RconThread.stop
func TestListenRconServesUntilStop(t *testing.T) {
	t.Parallel()
	port := freeRconPort(t)
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.EnableRcon = true
	cfg.RconPassword = "sesame"
	cfg.RconPort = port
	cfg.ServerPort = 0
	srv, _ := servertest.Start(t, cfg)

	served := make(chan error, 1)
	go func() { served <- srv.Listen() }()

	conn := dialRconPort(t, port)
	c := &rconConn{t: t, conn: conn}
	c.send(1, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 1 {
		t.Fatalf("login answered id %d, want 1", got.id)
	}
	c.send(2, rconPacketCommand, "list")
	want := "There are 0 of a max of 20 players online: "
	if got := c.receive(); got.id != 2 || got.typ != rconPacketOutput || got.payload != want {
		t.Errorf("list answered %+v, want id 2, type 0, %q", got, want)
	}

	srv.RequestStop()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("listen did not end after the stop")
	}

	// The connection open when the listener stopped is closed with it: the
	// read ends at EOF, not at the deadline.
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("the open rcon connection did not close with the stop: %v", err)
	}
	// The listener itself is gone: a fresh dial is refused.
	if fresh, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		fresh.Close()
		t.Error("the rcon listener still accepted after the stop")
	}
}

// TestListenRconRefusedSettings starts the listener with RCON off and with no
// password: neither binds the port, the cases RconThread.create declines to
// run on.
func TestListenRconRefusedSettings(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*config.Config){
		"disabled":    func(c *config.Config) { c.EnableRcon = false },
		"no password": func(c *config.Config) { c.RconPassword = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			port := freeRconPort(t)
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.EnableRcon = true
			cfg.RconPassword = "sesame"
			cfg.RconPort = port
			cfg.ServerPort = 0
			mutate(cfg)
			srv, _ := servertest.Start(t, cfg)

			served := make(chan error, 1)
			go func() { served <- srv.Listen() }()
			// The listen starts listenRcon before its accept loop, so a short
			// wait is enough for a listener that should have bound to have.
			time.Sleep(100 * time.Millisecond)

			if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond); err == nil {
				conn.Close()
				t.Errorf("rcon bound %d with %s", port, name)
			}
			srv.RequestStop()
			if err := <-served; err != nil {
				t.Errorf("listen: %v", err)
			}
		})
	}
}
