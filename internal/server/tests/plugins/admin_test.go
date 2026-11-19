package plugins_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

func pluginAdminServer(t *testing.T) (*server.Server, plugin.Server) {
	t.Helper()
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "admin", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}}, servertest.WriteAdminFiles(t, nil, nil, nil), servertest.WriteOpsFile(t))
	return srv, host
}

func pluginJoin(t *testing.T, srv *server.Server, name string) *servertest.Client {
	t.Helper()
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	return c.Join(name)
}

func pluginLoginRefusal(t *testing.T, srv *server.Server, name, contains string) {
	t.Helper()
	c := servertest.DialServer(t, srv)
	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) { b.PutString(name); b.PutUUID(uuid.Nil) })
	got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767)
	if !strings.Contains(got, contains) {
		t.Fatalf("login refusal = %s, want %s", got, contains)
	}
	c.Disconnect()
}

// vanilla: BanPlayerCommands.banPlayers, BanIpCommands.banIp, PlayerList.canPlayerLogin
func TestPluginBans(t *testing.T) {
	srv, host := pluginAdminServer(t)
	c := pluginJoin(t, srv, "Banned")
	id := protocol.OfflineUUID("Banned").String()
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	host.Bans().Ban(id, "plugin reason", expires, "plugin source")
	// Ban only records: the plugin kicks with its own message.
	p, _ := host.PlayerNamed("Banned")
	p.Kick(plugin.Plain("bye"))
	if got := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Contains(got, []byte("bye")) {
		t.Fatalf("disconnect = % X, want the plugin message", got)
	}
	e, ok := host.Bans().Entry(id)
	if !ok || e.Target != id || e.Reason != "plugin reason" || e.Source != "plugin source" || !e.Expires.Equal(expires) || e.Created.IsZero() || len(host.Bans().Entries()) != 1 {
		t.Fatalf("ban = %+v, %v", e, ok)
	}
	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 || records[0].Name != "Banned" || records[0].Source != e.Source || records[0].Reason != e.Reason {
		t.Fatalf("ban file = %+v", records)
	}
	pluginLoginRefusal(t, srv, "Banned", "multiplayer.disconnect.banned.reason")
	host.Bans().Pardon(id)
	if _, ok := host.Bans().Entry(id); ok {
		t.Fatal("pardon retained ban")
	}
	c = pluginJoin(t, srv, "Banned")
	host.Bans().Ban(protocol.OfflineUUID("Expired").String(), "expired", time.Now().Add(-time.Hour), "source")
	if _, ok := host.Bans().Entry(protocol.OfflineUUID("Expired").String()); ok || len(host.Bans().Entries()) != 0 {
		t.Fatal("expired ban remains live")
	}
	host.IPBans().Ban("127.0.0.1", "address reason", time.Time{}, "address source")
	p, _ = host.PlayerNamed("Banned")
	p.Kick(plugin.Plain("bye again"))
	if got := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Contains(got, []byte("bye again")) {
		t.Fatalf("disconnect = % X, want the plugin message", got)
	}
	ip, ok := host.IPBans().Entry("127.0.0.1")
	ips := servertest.ReadListFile[servertest.IpBanRecord](t, srv.BannedIPsFile())
	if !ok || !ip.Expires.IsZero() || len(ips) != 1 || ips[0].Expires != "forever" || ips[0].Reason != ip.Reason || len(host.IPBans().Entries()) != 1 {
		t.Fatalf("IP ban = %+v, file %+v", ip, ips)
	}
	pluginLoginRefusal(t, srv, "Other", "multiplayer.disconnect.banned_ip.reason")
	host.IPBans().Pardon("127.0.0.1")
	if _, ok := host.IPBans().Entry("127.0.0.1"); ok {
		t.Fatal("IP pardon retained ban")
	}
	pluginJoin(t, srv, "Other")
}

// vanilla: WhitelistCommand, MinecraftServer.kickUnlistedPlayers, PlayerList.op/deop
func TestPluginWhitelistAndOps(t *testing.T) {
	srv, host := pluginAdminServer(t)
	c := pluginJoin(t, srv, "Listed")
	p, _ := host.PlayerNamed("Listed")
	id := p.UUID()
	host.Ops().Set(id, p.Name(), 3, true)
	c.ExpectPlay(protocol.IDPlayCommands)
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayEntityEvent))
	if r.ReadI32() != c.PlayerEntityID || r.ReadU8() != 27 || r.Err() != nil || p.OpLevel() != 3 || host.Ops().Level(id) != 3 {
		t.Fatal("operator level was not refreshed")
	}
	ops := servertest.ReadListFile[servertest.OpRecord](t, srv.OpsFile())
	if len(ops) != 1 || ops[0].Level != 3 || !ops[0].BypassesPlayerLimit {
		t.Fatalf("ops file = %+v", ops)
	}
	host.Whitelist().Add(id, p.Name())
	if !host.Whitelist().Contains(id) {
		t.Fatal("whitelist add")
	}
	host.Whitelist().SetEnabled(true)
	if !host.Whitelist().Enabled() {
		t.Fatal("whitelist enable")
	}
	host.Whitelist().SetEnforced(true)
	if loaded, err := config.Load(srv.ConfigFile()); err != nil || !loaded.WhiteList || !loaded.EnforceWhitelist {
		t.Fatalf("whitelist settings not persisted: %+v, %v", loaded, err)
	}
	if !host.Whitelist().Enforced() {
		t.Fatal("whitelist enforcement")
	}
	pluginLoginRefusal(t, srv, "Unlisted", "multiplayer.disconnect.not_whitelisted")
	host.Ops().Set(id, p.Name(), 0, false)
	c.ExpectPlay(protocol.IDPlayCommands)
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlayEntityEvent))
	if r.ReadI32() != c.PlayerEntityID || r.ReadU8() != 24 || r.Err() != nil || p.OpLevel() != 0 || host.Ops().Level(id) != 0 {
		t.Fatal("deop level was not refreshed")
	}
	host.Whitelist().Remove(id)
	servertest.ExpectDisconnect(t, c, protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"})
	if host.Whitelist().Contains(id) || len(servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile())) != 0 {
		t.Fatal("whitelist remove did not persist")
	}
	host.Whitelist().SetEnabled(false)
	pluginJoin(t, srv, "Unlisted")
	host.SetMaxPlayers(1)
	bypass := protocol.OfflineUUID("Bypass")
	host.Ops().Set(bypass, "Bypass", 2, true)
	pluginJoin(t, srv, "Bypass")
	host.Ops().Set(protocol.OfflineUUID("NoBypass"), "NoBypass", 2, false)
	pluginLoginRefusal(t, srv, "NoBypass", "multiplayer.disconnect.server_full")
}

