package network

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestListenOnIPv6ServerIP checks that an IPv6 server-ip binds and answers a
// status ping.
func TestListenOnIPv6ServerIP(t *testing.T) {
	t.Parallel()
	probe, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ServerIP = "::1"
	cfg.ServerPort = port
	srv, _ := servertest.Start(t, cfg)

	served := make(chan error, 1)
	go func() { served <- srv.Listen() }()
	t.Cleanup(func() {
		srv.RequestStop()
		select {
		case err := <-served:
			_ = err
		case <-time.After(10 * time.Second):
		}
	})

	addr := net.JoinHostPort("::1", fmt.Sprint(port))
	deadline := time.Now().Add(5 * time.Second)
	var conn net.Conn
	for {
		select {
		case err := <-served:
			t.Fatalf("listen ended: %v", err)
		default:
		}
		conn, err = net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() { conn.Close() })

	c := &servertest.Client{T: t, Raw: conn, Conn: protocol.NewConn(conn), ViewAsk: 4}
	c.Handshake(protocol.NextStateStatus)
	c.Send(protocol.IDStatusRequest, nil)
	c.Expect(protocol.IDStatusResponse)
}
