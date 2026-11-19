package network

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// floodUntilKicked holds the loop and sends perSecond movement-flag packets
// for each of up to seconds seconds of ticks, and reports whether a
// Disconnect with the given translation key arrived. The rate limit checks
// once a second on the loop's clock, so a second is twenty held ticks.
func floodUntilKicked(t *testing.T, srv *server.Server, c *servertest.Client, perSecond, seconds int, key string) bool {
	t.Helper()
	servertest.HoldTicks(t, srv)
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: key})
	for range seconds {
		for range perSecond {
			var b protocol.Buffer
			b.PutU8(0)
			if err := c.Conn.WritePacket(protocol.IDPlayMovementFlags, b.Bytes()); err != nil {
				return readKick(c, want.Bytes())
			}
		}
		// Read up to the barrier, so the second's packets are counted
		// before its ticks run.
		if settleOrKick(c, want.Bytes()) {
			return true
		}
		servertest.RunTicks(t, srv, 20)
	}
	// The last second's check ran in its last tick, and a kick in it is
	// still unread.
	return settleOrKick(c, want.Bytes())
}

// settleOrKick is settleUnanswered for a connection the server may close: it
// reports whether the Disconnect with the reason came before the ping's
// answer, or before the connection ended.
func settleOrKick(c *servertest.Client, reason []byte) bool {
	// A kicked connection may refuse the ping; its Disconnect is already
	// queued to be read.
	var ping protocol.Buffer
	ping.PutI64(servertest.SettlePing)
	_ = c.Conn.WritePacket(protocol.IDPlayPingRequest, ping.Bytes())
	deadline := time.Now().Add(servertest.ExpectTimeout)
	for {
		id, data, err := c.ReadBy(deadline)
		if err != nil {
			return false
		}
		switch {
		case id == protocol.IDPlayDisconnect:
			return string(data) == string(reason)
		case servertest.IsPingAnswer(id, data, servertest.SettlePing):
			return false
		}
	}
}

// readKick reads a connection the server has closed and reports whether it
// held the Disconnect with the reason.
func readKick(c *servertest.Client, reason []byte) bool {
	for {
		id, data, err := c.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if err != nil {
			return false
		}
		if id == protocol.IDPlayDisconnect {
			return string(data) == string(reason)
		}
	}
}

func joinedClient(t *testing.T, cfg *config.Config) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate")
	return srv, c
}

// TestRateLimitKicksAFloodingClient checks that rate-limit=50 disconnects a
// client sending 200 packets a second with disconnect.exceeded_packet_rate.
func TestRateLimitKicksAFloodingClient(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.RateLimit = 50
	srv, c := joinedClient(t, cfg)
	if !floodUntilKicked(t, srv, c, 200, 6, "disconnect.exceeded_packet_rate") {
		t.Fatal("a client sending 200 packets a second was not kicked for the packet rate")
	}
}

// TestRateLimitSparesAQuietClient checks a client under the limit stays, and
// that rate-limit=0 turns the limit off.
func TestRateLimitSparesAQuietClient(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ limit, perSecond int }{
		"under the limit": {limit: 50, perSecond: 20},
		"limit off":       {limit: 0, perSecond: 200},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.RateLimit = tc.limit
			srv, c := joinedClient(t, cfg)
			if floodUntilKicked(t, srv, c, tc.perSecond, 4, "disconnect.exceeded_packet_rate") {
				t.Fatal("the client was kicked for the packet rate")
			}
		})
	}
}

// hasJoinedQuery logs in against a stub session server and returns the query
// the server sent it.
func hasJoinedQuery(t *testing.T, cfg *config.Config) url.Values {
	t.Helper()
	got := make(chan url.Values, 1)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(stub.Close)

	cfg.OnlineMode = true
	srv, c := servertest.Dial(t, cfg)
	srv.SetSessionServer(stub.URL)
	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString("Testificate")
		b.PutUUID([16]byte{})
	})

	c.CompleteEncryption()

	select {
	case q := <-got:
		return q
	case <-time.After(10 * time.Second):
		t.Fatal("the session server was never asked")
		return nil
	}
}

// TestSessionServerFailureReasons checks the Disconnect an online-mode login
// gets from each way the session server can fail.
func TestSessionServerFailureReasons(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		key     string
	}{
		{"not verified", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }, "multiplayer.disconnect.unverified_username"},
		{"server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }, "multiplayer.disconnect.authservers_down"},
		{"unreachable", func(w http.ResponseWriter, _ *http.Request) {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
		}, "multiplayer.disconnect.authservers_down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := httptest.NewServer(tt.handler)
			t.Cleanup(stub.Close)
			cfg := config.Default()
			cfg.OnlineMode = true
			srv, c := servertest.Dial(t, cfg)
			srv.SetSessionServer(stub.URL)
			c.SendLoginStart("Testificate")
			c.CompleteEncryption()
			var got struct {
				Translate string `json:"translate"`
			}
			data := c.Expect(protocol.IDLoginDisconnect)
			if err := json.Unmarshal([]byte(protocol.NewReader(data).ReadString(32767)), &got); err != nil {
				t.Fatalf("disconnect reason: %v", err)
			}
			if got.Translate != tt.key {
				t.Errorf("reason = %q, want %q", got.Translate, tt.key)
			}
		})
	}
}
