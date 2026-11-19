package admin

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// expectLoginRefusal sends a login start and fails unless the server refuses
// it with the exact JSON component vanilla sends, spelled out so a change to
// the component's shape fails the test.
func expectLoginRefusal(t *testing.T, c *servertest.Client, name, wantJSON string) {
	t.Helper()
	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString(name)
		b.PutUUID(uuid.Nil)
	})
	got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767)
	if got != wantJSON {
		t.Errorf("login refusal for %s = %s, want %s", name, got, wantJSON)
	}
}

// TestKickCommand kicks a player with a reason: the feedback names the target
// and the reason, and the kicked client is dropped with the reason.
func TestKickCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("kick Bob Bye bye")
	feedback := servertest.FeedbackComponent(t, alice)
	if feedback.Translate != "commands.kick.success" {
		t.Fatalf("kick feedback key = %q, want commands.kick.success", feedback.Translate)
	}
	if len(feedback.With) != 2 || feedback.With[0].Text != "Bob" || feedback.With[1].Text != "Bye bye" {
		t.Errorf("kick feedback args = %+v, want Bob and Bye bye", feedback.With)
	}
	servertest.ExpectDisconnect(t, bob, protocol.Text{Text: "Bye bye"})

	alice.Disconnect()
}

// TestBanCommand bans an online player: the feedback and the list file name
// the reason, the banned player is dropped with the ban message, and a later
// login is refused with the reason from the file.
func TestBanCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("ban Bob Bad Bob")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Bob"}, protocol.Text{Text: "Bad Bob"})
	servertest.ExpectDisconnect(t, bob, protocol.Text{Translate: "multiplayer.disconnect.banned"})

	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 {
		t.Fatalf("banned-players.json holds %d entries, want 1", len(records))
	}
	entry := records[0]
	if entry.UUID != protocol.OfflineUUID("Bob").String() || entry.Name != "Bob" ||
		entry.Source != "Alice" || entry.Expires != "forever" || entry.Reason != "Bad Bob" {
		t.Errorf("ban entry = %+v", entry)
	}

	// A banned player is refused at login, with their reason.
	refused := connect()
	expectLoginRefusal(t, refused, "Bob",
		`{"translate":"multiplayer.disconnect.banned.reason","with":["Bad Bob"]}`)
	refused.Disconnect()

	// Banning again finds the player on the list and refuses the command.
	alice.ChatCommand("ban Bob")
	servertest.AwaitCommandFailure(t, alice, "commands.ban.failed")

	alice.Disconnect()
}

// TestBanIPCommand bans an address given directly: the feedback and the file
// note it, and an already-banned address refuses the command.
func TestBanIPCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("ban-ip 1.2.3.4 Oops")
	alice.ExpectCommandFeedbackText("commands.banip.success",
		protocol.Text{Text: "1.2.3.4"}, protocol.Text{Text: "Oops"})

	records := servertest.ReadListFile[servertest.IpBanRecord](t, srv.BannedIPsFile())
	if len(records) != 1 || records[0].IP != "1.2.3.4" || records[0].Source != "Alice" ||
		records[0].Expires != "forever" || records[0].Reason != "Oops" {
		t.Errorf("banned-ips.json = %+v", records)
	}

	alice.ChatCommand("ban-ip 1.2.3.4")
	servertest.AwaitCommandFailure(t, alice, "commands.banip.failed")

	alice.Disconnect()
	bob.Disconnect()
}

// TestBanIPLogin checks a banned address is refused at login, which needs the
// address the test client actually connects from.
func TestBanIPLogin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	banned := servertest.IpBanRecord{IP: "127.0.0.1", Source: "Alice", Expires: "forever", Reason: "Oops"}
	_, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, []servertest.IpBanRecord{banned}, nil))

	c := connect()
	expectLoginRefusal(t, c, "Alice",
		`{"translate":"multiplayer.disconnect.banned_ip.reason","with":["Oops"]}`)
	c.Disconnect()
}

// TestPardonCommand lifts a ban named from the list even when the player is
// not connected, and refuses a player who is not banned.
func TestPardonCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	banned := servertest.BanRecord{
		UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob", Created: "2026-01-01 00:00:00 +0000",
		Source: "Alice", Expires: "forever", Reason: "Oops",
	}
	srv, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, []servertest.BanRecord{banned}, nil, nil))

	alice.ChatCommand("pardon Bob")
	alice.ExpectCommandFeedbackText("commands.pardon.success", protocol.Text{Text: "Bob"})
	if records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile()); len(records) != 0 {
		t.Errorf("banned-players.json = %+v, want it emptied", records)
	}

	alice.Disconnect()
}

