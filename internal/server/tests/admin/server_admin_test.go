package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

// TestKickDefaultReason kicks a player without a reason, which is the default
// "Kicked by an operator".
func TestKickDefaultReason(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("kick Bob")
	feedback := servertest.FeedbackComponent(t, alice)
	if feedback.Translate != "commands.kick.success" {
		t.Fatalf("kick feedback key = %q, want commands.kick.success", feedback.Translate)
	}
	if len(feedback.With) != 2 || feedback.With[1].Translate != "multiplayer.disconnect.kicked" {
		t.Errorf("default kick reason = %+v, want multiplayer.disconnect.kicked", feedback.With)
	}
	servertest.ExpectDisconnect(t, bob, protocol.Text{Translate: "multiplayer.disconnect.kicked"})

	alice.Disconnect()
}

// TestKickCommandResolvesSelector expands a selector in the kick reason into
// the display names it names, in the feedback and in the disconnect alike.
// vanilla: KickCommand.kickPlayers
func TestKickCommandResolvesSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	reason := protocol.Text{Text: "bye ", Extra: []protocol.Text{servertest.PlayerDisplayComponent("Alice")}}
	alice.ChatCommand("kick Bob bye @s")
	alice.ExpectCommandFeedbackText("commands.kick.success", servertest.PlayerDisplayComponent("Bob"), reason)
	servertest.ExpectDisconnect(t, bob, reason)

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

// TestBanCommandResolvesSelector expands a selector in the ban reason into the
// display names it names: the resolved text is what the feedback, the stored
// entry and the ban list carry. vanilla: BanPlayerCommands.banPlayers
func TestBanCommandResolvesSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	// The name names nobody online, so the runner stays connected to read
	// the ban list back.
	alice.ChatCommand("ban Carol bad @s")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Carol"}, protocol.Text{Text: "bad Alice"})

	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 || records[0].Reason != "bad Alice" {
		t.Fatalf("banned-players.json = %+v, want the resolved reason", records)
	}

	alice.ChatCommand("banlist players")
	alice.ExpectCommandFeedbackText("commands.banlist.list", servertest.NumberText(1))
	alice.ExpectCommandFeedbackText("commands.banlist.entry",
		protocol.Text{Text: "Carol"}, protocol.Text{Text: "Alice"}, protocol.Text{Text: "bad Alice"})

	alice.Disconnect()
}

// TestBanWithoutReason checks the default reason in the feedback and in the
// login refusal, and that the file carries no reason field.
func TestBanWithoutReason(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("ban Bob")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Bob"}, protocol.Text{Translate: "multiplayer.disconnect.banned.reason.default"})
	servertest.ExpectDisconnect(t, bob, protocol.Text{Translate: "multiplayer.disconnect.banned"})

	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 || records[0].Reason != "" {
		t.Errorf("banned-players.json = %+v, want one entry without a reason", records)
	}

	refused := connect()
	expectLoginRefusal(t, refused, "Bob",
		`{"translate":"multiplayer.disconnect.banned.reason","with":[{"translate":"multiplayer.disconnect.banned.reason.default"}]}`)
	refused.Disconnect()
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

// TestBanIPCommandResolvesSelector expands a selector in the ban-ip reason into
// the display names it names, in the feedback, the stored entry and the list.
func TestBanIPCommandResolvesSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	// The address is one nobody is behind, so the runner stays connected to
	// read the ban list back.
	alice.ChatCommand("ban-ip 1.2.3.4 bad @s")
	alice.ExpectCommandFeedbackText("commands.banip.success",
		protocol.Text{Text: "1.2.3.4"}, protocol.Text{Text: "bad Alice"})

	records := servertest.ReadListFile[servertest.IpBanRecord](t, srv.BannedIPsFile())
	if len(records) != 1 || records[0].Reason != "bad Alice" {
		t.Fatalf("banned-ips.json = %+v, want the resolved reason", records)
	}

	alice.ChatCommand("banlist ips")
	alice.ExpectCommandFeedbackText("commands.banlist.list", servertest.NumberText(1))
	alice.ExpectCommandFeedbackText("commands.banlist.entry",
		protocol.Text{Text: "1.2.3.4"}, protocol.Text{Text: "Alice"}, protocol.Text{Text: "bad Alice"})

	alice.Disconnect()
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

// TestBanIPAnonymous checks a banned address is refused at login for a player
// with no other standing.
func TestBanIPAnonymous(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	banned := servertest.IpBanRecord{IP: "127.0.0.1", Source: "Alice", Expires: "forever"}
	_, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, []servertest.IpBanRecord{banned}, nil))

	c := connect()
	expectLoginRefusal(t, c, "Carol",
		`{"translate":"multiplayer.disconnect.banned_ip.reason","with":[{"translate":"multiplayer.disconnect.banned.reason.default"}]}`)
	c.Disconnect()
}

