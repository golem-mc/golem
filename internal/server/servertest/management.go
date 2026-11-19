package servertest

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/management"
)

// ManagementSecret is the management server secret ManagementConfig sets.
var ManagementSecret = strings.Repeat("a1", 20)

// ManagementOrigin is the one Origin ManagementConfig allows.
const ManagementOrigin = "http://example.test"

// ManagementConfig turns the management server on in cfg, with
// ManagementSecret, TLS off and ManagementOrigin allowed, and returns cfg.
func ManagementConfig(cfg *config.Config) *config.Config {
	cfg.ManagementServerEnabled = true
	cfg.ManagementServerSecret = ManagementSecret
	cfg.ManagementServerTLSEnabled = false
	cfg.ManagementServerAllowedOrigins = ManagementOrigin
	return cfg
}

// ManagementClient is a test's management connection.
type ManagementClient struct {
	t    testing.TB
	conn net.Conn
	ws   *management.WSConn
	// lastID numbers Call's and Barrier's requests.
	lastID int
}

// bufferedConn reads a socket through the reader its handshake response was
// read with, which may already hold the first frame.
type bufferedConn struct {
	*bufio.Reader
	net.Conn
}

func (b bufferedConn) Read(p []byte) (int, error) { return b.Reader.Read(p) }

// DialManagement serves srv's management handler on a test listener and
// sends it a WebSocket handshake with header added. The client is nil unless
// the answer is 101.
func DialManagement(t testing.TB, srv *server.Server, header http.Header) (*ManagementClient, *http.Response) {
	t.Helper()
	ts := httptest.NewServer(srv.ManagementHandler())
	t.Cleanup(ts.Close)
	return DialManagementAddr(t, ts.Listener.Addr().String(), header)
}

// DialManagementAddr is DialManagement to a listening address.
func DialManagementAddr(t testing.TB, addr string, header http.Header) (*ManagementClient, *http.Response) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		t.Fatalf("dial management %s: %v", addr, err)
	}
	// Registered after the test server's Close, so it runs first: the server
	// waits on no hijacked connection, but the handler ends sooner this way.
	t.Cleanup(func() { conn.Close() })
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString(key))
	req.Header.Set("Sec-WebSocket-Version", "13")
	maps.Copy(req.Header, header)
	if err := conn.SetDeadline(time.Now().Add(ExpectTimeout)); err != nil {
		t.Fatal(err)
	}
	if err := req.Write(conn); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, resp
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	return &ManagementClient{t: t, conn: conn, ws: management.NewWSConn(bufferedConn{br, conn}, true)}, resp
}

// ManagementClientFor dials srv's management handler with the Bearer key.
func ManagementClientFor(t testing.TB, srv *server.Server) *ManagementClient {
	t.Helper()
	c, resp := DialManagement(t, srv, http.Header{"Authorization": {"Bearer " + ManagementSecret}})
	if c == nil {
		t.Fatalf("management handshake answered %s", resp.Status)
	}
	return c
}

// Send sends raw as one text message.
func (c *ManagementClient) Send(raw string) {
	c.t.Helper()
	if err := c.ws.WriteText([]byte(raw)); err != nil {
		c.t.Fatalf("send %s: %v", raw, err)
	}
}

// Next returns the next message, waiting up to ExpectTimeout.
func (c *ManagementClient) Next() json.RawMessage {
	c.t.Helper()
	msg, err := c.read()
	if err != nil {
		c.t.Fatalf("read management message: %v", err)
	}
	return msg
}

func (c *ManagementClient) read() (json.RawMessage, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(ExpectTimeout)); err != nil {
		return nil, err
	}
	return c.ws.ReadMessage()
}

// ExpectClose reads until the server closes the connection and returns the
// close frame's code.
func (c *ManagementClient) ExpectClose() uint16 {
	c.t.Helper()
	for {
		msg, err := c.read()
		if closed, ok := errors.AsType[*management.CloseError](err); ok {
			return closed.Code
		}
		if err != nil {
			c.t.Fatalf("read before the close: %v", err)
		}
		c.t.Fatalf("got %s, want a close", msg)
	}
}

// message is the fields of a JSON-RPC message the client reads.
type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (c *ManagementClient) decode(raw json.RawMessage) message {
	c.t.Helper()
	var m message
	if err := json.Unmarshal(raw, &m); err != nil {
		c.t.Fatalf("decode %s: %v", raw, err)
	}
	return m
}

// Call sends a request for method with a fresh numeric id, params left out
// when nil, and returns that id's response object. Notifications before it
// are skipped.
func (c *ManagementClient) Call(method string, params any) map[string]json.RawMessage {
	c.t.Helper()
	c.lastID++
	id := strconv.Itoa(c.lastID)
	req := map[string]any{"jsonrpc": "2.0", "id": c.lastID, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		c.t.Fatal(err)
	}
	c.Send(string(b))
	for {
		raw := c.Next()
		if m := c.decode(raw); m.Method != "" && m.ID == nil {
			continue
		} else if string(m.ID) != id {
			c.t.Fatalf("got %s, want the response to id %s", raw, id)
		}
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(raw, &obj)
		return obj
	}
}

// ExpectNotification returns the params of the next notification of method,
// skipping other notifications; a response fails the test.
func (c *ManagementClient) ExpectNotification(method string) json.RawMessage {
	c.t.Helper()
	for {
		raw := c.Next()
		m := c.decode(raw)
		if m.Method == "" || m.ID != nil {
			c.t.Fatalf("got %s, want a %s notification", raw, method)
		}
		if m.Method == method {
			return m.Params
		}
	}
}

// Barrier sends a request for an unknown method and fails if anything
// arrives before its error response: everything the server answered or sent
// before reading it is then known to have been read.
func (c *ManagementClient) Barrier() {
	c.t.Helper()
	c.BarrierAllowing()
}

// BarrierAllowing is Barrier that skips notifications of the listed methods
// arriving before the answer.
func (c *ManagementClient) BarrierAllowing(methods ...string) {
	c.t.Helper()
	c.lastID++
	id := `"barrier-` + strconv.Itoa(c.lastID) + `"`
	c.Send(`{"jsonrpc":"2.0","id":` + id + `,"method":"minecraft:barrier"}`)
	for {
		raw := c.Next()
		var got struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Error  struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		err := json.Unmarshal(raw, &got)
		if err == nil && got.ID == nil && slices.Contains(methods, got.Method) {
			continue
		}
		if err != nil || !bytes.Equal(got.ID, []byte(id)) || got.Error.Code != -32601 {
			c.t.Fatalf("got %s before the barrier's answer", raw)
		}
		return
	}
}
