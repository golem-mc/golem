package management

import (
	"encoding/json"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

var notifiedID atomic.Int64

// callNotified calls method and returns its result with the params of every
// notification of the named kind sent before the answer, which Call skips.
func callNotified(t *testing.T, c *servertest.ManagementClient, method string, params any, kind string) (json.RawMessage, []json.RawMessage) {
	t.Helper()
	id := `"n` + strconv.FormatInt(notifiedID.Add(1), 10) + `"`
	req := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	c.Send(string(b[:len(b)-1]) + `,"id":` + id + `}`)
	var notes []json.RawMessage
	for {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		raw := c.Next()
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		switch {
		case m.Method == "minecraft:notification/"+kind:
			notes = append(notes, m.Params)
		case m.Method != "":
		case string(m.ID) != id || m.Error != nil:
			t.Fatalf("%s: got %s", method, raw)
		default:
			return m.Result, notes
		}
	}
}

func playerJSON(name string) string {
	return `{"id":"` + protocol.OfflineUUID(name).String() + `","name":"` + name + `"}`
}

// vanilla: AllowlistService, MinecraftAllowListServiceImpl, UserWhiteList.add,
// remove, clear, MinecraftServer.kickUnlistedPlayers
func TestAllowlistMethods(t *testing.T) {
	t.Parallel()
	cfg := servertest.ManagementConfig(offline())
	cfg.WhiteList = true
	cfg.EnforceWhitelist = true
	// Alice is an operator, which lets her past the login check but not the
	// enforced list.
	srv, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil), servertest.OpsListing(t, "Alice"))
	c := servertest.ManagementClientFor(t, srv)

	res, added := callNotified(t, c, "minecraft:allowlist/add", []any{[]any{map[string]any{"name": "Bob"}}}, "allowlist/added")
	sameJSON(t, "add", res, `[`+playerJSON("Bob")+`]`)
	if len(added) != 1 {
		t.Fatalf("add sent %d allowlist/added, want 1", len(added))
	}
	sameJSON(t, "added", added[0], `[`+playerJSON("Bob")+`]`)
	if got := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile()); len(got) != 1 || got[0].Name != "Bob" {
		t.Errorf("whitelist.json = %+v, want Bob", got)
	}

	srv.RunLine("whitelist add Carol")
	sameJSON(t, "command added", c.ExpectNotification("minecraft:notification/allowlist/added"), `[`+playerJSON("Carol")+`]`)
	sameJSON(t, "get", result(t, c, "allowlist", nil), `[`+playerJSON("Bob")+`,`+playerJSON("Carol")+`]`)

	alice := connect().Join("Alice")
	res, removed := callNotified(t, c, "minecraft:allowlist/set", []any{[]any{}}, "allowlist/removed")
	sameJSON(t, "set", res, `[]`)
	if len(removed) != 2 {
		t.Fatalf("set sent %d allowlist/removed, want 2", len(removed))
	}
	sameJSON(t, "removed Bob", removed[0], `[`+playerJSON("Bob")+`]`)
	sameJSON(t, "removed Carol", removed[1], `[`+playerJSON("Carol")+`]`)
	servertest.ExpectDisconnect(t, alice, protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"})

	result(t, c, "allowlist/add", []any{[]any{map[string]any{"name": "Dave"}}})
	sameJSON(t, "clear", result(t, c, "allowlist/clear", nil), `[]`)
}