// TestBanIPByNameKicksEveryone bans the address an online player is behind:
// every player on it is dropped with the IP ban message and named in the
// feedback, and the address is on disk.
func TestBanIPByNameKicksEveryone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("ban-ip Bob Bad")
	alice.ExpectCommandFeedbackText("commands.banip.success",
		protocol.Text{Text: "127.0.0.1"}, protocol.Text{Text: "Bad"})

	info := servertest.FeedbackComponent(t, alice)
	if info.Translate != "commands.banip.info" {
		t.Fatalf("ban-ip info key = %q, want commands.banip.info", info.Translate)
	}
	if len(info.With) != 2 || info.With[0].Number == nil || *info.With[0].Number != 2 {
		t.Errorf("ban-ip info args = %+v, want a count of two players", info.With)
	}

	records := servertest.ReadListFile[servertest.IpBanRecord](t, srv.BannedIPsFile())
	if len(records) != 1 || records[0].IP != "127.0.0.1" || records[0].Reason != "Bad" {
		t.Errorf("banned-ips.json = %+v", records)
	}

	// Both players stood behind 127.0.0.1, so both are dropped.
	servertest.ExpectDisconnect(t, bob, protocol.Text{Translate: "multiplayer.disconnect.ip_banned"})
	servertest.ExpectDisconnect(t, alice, protocol.Text{Translate: "multiplayer.disconnect.ip_banned"})
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

// TestPardonNotBanned refuses a player who is not on the ban list.
func TestPardonNotBanned(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, _ := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("pardon Bob")
	servertest.AwaitCommandFailure(t, alice, "commands.pardon.failed")

	alice.Disconnect()
}

// TestPardonIPCommand lifts an address's ban and refuses an address that is
// not an address or is not banned.
func TestPardonIPCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	banned := servertest.IpBanRecord{
		IP: "1.2.3.4", Created: "2026-01-01 00:00:00 +0000", Source: "Alice", Expires: "forever",
	}
	srv, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, []servertest.IpBanRecord{banned}, nil))

	alice.ChatCommand("pardon-ip 1.2.3.4")
	alice.ExpectCommandFeedbackText("commands.pardonip.success", protocol.Text{Text: "1.2.3.4"})
	if records := servertest.ReadListFile[servertest.IpBanRecord](t, srv.BannedIPsFile()); len(records) != 0 {
		t.Errorf("banned-ips.json = %+v, want it emptied", records)
	}

	alice.ChatCommand("pardon-ip 1.2.3.4")
	servertest.AwaitCommandFailure(t, alice, "commands.pardonip.failed")
	alice.ChatCommand("pardon-ip not-an-ip")
	servertest.AwaitCommandFailure(t, alice, "commands.pardonip.invalid")

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

// TestServerFullAtConfigurationRefusesLogin checks the capacity check that runs
// when a configuring player enters the world: both players pass login while a
// slot is free, and the one who arrives after the other has taken the last slot
// is refused with the full-server message.
func TestServerFullAtConfigurationRefusesLogin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.MaxPlayers = 1

	srv, connect := servertest.Start(t, cfg)

	// Both logins see an empty server, so both pass the login-time check.
	alice := connect()
	alice.Login("Alice")
	bravo := connect()
	bravo.Login("Bravo")

	alice.Configure()
	alice.EnterWorld("Alice")

	bravo.Configure()
	servertest.ExpectDisconnect(t, bravo, protocol.Text{Translate: "multiplayer.disconnect.server_full"})

	if got := srv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want 1", got)
	}

	alice.Disconnect()
}