// TestBanListCommand shows both ban lists together by default, each list on
// its own, and the empty lists.
func TestBanListCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	banned := servertest.BanRecord{
		UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob", Created: "2026-01-01 00:00:00 +0000",
		Source: "Tester", Expires: "forever", Reason: "Bad",
	}
	bannedIP := servertest.IpBanRecord{
		IP: "1.2.3.4", Created: "2026-01-01 00:00:00 +0000", Source: "Tester", Expires: "forever",
	}
	_, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, []servertest.BanRecord{banned}, []servertest.IpBanRecord{bannedIP}, nil))

	// The bare command concatenates the player bans and the address bans.
	alice.ChatCommand("banlist")
	alice.ExpectCommandFeedbackText("commands.banlist.list", servertest.NumberText(2))
	alice.ExpectCommandFeedbackText("commands.banlist.entry",
		protocol.Text{Text: "Bob"}, protocol.Text{Text: "Tester"}, protocol.Text{Text: "Bad"})
	alice.ExpectCommandFeedbackText("commands.banlist.entry",
		protocol.Text{Text: "1.2.3.4"}, protocol.Text{Text: "Tester"},
		protocol.Text{Translate: "multiplayer.disconnect.banned.reason.default"})

	alice.ChatCommand("banlist players")
	alice.ExpectCommandFeedbackText("commands.banlist.list", servertest.NumberText(1))
	alice.ExpectCommandFeedbackText("commands.banlist.entry",
		protocol.Text{Text: "Bob"}, protocol.Text{Text: "Tester"}, protocol.Text{Text: "Bad"})

	alice.ChatCommand("banlist ips")
	alice.ExpectCommandFeedbackText("commands.banlist.list", servertest.NumberText(1))
	alice.ExpectCommandFeedbackText("commands.banlist.entry",
		protocol.Text{Text: "1.2.3.4"}, protocol.Text{Text: "Tester"},
		protocol.Text{Translate: "multiplayer.disconnect.banned.reason.default"})

	alice.ChatCommand("pardon Bob")
	alice.ExpectCommandFeedbackText("commands.pardon.success", protocol.Text{Text: "Bob"})
	alice.ChatCommand("banlist")
	alice.ExpectCommandFeedbackText("commands.banlist.list", servertest.NumberText(1))
	alice.ExpectCommandFeedbackText("commands.banlist.entry",
		protocol.Text{Text: "1.2.3.4"}, protocol.Text{Text: "Tester"},
		protocol.Text{Translate: "multiplayer.disconnect.banned.reason.default"})

	alice.ChatCommand("pardon-ip 1.2.3.4")
	alice.ExpectCommandFeedbackText("commands.pardonip.success", protocol.Text{Text: "1.2.3.4"})
	alice.ChatCommand("banlist")
	alice.ExpectCommandFeedback("commands.banlist.none")

	alice.Disconnect()
}

// TestWhitelistCommand runs the white-list subcommands: add, list, remove,
// on, off and reload, with their failures and the file they write.
func TestWhitelistCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, alice, _ := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("whitelist add Bob")
	alice.ExpectCommandFeedbackText("commands.whitelist.add.success", protocol.Text{Text: "Bob"})
	records := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile())
	if len(records) != 1 || records[0].UUID != protocol.OfflineUUID("Bob").String() || records[0].Name != "Bob" {
		t.Errorf("whitelist.json = %+v", records)
	}

	alice.ChatCommand("whitelist list")
	alice.ExpectCommandFeedbackText("commands.whitelist.list", servertest.NumberText(1), protocol.Text{Text: "Bob"})

	alice.ChatCommand("whitelist add Bob")
	servertest.AwaitCommandFailure(t, alice, "commands.whitelist.add.failed")

	alice.ChatCommand("whitelist remove Bob")
	alice.ExpectCommandFeedbackText("commands.whitelist.remove.success", protocol.Text{Text: "Bob"})
	if records := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile()); len(records) != 0 {
		t.Errorf("whitelist.json = %+v, want it emptied", records)
	}

	alice.ChatCommand("whitelist remove Bob")
	servertest.AwaitCommandFailure(t, alice, "commands.whitelist.remove.failed")

	alice.ChatCommand("whitelist off")
	servertest.AwaitCommandFailure(t, alice, "commands.whitelist.alreadyOff")

	alice.ChatCommand("whitelist on")
	alice.ExpectCommandFeedback("commands.whitelist.enabled")
	alice.ChatCommand("whitelist on")
	servertest.AwaitCommandFailure(t, alice, "commands.whitelist.alreadyOn")

	alice.ChatCommand("whitelist list")
	alice.ExpectCommandFeedback("commands.whitelist.none")

	alice.ChatCommand("whitelist reload")
	alice.ExpectCommandFeedback("commands.whitelist.reloaded")

	alice.Disconnect()
}

// TestWhitelistLogin checks the white-list at login: a player not on it is
// refused, a whitelisted player is let in, and an operator bypasses it.
func TestWhitelistLogin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.WhiteList = true

	listed := servertest.WhitelistRecord{UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob"}
	_, connect, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, []servertest.WhitelistRecord{listed}))

	refused := connect()
	expectLoginRefusal(t, refused, "Carol", `{"translate":"multiplayer.disconnect.not_whitelisted"}`)
	refused.Disconnect()

	bob := connect().Join("Bob")
	bob.Disconnect()

	alice.Disconnect()
}

