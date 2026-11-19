package management

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/management"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// sameJSON fails unless got and want decode to the same value.
func sameJSON(t *testing.T, what string, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: decode %s: %v", what, got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: decode want %s: %v", what, want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s: got %s, want %s", what, got, want)
	}
}

func newManagementServer(t *testing.T) *server.Server {
	t.Helper()
	cfg := servertest.ManagementConfig(config.Default())
	cfg.OnlineMode = false
	return servertest.NewTestServer(t, cfg)
}

// vanilla: AuthenticationHandler.performSecurityChecks, sendUnauthorizedResponse
func TestTransportAuth(t *testing.T) {
	t.Parallel()
	srv := newManagementServer(t)
	key := servertest.ManagementSecret

	refused := map[string]struct {
		header http.Header
		reason string
	}{
		"no key":     {http.Header{}, "Missing API key"},
		"bad bearer": {http.Header{"Authorization": {"Bearer nope"}}, "Invalid API key"},
		// A Bearer header is checked alone: a good subprotocol key does not
		// rescue a bad one.
		"bad bearer, good subprotocol": {http.Header{"Authorization": {"Bearer nope"},
			"Sec-Websocket-Protocol": {"minecraft-v1," + key}, "Origin": {servertest.ManagementOrigin}}, "Invalid API key"},
		"subprotocol without origin": {http.Header{"Sec-Websocket-Protocol": {"minecraft-v1," + key}}, "Origin Not Allowed"},
		"subprotocol from another origin": {http.Header{"Sec-Websocket-Protocol": {"minecraft-v1," + key},
			"Origin": {"http://example.test/"}}, "Origin Not Allowed"},
		"bad subprotocol key": {http.Header{"Sec-Websocket-Protocol": {"minecraft-v1,nope"},
			"Origin": {servertest.ManagementOrigin}}, "Invalid API key"},
	}
	for name, tc := range refused {
		c, resp := servertest.DialManagement(t, srv, tc.header)
		if c != nil || resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %s, want 401", name, resp.Status)
			continue
		}
		// ReadResponse moves Connection: close into resp.Close.
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" || !resp.Close {
			t.Errorf("%s: Content-Type %q, Connection: close %v", name, ct, resp.Close)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("%s: read body: %v", name, err)
		}
		sameJSON(t, name, body, `{"error":"Unauthorized","message":"`+tc.reason+`"}`)
	}

	c, resp := servertest.DialManagement(t, srv, http.Header{
		"Sec-Websocket-Protocol": {"minecraft-v1, " + key}, "Origin": {servertest.ManagementOrigin}})
	if c == nil {
		t.Fatalf("subprotocol key from the allowed origin: status %s, want 101", resp.Status)
	}
	if got := resp.Header.Get("Sec-Websocket-Protocol"); got != "minecraft-v1" {
		t.Errorf("subprotocol 101 names %q, want minecraft-v1", got)
	}
	c.Barrier()

	c, resp = servertest.DialManagement(t, srv, http.Header{"Authorization": {"Bearer " + key}})
	if c == nil {
		t.Fatalf("Bearer key: status %s, want 101", resp.Status)
	}
	if got, ok := resp.Header["Sec-Websocket-Protocol"]; ok {
		t.Errorf("Bearer 101 names a subprotocol %q", got)
	}
	c.Barrier()
}