// TestServerFullBypassAtConfiguration checks the ops.json bypassesPlayerLimit
// flag when the last slot is only taken after both logins: the bystanding
// operator is let past the capacity check that runs as they enter the world.
func TestServerFullBypassAtConfiguration(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.MaxPlayers = 1

	level := config.Default().OpPermissionLevel
	aliceOp := servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: level}
	bravoOp := servertest.OpRecord{UUID: protocol.OfflineUUID("Bravo").String(), Name: "Bravo", Level: level, BypassesPlayerLimit: true}
	srv, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t, aliceOp, bravoOp))

	// Both logins see an empty server, so neither is refused at login.
	alice := connect()
	alice.Login("Alice")
	bravo := connect()
	bravo.Login("Bravo")

	alice.Configure()
	alice.EnterWorld("Alice")

	bravo.Configure()
	bravo.EnterWorld("Bravo")

	if got := srv.PlayerCount(); got != 2 {
		t.Errorf("player count = %d, want 2 with the bypassing operator", got)
	}

	alice.Disconnect()
	bravo.Disconnect()
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

// TestServerFullRefusesOperatorWithoutBypass checks the other half of the
// bypass: an operator whose ops.json entry does not carry bypassesPlayerLimit
// is refused at login exactly like a non-operator.
func TestServerFullRefusesOperatorWithoutBypass(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.MaxPlayers = 1

	level := config.Default().OpPermissionLevel
	aliceOp := servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: level}
	bravoOp := servertest.OpRecord{UUID: protocol.OfflineUUID("Bravo").String(), Name: "Bravo", Level: level}
	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t, aliceOp, bravoOp))

	alice := connect().Join("Alice")

	refused := connect()
	expectLoginRefusal(t, refused, "Bravo", `{"translate":"multiplayer.disconnect.server_full"}`)
	refused.Disconnect()

	alice.Disconnect()
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

// TestBanOfflineProfile bans a known profile that is not connected. There is
// no client to disconnect, which must not read as one.
func TestBanOfflineProfile(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	listed := servertest.WhitelistRecord{UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob"}
	srv, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, []servertest.WhitelistRecord{listed}))

	alice.ChatCommand("ban Bob Offline")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Bob"}, protocol.Text{Text: "Offline"})

	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 || records[0].Name != "Bob" || records[0].Reason != "Offline" {
		t.Errorf("banned-players.json = %+v, want Bob banned offline", records)
	}
	alice.Disconnect()
}

// TestUnknownNamesResolveOffline bans, whitelists and pardons names that have
// never joined. Offline mode resolves each to an offline-UUID profile, so a
// player can be listed before their first join; a pardon still fails because
// nobody by that name is banned.
func TestUnknownNamesResolveOffline(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("ban Ghost testing")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Ghost"}, protocol.Text{Text: "testing"})
	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 || records[0].UUID != protocol.OfflineUUID("Ghost").String() || records[0].Name != "Ghost" {
		t.Errorf("banned-players.json = %+v, want Ghost under the offline UUID", records)
	}

	alice.ChatCommand("whitelist add Newbie")
	alice.ExpectCommandFeedbackText("commands.whitelist.add.success", protocol.Text{Text: "Newbie"})
	listed := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile())
	if len(listed) != 1 || listed[0].UUID != protocol.OfflineUUID("Newbie").String() {
		t.Errorf("whitelist.json = %+v, want Newbie under the offline UUID", listed)
	}

	alice.ChatCommand("pardon Nobody")
	servertest.AwaitCommandFailure(t, alice, "commands.pardon.failed")

	alice.Disconnect()
}