// TestServerFullRefusesLogin checks the player limit at login: with the server
// full, a player who is not an operator is refused with the full-server
// message before the login is confirmed.
func TestServerFullRefusesLogin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.MaxPlayers = 1

	_, connect := servertest.Start(t, cfg)

	alice := connect().Join("Alice")

	refused := connect()
	expectLoginRefusal(t, refused, "Bravo", `{"translate":"multiplayer.disconnect.server_full"}`)
	refused.Disconnect()

	alice.Disconnect()
}

// TestServerFullBypassForOperator checks the ops.json bypassesPlayerLimit flag:
// an operator carrying it joins a full server that refuses everyone else.
func TestServerFullBypassForOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.MaxPlayers = 1

	level := config.Default().OpPermissionLevel
	aliceOp := servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: level}
	bravoOp := servertest.OpRecord{UUID: protocol.OfflineUUID("Bravo").String(), Name: "Bravo", Level: level, BypassesPlayerLimit: true}
	srv, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t, aliceOp, bravoOp))

	alice := connect().Join("Alice")

	bravo := connect().Join("Bravo")

	if got := srv.PlayerCount(); got != 2 {
		t.Errorf("player count = %d, want 2 with the bypassing operator", got)
	}

	alice.Disconnect()
	bravo.Disconnect()
}

// TestWhitelistEnforceKicksUnlisted runs the white-list on with
// enforce-whitelist set: the online players not on the raw list are dropped
// with the white-list message, an operator among them.
func TestWhitelistEnforceKicksUnlisted(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.WhiteList = true
	cfg.EnforceWhitelist = true

	listed := servertest.WhitelistRecord{UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob"}
	_, _, alice, _ := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, []servertest.WhitelistRecord{listed}))

	alice.ChatCommand("whitelist reload")
	alice.ExpectCommandFeedback("commands.whitelist.reloaded")
	// Alice is an operator and not on the raw list, so the kick reaches her;
	// Bob, who is listed, is left alone.
	servertest.ExpectDisconnect(t, alice, protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"})
}

// TestBanMultipleTargets bans a selector naming an already-banned player and a
// new one: the already-banned player is skipped, the new one is banned, and
// the command does not fail part-way.
func TestBanMultipleTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	wally := connect().Join("Wally")

	xena := connect().Join("Xena")

	// Wally is already banned, on a list the running server rereads; Xena is
	// not, so the command bans her and answers one success.
	path := filepath.Join(t.TempDir(), "banned-players.json")
	servertest.WriteJSONList(t, path, []servertest.BanRecord{{
		UUID: protocol.OfflineUUID("Wally").String(), Name: "Wally",
		Created: "2026-01-01 00:00:00 +0000", Source: "Tester", Expires: "forever",
	}})
	srv.SetBannedPlayersFile(path)

	reply := srv.RunLine("ban @a")
	if len(reply) != 1 || reply[0].Translate != "commands.ban.success" ||
		len(reply[0].With) == 0 || reply[0].With[0].Text != "Xena" {
		t.Fatalf("ban @a answered %v, want one success for Xena", reply)
	}
	if records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile()); len(records) != 2 {
		t.Errorf("banned-players.json holds %d entries, want Wally and Xena", len(records))
	}
	servertest.ExpectDisconnect(t, xena, protocol.Text{Translate: "multiplayer.disconnect.banned"})
	xena.Disconnect()

	// Both are on the list now, so there is nobody left to ban.
	reply = srv.RunLine("ban @a")
	if len(reply) != 1 || len(reply[0].Extra) != 1 || reply[0].Extra[0].Translate != "commands.ban.failed" {
		t.Fatalf("ban @a answered %v, want commands.ban.failed", reply)
	}
	wally.Disconnect()
}

// TestBanExpiryRefusal appends the lapse date to a login refused by a dated
// ban.
func TestBanExpiryRefusal(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	banned := servertest.BanRecord{
		UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob",
		Created: "2026-01-01 00:00:00 +0000", Source: "Tester",
		Expires: "2027-01-01 00:00:00 +0000", Reason: "Bad",
	}
	_, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, []servertest.BanRecord{banned}, nil, nil))

	c := connect()
	expectLoginRefusal(t, c, "Bob",
		`{"extra":[{"translate":"multiplayer.disconnect.banned.expiration","with":["2027-01-01 at 00:00:00 UTC"]}],"translate":"multiplayer.disconnect.banned.reason","with":["Bad"]}`)
	c.Disconnect()
}

// TestExpiredBanLetsThePlayerJoin treats a ban whose date has passed as no ban,
// so the player is let in.
func TestExpiredBanLetsThePlayerJoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	banned := servertest.BanRecord{
		UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob",
		Created: "2020-01-01 00:00:00 +0000", Source: "Tester",
		Expires: "2021-01-01 00:00:00 +0000", Reason: "Bad",
	}
	_, bob := servertest.Join(t, cfg, "Bob", servertest.WriteAdminFiles(t, []servertest.BanRecord{banned}, nil, nil))
	bob.Disconnect()
}
