package management

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BanlistService, MinecraftBanListServiceImpl, UserBanList.add,
// remove
func TestBanMethods(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()), servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)
	alice := connect().Join("Alice")

	ban := `{"player":` + playerJSON("Alice") + `,"reason":"griefing","source":"Management server","expires":"2030-01-01T00:00:00Z"}`
	res, added := callNotified(t, c, "minecraft:bans/add",
		[]any{[]any{map[string]any{"player": map[string]any{"name": "Alice"}, "reason": "griefing", "expires": "2030-01-01T00:00:00Z"}}}, "bans/added")
	servertest.ExpectDisconnect(t, alice, protocol.Text{Translate: "multiplayer.disconnect.banned"})
	sameJSON(t, "add", res, `[`+ban+`]`)
	if len(added) != 1 {
		t.Fatalf("add sent %d bans/added, want 1", len(added))
	}
	sameJSON(t, "added", added[0], `[`+ban+`]`)
	// The file keeps the server's zone, as vanilla's SimpleDateFormat does.
	wantExpires := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).In(time.Local).Format("2006-01-02 15:04:05 -0700")
	got := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(got) != 1 || got[0].Name != "Alice" || got[0].Expires != wantExpires || got[0].Reason != "griefing" {
		t.Errorf("banned-players.json = %+v, want Alice until %s", got, wantExpires)
	}

	errorData(t, c, "bans/add", []any{[]any{map[string]any{"player": map[string]any{"name": "Bob"}, "expires": "soon"}}},
		-32602, "Text 'soon' could not be parsed")

	srv.RunLine("pardon Alice")
	sameJSON(t, "pardon", c.ExpectNotification("minecraft:notification/bans/removed"), `[`+playerJSON("Alice")+`]`)
	sameJSON(t, "get", result(t, c, "bans", nil), `[]`)
}

// vanilla: IpBanlistService, MinecraftBanListServiceImpl, IpBanList.add,
// remove
func TestIPBanMethods(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()), servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)

	res, added := callNotified(t, c, "minecraft:ip_bans/add", []any{[]any{map[string]any{"ip": "10.0.0.1"}}}, "ip_bans/added")
	listed := `{"ip":"10.0.0.1","source":"Management server"}`
	sameJSON(t, "add", res, `[`+listed+`]`)
	if len(added) != 1 {
		t.Fatalf("add sent %d ip_bans/added, want 1", len(added))
	}
	sameJSON(t, "added", added[0], `[`+listed+`]`)

	bob := connect().Join("Bob")
	res = result(t, c, "ip_bans/add", []any{[]any{map[string]any{"player": map[string]any{"name": "Bob"}}}})
	servertest.ExpectDisconnect(t, bob, protocol.Text{Translate: "multiplayer.disconnect.ip_banned"})
	both := `[` + listed + `,{"ip":"127.0.0.1","source":"Management server"}]`
	sameJSON(t, "add by player", res, both)

	sameJSON(t, "invalid", result(t, c, "ip_bans/add", []any{[]any{map[string]any{"ip": "not-an-ip"}}}), both)

	res, removed := callNotified(t, c, "minecraft:ip_bans/remove", []any{[]any{"10.0.0.1"}}, "ip_bans/removed")
	sameJSON(t, "remove", res, `[{"ip":"127.0.0.1","source":"Management server"}]`)
	if len(removed) != 1 {
		t.Fatalf("remove sent %d ip_bans/removed, want 1", len(removed))
	}
	sameJSON(t, "removed", removed[0], `["10.0.0.1"]`)

	sameJSON(t, "clear", result(t, c, "ip_bans/clear", nil), `[]`)
}

// vanilla: PlayerService.kick
func TestKickMethod(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()))
	c := servertest.ManagementClientFor(t, srv)
	carol := connect().Join("Carol")

	res := result(t, c, "players/kick", []any{[]any{map[string]any{"player": map[string]any{"name": "Carol"}, "message": map[string]any{"literal": "bye"}}}})
	servertest.ExpectDisconnect(t, carol, protocol.Text{Text: "bye"})
	sameJSON(t, "kick", res, `[{"name":"Carol"}]`)

	sameJSON(t, "offline", result(t, c, "players/kick", []any{[]any{map[string]any{"player": map[string]any{"name": "Nobody"}}}}), `[]`)
}