// vanilla: Connection.handleJsonObject, handleIncomingRequest, dispatchIncomingRequest
func TestEnvelope(t *testing.T) {
	t.Parallel()
	srv := newManagementServer(t)
	c := servertest.ManagementClientFor(t, srv)

	for _, tc := range []struct{ send, want string }{
		{`{"jsonrpc":"2.0","id":1,"method":"minecraft:no_such_method"}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found","data":"Method not found: minecraft:no_such_method"}}`},
		{`{"jsonrpc":"2.0","id":1,"method":"no_such_method"}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found","data":"Method not found: no_such_method"}}`},
		{`{"jsonrpc":"2.0","id":2,"method":"Bad Name"}`,
			`{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Invalid Request","data":"Failed to parse method value: Bad Name"}}`},
		{`{"jsonrpc":"2.0","id":{},"method":"minecraft:x"}`,
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Invalid Request","data":"Invalid request id - only String, Number and NULL supported"}}`},
		{`5`, `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Invalid Request"}}`},
		{`{"jsonrpc":"2.0","id":3}`, `{"jsonrpc":"2.0","id":3,"error":{"code":-32600,"message":"Invalid Request"}}`},
		// An explicit null id is a request, answered with id null.
		{`{"jsonrpc":"2.0","id":null,"method":"minecraft:x"}`,
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32601,"message":"Method not found","data":"Method not found: minecraft:x"}}`},
		{`{"jsonrpc":"2.0","id":"s","method":"minecraft:x"}`,
			`{"jsonrpc":"2.0","id":"s","error":{"code":-32601,"message":"Method not found","data":"Method not found: minecraft:x"}}`},
		{`[{"jsonrpc":"2.0","id":"a","method":"minecraft:x"},{"jsonrpc":"2.0","method":"minecraft:x"}]`,
			`[{"jsonrpc":"2.0","id":"a","error":{"code":-32601,"message":"Method not found","data":"Method not found: minecraft:x"}}]`},
		{`[]`, `[]`},
	} {
		c.Send(tc.send)
		sameJSON(t, tc.send, c.Next(), tc.want)
	}

	c.Send(`not json`)
	var parse struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code int    `json:"code"`
			Data string `json:"data"`
		} `json:"error"`
	}
	if raw := c.Next(); json.Unmarshal(raw, &parse) != nil || string(parse.ID) != "null" ||
		parse.Error.Code != -32700 || parse.Error.Data == "" {
		t.Errorf("not json: got %s, want a -32700 with id null and data", raw)
	}

	// A notification and an unasked response get no answer.
	c.Send(`{"jsonrpc":"2.0","method":"minecraft:x"}`)
	c.Send(`{"jsonrpc":"2.0","id":7,"result":true}`)
	c.Barrier()

	c.Send(strings.Repeat("a", 70000))
	if code := c.ExpectClose(); code != management.CloseTooBig {
		t.Errorf("a 70000-byte message closed with %d, want %d", code, management.CloseTooBig)
	}
}

// TestLenientMessages reads each message as Gson's lenient JsonParser does.
// vanilla: WebSocketToJsonCodec.decode, Connection.channelRead0
func TestLenientMessages(t *testing.T) {
	t.Parallel()
	srv := newManagementServer(t)
	c := servertest.ManagementClientFor(t, srv)

	const invalid = `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Invalid Request"}}`
	notFound := func(id string) string {
		return `{"jsonrpc":"2.0","id":` + id + `,"error":{"code":-32601,"message":"Method not found","data":"Method not found: minecraft:x"}}`
	}
	for _, tc := range []struct{ send, want string }{
		{`abc`, invalid},
		{``, invalid},
		{" \n// only a comment\n", invalid},
		{`NULL trailing`, invalid},
		{")]}'\n", invalid},
		{")]}'\n// c", invalid},
		{`{id:1,method:'minecraft:x'}`, notFound("1")},
		{`{'id'=>"a\'b";  method = 'minecraft:x' # comment` + "\n}", notFound(`"a'b"`)},
		{`)]}'` + "\n" + `/* c */ {"id":01,"method":"minecraft:x"}`, notFound(`"01"`)},
		{`[{id:2,method:"minecraft:x"},,]`, `[` + notFound("2") + `,` + invalid + `,` + invalid + `]`},
	} {
		c.Send(tc.send)
		sameJSON(t, tc.send, c.Next(), tc.want)
	}
	// A comma before '}' wants another name, and an unquoted string ends at ':'.
	for _, send := range []string{`{"id":1,}`, `{id:1,method:minecraft:x}`, `abc def`, `{"id":1} // trailing`, `{id:1`, `/* open`, `"\x"`} {
		c.Send(send)
		var parse struct {
			ID    json.RawMessage    `json:"id"`
			Error struct{ Code int } `json:"error"`
		}
		if raw := c.Next(); json.Unmarshal(raw, &parse) != nil || string(parse.ID) != "null" || parse.Error.Code != -32700 {
			t.Errorf("%q: got %s, want a -32700 with id null", send, raw)
		}
	}
}

// TestNestingLimit refuses strict JSON nested past Gson's limit of 255.
// vanilla: WebSocketToJsonCodec.decode (JsonReader nesting limit)
func TestNestingLimit(t *testing.T) {
	t.Parallel()
	srv := newManagementServer(t)
	c := servertest.ManagementClientFor(t, srv)

	nested := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	c.Send(nested(255))
	sameJSON(t, "255 levels", c.Next(), `[{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Invalid Request"}}]`)
	c.Send(nested(256))
	var parse struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code int
			Data string
		} `json:"error"`
	}
	if raw := c.Next(); json.Unmarshal(raw, &parse) != nil || string(parse.ID) != "null" || parse.Error.Code != -32700 ||
		!strings.Contains(parse.Error.Data, "Nesting limit 255 reached") {
		t.Errorf("256 levels: got %s, want a -32700 with id null for the nesting limit", raw)
	}
}

// TestListenServesManagement binds both ports on 127.0.0.1 through Listen.
// vanilla: JsonRpc.create, ManagementServer.start
func TestListenServesManagement(t *testing.T) {
	t.Parallel()
	cfg := servertest.ManagementConfig(config.Default())
	cfg.OnlineMode = false
	cfg.ServerIP = "127.0.0.1"
	cfg.ServerPort = 0
	cfg.ManagementServerHost = "127.0.0.1"
	cfg.ManagementServerPort = 0
	srv := servertest.NewTestServer(t, cfg)

	served := make(chan error, 1)
	go func() { served <- srv.Listen() }()
	addr := srv.ManagementAddr()
	if addr == "" {
		t.Fatal("the server stopped before binding the management port")
	}
	c, resp := servertest.DialManagementAddr(t, addr, http.Header{"Authorization": {"Bearer " + servertest.ManagementSecret}})
	if c == nil {
		t.Fatalf("dial %s: status %s, want 101", addr, resp.Status)
	}
	// A connection opened during startup is told the start, as vanilla's is:
	// JsonRpc.create binds before DedicatedServer.initServer notifies.
	c.BarrierAllowing("minecraft:notification/server/started")

	srv.RequestStop()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("listen did not end after the stop")
	}
}

// TestListenRefusesManagementSettings checks the settings New refuses before
// the world loads. vanilla: JsonRpc.create
func TestListenRefusesManagementSettings(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		mutate func(*config.Config)
		want   string
	}{
		"tls on":       {func(c *config.Config) { c.ManagementServerTLSEnabled = true }, "management-server-tls-enabled"},
		"short secret": {func(c *config.Config) { c.ManagementServerSecret = "short" }, "40 alphanumeric"},
	} {
		cfg := servertest.ManagementConfig(config.Default())
		cfg.OnlineMode = false
		cfg.LevelName = t.TempDir()
		tc.mutate(cfg)
		srv, err := server.New(cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			if srv != nil {
				srv.Shutdown()
			}
			t.Errorf("%s: New returned %v, want an error naming %q", name, err, tc.want)
		}
	}
}