// TestWhitelistSettingPersists writes the white-list flag back to
// server.properties, so /whitelist on and off survive a restart.
func TestWhitelistSettingPersists(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("whitelist on")
	alice.ExpectCommandFeedback("commands.whitelist.enabled")
	if loaded, err := config.Load(srv.ConfigFile()); err != nil {
		t.Fatalf("reload server.properties: %v", err)
	} else if !loaded.WhiteList {
		t.Errorf("white-list = false after /whitelist on, want it written true")
	}

	alice.ChatCommand("whitelist off")
	alice.ExpectCommandFeedback("commands.whitelist.disabled")
	if loaded, err := config.Load(srv.ConfigFile()); err != nil {
		t.Fatalf("reload server.properties: %v", err)
	} else if loaded.WhiteList {
		t.Errorf("white-list = true after /whitelist off, want it written false")
	}

	alice.Disconnect()
}

// TestWhitelistMultipleTargets adds and removes a selector that names one
// player already in the wanted state and one not: the one already there is
// skipped, the other changed, and the command does not fail part-way.
func TestWhitelistMultipleTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, _ := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("whitelist add Bob")
	alice.ExpectCommandFeedbackText("commands.whitelist.add.success", protocol.Text{Text: "Bob"})

	// @a names Alice first: she is added, Bob is skipped.
	alice.ChatCommand("whitelist add @a")
	alice.ExpectCommandFeedbackText("commands.whitelist.add.success", protocol.Text{Text: "Alice"})

	// Both are listed now, so there is nobody left to add.
	alice.ChatCommand("whitelist add @a")
	servertest.AwaitCommandFailure(t, alice, "commands.whitelist.add.failed")

	// Alice comes off by name, leaving Bob; removing @a skips Alice and
	// removes Bob.
	alice.ChatCommand("whitelist remove Alice")
	alice.ExpectCommandFeedbackText("commands.whitelist.remove.success", protocol.Text{Text: "Alice"})
	alice.ChatCommand("whitelist remove @a")
	alice.ExpectCommandFeedbackText("commands.whitelist.remove.success", protocol.Text{Text: "Bob"})

	// Nobody is left to remove.
	alice.ChatCommand("whitelist remove @a")
	servertest.AwaitCommandFailure(t, alice, "commands.whitelist.remove.failed")

	alice.Disconnect()
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

// TestPardonMultipleTargets pardons a selector naming an unbanned player and a
// banned one: the unbanned player is skipped, the banned one pardoned, and the
// command does not fail part-way.
func TestPardonMultipleTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	wally := connect().Join("Wally")

	xena := connect().Join("Xena")

	// Xena is banned, Wally is not: @a names Wally first, who is skipped, so
	// Xena is pardoned and the command answers one success.
	path := filepath.Join(t.TempDir(), "banned-players.json")
	servertest.WriteJSONList(t, path, []servertest.BanRecord{{
		UUID: protocol.OfflineUUID("Xena").String(), Name: "Xena",
		Created: "2026-01-01 00:00:00 +0000", Source: "Tester", Expires: "forever",
	}})
	srv.SetBannedPlayersFile(path)

	reply := srv.RunLine("pardon @a")
	if len(reply) != 1 || reply[0].Translate != "commands.pardon.success" ||
		len(reply[0].With) != 1 || reply[0].With[0].Text != "Xena" {
		t.Fatalf("pardon @a answered %v, want one success for Xena", reply)
	}
	if records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile()); len(records) != 0 {
		t.Errorf("banned-players.json = %+v, want it emptied", records)
	}
	wally.Disconnect()
	xena.Disconnect()
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

// TestBanIPExpiryRefusal appends the lapse date to a login refused by a dated
// address ban, which carries the address-ban expiry key.
func TestBanIPExpiryRefusal(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	banned := servertest.IpBanRecord{
		IP: "127.0.0.1", Created: "2026-01-01 00:00:00 +0000", Source: "Tester",
		Expires: "2027-01-01 00:00:00 +0000", Reason: "Bad",
	}
	_, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, []servertest.IpBanRecord{banned}, nil))

	c := connect()
	expectLoginRefusal(t, c, "Bob",
		`{"extra":[{"translate":"multiplayer.disconnect.banned_ip.expiration","with":["2027-01-01 at 00:00:00 UTC"]}],"translate":"multiplayer.disconnect.banned_ip.reason","with":["Bad"]}`)
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

