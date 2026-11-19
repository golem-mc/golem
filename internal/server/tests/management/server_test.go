package management

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// result returns a call's result, failing on an error answer.
func result(t *testing.T, c *servertest.ManagementClient, method string, params any) json.RawMessage {
	t.Helper()
	obj := c.Call(method, params)
	if e, bad := obj["error"]; bad {
		t.Fatalf("%s: error %s", method, e)
	}
	return obj["result"]
}

func errorData(t *testing.T, c *servertest.ManagementClient, method string, params any, code int, data string) {
	t.Helper()
	obj := c.Call(method, params)
	var e struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(obj["error"], &e); err != nil || e.Code != code || e.Data != data {
		t.Errorf("%s %v: got %s, want %d %q", method, params, obj["error"], code, data)
	}
}

// vanilla: DiscoveryService.discover
func TestServerDiscover(t *testing.T) {
	t.Parallel()
	c := servertest.ManagementClientFor(t, newManagementServer(t))
	var doc struct {
		OpenRPC string `json:"openrpc"`
		Info    struct{ Title, Version string }
		Methods []struct{ Name string }
		Schemas struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(result(t, c, "rpc.discover", nil), &doc); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range doc.Methods {
		found = found || m.Name == "minecraft:notification/server/status"
	}
	if doc.OpenRPC != "1.3.2" || doc.Info.Title != "Minecraft Server JSON-RPC" || doc.Info.Version != "3.1.0" ||
		len(doc.Methods) != 89 || !found || len(doc.Schemas.Schemas) != 14 {
		t.Errorf("discover: %s %+v, %d methods (status notification %v), %d schemas",
			doc.OpenRPC, doc.Info, len(doc.Methods), found, len(doc.Schemas.Schemas))
	}
}

// vanilla: PlayerService.get, ServerStateService.status, ServerStateService.systemMessage,
// PlayerList.placeNewPlayer, PlayerList.remove
func TestServerPlayersAndMessages(t *testing.T) {
	t.Parallel()
	srv, alice := servertest.Dial(t, servertest.ManagementConfig(offline()))
	c := servertest.ManagementClientFor(t, srv)
	c.Barrier()
	alice.Join("Alice")
	id := protocol.OfflineUUID("Alice").String()
	player := `{"id":"` + id + `","name":"Alice"}`

	sameJSON(t, "joined", c.ExpectNotification("minecraft:notification/players/joined"), `[`+player+`]`)
	sameJSON(t, "players", result(t, c, "players", nil), `[`+player+`]`)
	sameJSON(t, "status", result(t, c, "server/status", nil),
		`{"started":true,"players":[`+player+`],"version":{"name":"26.3","protocol":777}}`)

	sameJSON(t, "literal", result(t, c, "server/system_message",
		sysMsg(map[string]any{"message": map[string]any{"literal": "hello"}, "overlay": false})), `true`)
	alice.ExpectSystemText(protocol.Text{Text: "hello"})
	sameJSON(t, "translatable", result(t, c, "server/system_message", sysMsg(map[string]any{"message": map[string]any{
		"translatable": "multiplayer.player.joined", "translatableParams": []string{"Bob"}}, "overlay": true})), `true`)
	alice.ExpectSystemChat("multiplayer.player.joined", true, protocol.Text{Text: "Bob"})
	sameJSON(t, "no receivers", result(t, c, "server/system_message", sysMsg(map[string]any{
		"message": map[string]any{"literal": "x"}, "overlay": false, "receivingPlayers": []any{}})), `false`)
	sameJSON(t, "no text", result(t, c, "server/system_message", sysMsg(map[string]any{
		"message": map[string]any{}, "overlay": false})), `false`)
	sameJSON(t, "by name", result(t, c, "server/system_message", sysMsg(map[string]any{
		"message": map[string]any{"literal": "named"}, "overlay": false,
		"receivingPlayers": []any{map[string]any{"name": "alice"}}})), `true`)
	alice.ExpectSystemText(protocol.Text{Text: "named"})

	// A null element makes the list absent: the message goes to everyone, and
	// the translatable has no args. vanilla: JsonOps.getStream, lenientOptionalFieldOf
	bob := servertest.DialServer(t, srv)
	bob.Join("Bob")
	c.ExpectNotification("minecraft:notification/players/joined")
	sameJSON(t, "null receiver", result(t, c, "server/system_message", sysMsg(map[string]any{
		"message": map[string]any{"literal": "all"}, "overlay": false, "receivingPlayers": []any{nil}})), `true`)
	var all protocol.Buffer
	all.PutText(protocol.Text{Text: "all"})
	all.PutBool(false)
	awaitSystemChat(alice, all.Bytes())
	awaitSystemChat(bob, all.Bytes())
	sameJSON(t, "null param", result(t, c, "server/system_message", sysMsg(map[string]any{
		"message": map[string]any{"translatable": "k", "translatableParams": []any{"a", nil}}, "overlay": false})), `true`)
	awaitSystemChat(alice, servertest.SystemChatPayload("k", false))
	bob.Disconnect()
	c.ExpectNotification("minecraft:notification/players/left")

	alice.Disconnect()
	sameJSON(t, "left", c.ExpectNotification("minecraft:notification/players/left"), `[`+player+`]`)
	sameJSON(t, "status after", result(t, c, "server/status", nil),
		`{"started":true,"version":{"name":"26.3","protocol":777}}`)
}

// awaitSystemChat reads c's system chats until one is want, stepping over
// Bob's join traffic and announcement.
func awaitSystemChat(c *servertest.Client, want []byte) {
	c.T.Helper()
	for !bytes.Equal(c.AwaitPlay(protocol.IDPlaySystemChat), want) {
	}
}

// sysMsg is server/system_message's by-name params: its one param, message,
// is the whole system message object.
func sysMsg(m map[string]any) map[string]any { return map[string]any{"message": m} }

func offline() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// vanilla: ServerStateService.save, DedicatedServer.saveAllChunks
func TestServerSave(t *testing.T) {
	t.Parallel()
	c := servertest.ManagementClientFor(t, newManagementServer(t))
	c.Send(`{"jsonrpc":"2.0","id":1,"method":"minecraft:server/save","params":[false]}`)
	var order []string
	for len(order) < 3 {
		var m struct {
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(c.Next(), &m); err != nil {
			t.Fatal(err)
		}
		if m.Method != "" {
			order = append(order, m.Method)
		} else {
			order = append(order, "result "+string(m.Result))
		}
	}
	want := []string{"minecraft:notification/server/saving", "minecraft:notification/server/saved", "result true"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("got %v, want %v", order, want)
		}
	}
}

// vanilla: IncomingRpcMethod.Method.apply, ParameterlessMethod.apply
func TestServerParamErrors(t *testing.T) {
	t.Parallel()
	c := servertest.ManagementClientFor(t, newManagementServer(t))
	errorData(t, c, "server/save", map[string]any{}, -32602, "Params passed by-name, but expected param [flush] does not exist")
	errorData(t, c, "server/save", []any{}, -32602, "Expected exactly one element in the params array")
	errorData(t, c, "players", []any{1}, -32602, "Expected no params, or an empty array")
	for _, p := range []any{[]any{"x"}, map[string]any{"flush": "x"}} {
		obj := c.Call("server/save", p)
		var e struct{ Code int }
		if err := json.Unmarshal(obj["error"], &e); err != nil || e.Code != -32602 {
			t.Errorf("server/save %v: got %s, want code -32602", p, obj["error"])
		}
	}
	errorData(t, c, "server/save", map[string]any{"flush": "x"}, -32602, `Not a boolean: "x"`)
	errorData(t, c, "server/system_message", sysMsg(map[string]any{"message": map[string]any{"literal": "x"}}),
		-32602, `No key overlay in MapLike[{"message":{"literal":"x"}}]`)
	// RecordCodecBuilder joins field errors in reverse field order; a null is a missing key.
	for _, tc := range []struct {
		msg  map[string]any
		data string
	}{
		{map[string]any{}, `No key overlay in MapLike[{}]; No key message in MapLike[{}]`},
		{map[string]any{"message": map[string]any{"literal": 5}, "overlay": "y"}, `Not a boolean: "y"; Not a string: 5`},
		{map[string]any{"message": 5, "overlay": false}, `Not a JSON object: 5`},
		{map[string]any{"message": map[string]any{"literal": 5, "translatable": 6}, "overlay": false},
			`Not a string: 6; Not a string: 5`},
		{map[string]any{"message": map[string]any{"literal": "x"}, "overlay": nil},
			`No key overlay in MapLike[{"message":{"literal":"x"},"overlay":null}]`},
	} {
		errorData(t, c, "server/system_message", sysMsg(tc.msg), -32602, tc.data)
	}
}

// vanilla: MinecraftServer.stopServer, DedicatedServer.stopServer
func TestServerShutdownNotifies(t *testing.T) {
	t.Parallel()
	srv := newManagementServer(t)
	c := servertest.ManagementClientFor(t, srv)
	c.Barrier()
	go srv.Shutdown()
	for _, m := range []string{"server/stopping", "server/saving", "server/saved"} {
		c.ExpectNotification("minecraft:notification/" + m)
	}
}

// vanilla: MinecraftServer.autoSave
func TestServerAutoSaveNotifies(t *testing.T) {
	t.Parallel()
	srv := newManagementServer(t)
	c := servertest.ManagementClientFor(t, srv)
	c.Barrier()
	servertest.HoldTicks(t, srv)
	servertest.AutoSaveEveryTick(srv)
	servertest.RunTicks(t, srv, servertest.AutoSaveEvery)
	c.ExpectNotification("minecraft:notification/server/saving")
	c.ExpectNotification("minecraft:notification/server/saved")
}

// vanilla: ServerStateService.stop
func TestServerStop(t *testing.T) {
	t.Parallel()
	srv := newManagementServer(t)
	c := servertest.ManagementClientFor(t, srv)
	sameJSON(t, "stop", result(t, c, "server/stop", nil), `true`)
	select {
	case <-srv.StopRequested():
	default:
		t.Error("stop did not request a stop")
	}
}