// vanilla: ServerAdvancementManager.get, AdvancementNode.parent
func TestPluginAdvancementLookup(t *testing.T) {
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "lookup", "example/advancement/custom.json", `{"parent":"minecraft:story/root","display":{"icon":{"id":"minecraft:stone"},"title":{"text":"Custom title"},"description":{"text":"Custom description"}},"criteria":{"custom":{"trigger":"minecraft:impossible"}}}`)
	servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "lookup", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	custom, ok := host.Advancement(plugin.Key{Namespace: "example", Path: "custom"})
	if !ok || custom.Title().PlainString() != "Custom title" || custom.Description().PlainString() != "Custom description" || len(custom.Criteria()) != 1 || custom.Criteria()[0] != "custom" {
		t.Fatal("data pack advancement lookup")
	}
	hidden, ok := host.Advancement(plugin.MinecraftKey("nether/all_effects"))
	if !ok || hidden.Title().PlainString() != "" || hidden.Description().PlainString() != "" {
		t.Fatal("hidden advancement display")
	}
	a, ok := host.Advancement(plugin.MinecraftKey("story/mine_stone"))
	if !ok || a.Key().String() != "minecraft:story/mine_stone" || a.Title().Translate != "advancements.story.mine_stone.title" || a.Description().Translate != "advancements.story.mine_stone.description" || len(a.Criteria()) == 0 {
		t.Fatalf("advancement = %+v, %v", a, ok)
	}
	parent, ok := a.Parent()
	if !ok || parent.Key().String() != "minecraft:story/root" {
		t.Fatal("advancement parent")
	}
	if _, ok := parent.Parent(); ok {
		t.Fatal("root has a parent")
	}
	criteria := a.Criteria()
	criteria[0] = "mutated"
	if a.Criteria()[0] == "mutated" {
		t.Fatal("criteria alias server state")
	}
	if a, ok := host.Advancement(plugin.Key{Namespace: "test", Path: "absent"}); ok || a != nil {
		t.Fatal("unknown advancement")
	}
}

// vanilla: DeOpCommands.deopPlayers, MinecraftServer.kickUnlistedPlayers
func TestPluginDeopEnforcesWhitelist(t *testing.T) {
	srv, host := pluginAdminServer(t)
	id := protocol.OfflineUUID("Operator")
	host.Ops().Set(id, "Operator", 3, false)
	host.Whitelist().SetEnabled(true)
	host.Whitelist().SetEnforced(true)
	// Operators bypass the login whitelist, but not the enforcement kick.
	c := pluginJoin(t, srv, "Operator")
	host.Ops().Set(id, "Operator", 0, false)
	c.ExpectPlay(protocol.IDPlayCommands)
	c.ExpectPlay(protocol.IDPlayEntityEvent)
	servertest.ExpectDisconnect(t, c, protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"})
}

// A name that never joined is banned by name and takes its UUID at login.
// vanilla: PlayerList.canPlayerLogin
func TestPluginBanByName(t *testing.T) {
	srv, host := pluginAdminServer(t)
	host.Bans().Ban("Ghost", "by name", time.Time{}, "src")
	e, ok := host.Bans().Entry("gHOST")
	if !ok || e.Name != "Ghost" || e.Reason != "by name" {
		t.Fatalf("entry = %+v, %v", e, ok)
	}
	pluginLoginRefusal(t, srv, "gHost", "multiplayer.disconnect.banned.reason")
	e, ok = host.Bans().Entry(protocol.OfflineUUID("gHost").String())
	if !ok || e.Name != "gHost" || len(host.Bans().Entries()) != 1 {
		t.Fatalf("bound entry = %+v, %v", e, ok)
	}
	host.Bans().Ban("Other", "r", time.Time{}, "src")
	host.Bans().Pardon("other")
	if _, ok := host.Bans().Entry("Other"); ok {
		t.Fatal("pardon by name retained ban")
	}
}