// TestBanWhileConfiguringRefusesJoin bans a player who passed login and is
// still in configuration: finishing configuration disconnects them with the
// ban message instead of spawning them.
// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished
func TestBanWhileConfiguringRefusesJoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	bob := connect()
	bob.Login("Bob")

	alice.ChatCommand("ban Bob")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Bob"}, protocol.Text{Translate: "multiplayer.disconnect.banned.reason.default"})

	bob.Configure()
	servertest.ExpectDisconnect(t, bob, protocol.Text{
		Translate: "multiplayer.disconnect.banned.reason",
		With:      []protocol.Text{{Translate: "multiplayer.disconnect.banned.reason.default"}},
	})
	if got := srv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want 1", got)
	}
	alice.Disconnect()
}

// TestWhitelistRemovalWhileConfiguringRefusesJoin removes a configuring player
// from the white-list before they finish configuration.
func TestWhitelistRemovalWhileConfiguringRefusesJoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.WhiteList = true
	listed := servertest.WhitelistRecord{UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob"}
	srv, connect, alice := servertest.AdminSolo(t, cfg, servertest.WriteAdminFiles(t, nil, nil, []servertest.WhitelistRecord{listed}))

	bob := connect()
	bob.Login("Bob")

	alice.ChatCommand("whitelist remove Bob")
	alice.ExpectCommandFeedbackText("commands.whitelist.remove.success", protocol.Text{Text: "Bob"})

	bob.Configure()
	servertest.ExpectDisconnect(t, bob, protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"})
	if got := srv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want 1", got)
	}
	alice.Disconnect()
}

// TestWhitelistAddLooksUpANeverSeenNameInOnlineMode runs /whitelist add from
// the console on an online-mode server for a name nobody has joined under:
// the name is looked up with Mojang, whitelisted under the answer's UUID and
// name and cached; a name Mojang does not know is the player-unknown error.
// vanilla: CachedUserNameToIdResolver.lookupGameProfile
func TestWhitelistAddLooksUpANeverSeenNameInOnlineMode(t *testing.T) {
	var paths []string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		if !strings.EqualFold(r.URL.Path, "/Notch") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"069a79f444e94726a5befca90e38aaf5","name":"Notch"}`))
	}))
	t.Cleanup(stub.Close)

	cfg := config.Default()
	cfg.OnlineMode = true
	srv, _ := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil), usercacheAt(t))
	srv.SetProfileLookupServer(stub.URL)

	reply := srv.RunLine("whitelist add notch")
	if len(reply) != 1 || reply[0].Translate != "commands.whitelist.add.success" {
		t.Fatalf("whitelist add answered %v, want commands.whitelist.add.success", reply)
	}
	const wantID = "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	if got := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile()); len(got) != 1 || got[0].UUID != wantID || got[0].Name != "Notch" {
		t.Errorf("whitelist.json = %+v, want Notch %s", got, wantID)
	}
	cached := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.UsercacheFile())
	if len(cached) != 1 || cached[0].UUID != wantID || cached[0].Name != "Notch" {
		t.Errorf("usercache.json = %+v, want Notch %s", cached, wantID)
	}

	reply = srv.RunLine("whitelist add a?b#c")
	if len(reply) != 1 || len(reply[0].Extra) == 0 || reply[0].Extra[0].Translate != "argument.player.unknown" {
		t.Errorf("odd name answered %v, want argument.player.unknown", reply)
	}
	if want := []string{"/notch", "/a%3Fb%23c"}; len(paths) != 2 || !strings.EqualFold(paths[0], want[0]) || paths[1] != want[1] {
		t.Errorf("lookup paths = %q, want %q", paths, want)
	}

	reply = srv.RunLine("whitelist add Nobody_xyz")
	if len(reply) != 1 || len(reply[0].Extra) == 0 || reply[0].Extra[0].Translate != "argument.player.unknown" {
		t.Errorf("unknown name answered %v, want argument.player.unknown", reply)
	}
}
