package plugins_test

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

func TestPluginPermissionDefaults(t *testing.T) {
	var h plugin.Server
	ready := false
	servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "permissions", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		s.Permissions().Register(plugin.Permission{Name: "public", Default: plugin.PermissionDefault{OpLevel: 0}})
		s.Permissions().Register(plugin.Permission{Name: "private", Default: plugin.PermissionDefault{OpLevel: 5}})
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("permission registration did not complete")
	}
	if !h.Console().HasPermission("public") || h.Console().HasPermission("private") {
		t.Fatal("console did not use permission defaults")
	}
}

func TestPluginConsoleCommandEvent(t *testing.T) {
	seen := false
	var listener plugin.Listener
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "commands", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		listener = plugin.Listen(s, func(e *plugin.ServerCommandEvent) {
			seen = true
			s.Players()
			if e.Command == "cancel" {
				e.SetCancelled(true)
			} else {
				e.Command = "list"
			}
		})
		return nil
	}}}})
	out := srv.RunLine("unregistered")
	if !seen || len(out) != 1 || out[0].Translate != "commands.list.players" {
		t.Fatalf("console event did not rewrite command: seen=%v out=%v", seen, out)
	}
	if out := srv.RunLine("cancel"); len(out) != 0 {
		t.Error("cancelled console command produced feedback")
	}
	listener.Unregister()
	seen = false
	srv.RunLine("list")
	if seen {
		t.Error("removed console listener ran")
	}
}

type permissionProvider func(plugin.CommandSender, string) (bool, bool)

func (p permissionProvider) Has(s plugin.CommandSender, n string) (bool, bool) { return p(s, n) }

func TestPluginPermissionProviderAndChildren(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var h plugin.Server
	var records []servertest.OpRecord
	for level := 1; level <= 4; level++ {
		name := fmt.Sprint("Level", level)
		records = append(records, servertest.OpRecord{UUID: protocol.OfflineUUID(name).String(), Name: name, Level: level})
	}
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "permissions", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		for level := 0; level <= 5; level++ {
			s.Permissions().Register(plugin.Permission{Name: fmt.Sprint("level", level), Default: plugin.PermissionDefault{OpLevel: level}})
		}
		children := map[string]bool{"grant": true, "deny": false, "nested": true}
		s.Permissions().Register(plugin.Permission{Name: "parent", Children: children})
		children["grant"] = false
		s.Permissions().Register(plugin.Permission{Name: "nested", Default: plugin.PermissionDefault{OpLevel: 5}, Children: map[string]bool{"leaf": true}})
		s.Permissions().Register(plugin.Permission{Name: "cycleA", Default: plugin.PermissionDefault{OpLevel: 5}, Children: map[string]bool{"cycleB": true}})
		s.Permissions().Register(plugin.Permission{Name: "cycleB", Default: plugin.PermissionDefault{OpLevel: 5}, Children: map[string]bool{"cycleA": true}})
		return nil
	}}}}, servertest.WriteOpsFile(t, records...))
	servertest.FreezeUntilHeld(t, srv)
	var players []plugin.Player
	for level := 0; level <= 4; level++ {
		c := servertest.DialServer(t, srv)
		c.ViewAsk = config.MinDistance
		c.Join(fmt.Sprint("Level", level))
		p, _ := h.PlayerNamed(fmt.Sprint("Level", level))
		players = append(players, p)
	}
	servertest.HoldTicks(t, srv)
	run := func(f func()) {
		h.Scheduler().RunTask(f)
		servertest.RunTicks(t, srv, 1)
	}
	run(func() {
		for level, p := range players {
			for required := 0; required <= 5; required++ {
				if got := p.HasPermission(fmt.Sprint("level", required)); got != (level >= required) {
					t.Errorf("level %d required %d = %v", level, required, got)
				}
			}
			if p.HasPermission("unknown") != (level > 0) || !p.HasPermission("grant") || p.HasPermission("deny") || !p.HasPermission("leaf") || p.HasPermission("cycleA") {
				t.Errorf("inheritance for level %d", level)
			}
		}
		p, ok := h.Permissions().Get("parent")
		if !ok || !p.Children["grant"] {
			t.Error("registration retained caller's children")
		}
		p.Children["grant"] = false
		if !players[0].HasPermission("grant") {
			t.Error("Get returned mutable children")
		}
		if _, ok := h.Permissions().Get("missing"); ok {
			t.Error("unknown permission exists")
		}
		plugin.Provide[plugin.PermissionProvider](h, permissionProvider(func(sender plugin.CommandSender, node string) (bool, bool) {
			// Re-enter both registries: provider callbacks must hold neither lock.
			h.Permissions().Get(node)
			plugin.Service[plugin.PermissionProvider](h)
			switch node {
			case "level5":
				return sender.Name() == "Level0", true
			case "level0":
				return false, true
			default:
				return false, false
			}
		}), 1)
		if !players[0].HasPermission("level5") || players[4].HasPermission("level5") || players[0].HasPermission("level0") || !players[4].HasPermission("level4") || h.Console().HasPermission("level0") {
			t.Error("provider decisions or fallback")
		}
		if !players[0].PermissionSet("level5") || !players[0].PermissionSet("level0") || players[0].PermissionSet("level1") || !h.Console().PermissionSet("level0") {
			t.Error("PermissionSet does not follow the provider's decisions")
		}
	})
}

func TestPluginPermissionCleanup(t *testing.T) {
	var observer, failed plugin.Server
	ready := false
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{
		{Name: "observer", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			observer = s
			s.Permissions().Register(plugin.Permission{Name: "restored", Default: plugin.PermissionDefault{OpLevel: 5}})
			s.Permissions().Register(plugin.Permission{Name: "shared", Children: map[string]bool{"restored": true}})
			return nil
		}}},
		{Name: "failed", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			failed = s
			s.Permissions().Register(plugin.Permission{Name: "shared", Default: plugin.PermissionDefault{OpLevel: 5}})
			s.Permissions().Register(plugin.Permission{Name: "failed"})
			ready = true
			return errors.New("failed enable")
		}}},
	})
	servertest.HoldTicks(t, srv)
	if !ready {
		t.Fatal("permission registration failed")
	}
	if _, ok := observer.Permissions().Get("failed"); ok {
		t.Error("failed plugin's permission survived")
	}
	if p, ok := observer.Permissions().Get("shared"); !ok || p.Default.OpLevel != 0 {
		t.Error("cleanup did not reveal previous registration")
	}
	if !observer.Console().HasPermission("restored") {
		t.Error("cleanup did not restore previous dependency edges")
	}
	failed.Permissions().Register(plugin.Permission{Name: "late"})
	if _, ok := observer.Permissions().Get("late"); ok {
		t.Error("disabled handle registered permission")
	}
	srv.Shutdown()
	if _, ok := observer.Permissions().Get("shared"); ok {
		t.Error("permission survived disable")
	}
}

// vanilla: Commands.sendCommands, Commands.fillUsableCommands
func TestPluginInactiveCommandTree(t *testing.T) {
	// SHA-256 of this server's join Commands payloads at operator levels 0–4.
	// Levels 2–4 include rotation before facing (vanilla: TeleportCommand.register);
	// CommandNode.getChildren and ClientboundCommandsPacket.createEntry preserve that order.
	// Root and /dialog, /worldborder children follow vanilla's dispatcher registration order.
	// Bukkit's /help and /? replace vanilla's /help in both trees, and a loaded
	// plugin adds the minecraft:<name> aliases, so the two trees differ.
	want := map[bool][]string{
		false: {
			"bf1e99e39280f0ef647ffa44720fb40295ea62b26aea5b43eb82e8b14aa36218",
			"336e030975c07ebaa0df4925ae45effaa15e0d9d4da456dfd5b3ecfd77e430d9",
			"bd65ba6784c1ba9cc9e2415635047c9d2dc8f4b6130e12dfec0312ae7702317c",
			"ceded90a119c1ef5fc44233eee00845eeae8a1e7013a458921153fbcbd25f163",
			"4ebb249a8e69bb048fa0b87f7b2f2a6ec982eff58e48e1a986c4625a7e40208b",
		},
		true: {
			"61c3c2b35342ae1d64ddabd172f73c0d68beecc48d2209619aec925fe386d1fd",
			"94238bc46fede22267f03da67d55c45de23240b8682fd6545943ce2e7d647755",
			"bd92a31a80b015e09b44fd1c6a40fe4958752e4f488658758124c1bc318f5a3e",
			"ebc554bef660cfe0e2ce8ee7cb701f6fd51e7a23d5ac46c9bbc6f7cf58ab188e",
			"060a1a9045950ec0706fa1216d110a42ca1b73cb84f2a43e98b28daced00cecb",
		},
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			var plugins []plugin.Registration
			if enabled {
				plugins = []plugin.Registration{{Name: "inactive", Plugin: &testPlugin{}}}
			}
			var ops []servertest.OpRecord
			for level := 1; level <= 4; level++ {
				name := fmt.Sprint("Level", level)
				ops = append(ops, servertest.OpRecord{UUID: protocol.OfflineUUID(name).String(), Name: name, Level: level})
			}
			srv := servertest.NewTestServerWithPlugins(t, cfg, plugins, servertest.WriteOpsFile(t, ops...))
			servertest.FreezeUntilHeld(t, srv)
			for level := 0; level <= 4; level++ {
				data := joinCommandTree(t, srv, fmt.Sprint("Level", level))
				if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want[enabled][level] {
					t.Errorf("level %d tree hash = %s", level, got)
				}
			}
		})
	}
}

func TestPluginCommandTreePlayerIdentity(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var host plugin.Server
	type identity struct {
		initial plugin.Player
		trees   int
		inputs  int
	}
	identities := make([]identity, 2)
	var registrations []plugin.Registration
	for i := range identities {
		state := &identities[i]
		stored := make(map[plugin.Player]bool)
		registrations = append(registrations, plugin.Registration{Name: fmt.Sprint("identity", i), Plugin: &testPlugin{enable: func(s plugin.Server) error {
			host = s
			plugin.Listen(s, func(e *plugin.PlayerCommandSendEvent) {
				state.trees++
				p, ok := s.PlayerNamed("Identity")
				if !ok || !p.IsValid() || !e.Player.IsValid() {
					t.Error("tree event or lookup player is invalid")
				}
				if state.trees == 1 {
					state.initial = e.Player
					stored[p] = true
				}
				byID, found := s.Player(e.Player.UUID())
				players := s.Players()
				online, isOnline := e.Player.Online()
				if e.Player != state.initial || e.Player != p || !found || byID != e.Player || len(players) != 1 || players[0] != e.Player || !isOnline || online != e.Player {
					t.Errorf("plugin %d tree event changed player handle identity", i)
				}
				if i == 1 && stored[e.Player] {
					e.Hide = []string{"list"}
				}
			})
			plugin.Listen(s, func(e *plugin.PlayerCommandEvent) {
				state.inputs++
				if e.Player != state.initial || !stored[e.Player] {
					t.Errorf("plugin %d command input changed player handle identity", i)
				}
			})
			return nil
		}}})
	}
	srv := servertest.NewTestServerWithPlugins(t, cfg, registrations, servertest.WriteOpsFile(t))
	checkTree := func(data []byte) {
		t.Helper()
		nodes := servertest.DecodeCommands(t, data)
		for _, child := range nodes[0].Children {
			if nodes[child].Name == "list" {
				t.Error("tree ignored hiding keyed by a stored player handle")
			}
		}
	}
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Login("Identity")
	c.Configure()
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayCommands {
			checkTree(data)
		}
	})
	c.MarkLoaded()
	servertest.HoldTicks(t, srv)
	for i, state := range identities {
		if state.trees != 1 {
			t.Fatalf("plugin %d initial tree events = %d, want 1", i, state.trees)
		}
	}
	for _, line := range []string{"op Identity", "deop Identity"} {
		host.Scheduler().RunTask(func() {
			if _, err := host.Dispatch(host.Console(), line); err != nil {
				t.Error(err)
			}
		})
		checkTree(c.ExpectWithinTicks(srv, 4, protocol.IDPlayCommands))
		c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisconnect))
	}
	servertest.ReleaseTicks(t, srv)
	c.ChatCommand("list")
	checkPluginList(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "1", "Identity")
	servertest.HoldTicks(t, srv)
	for i, state := range identities {
		if state.trees != 3 || state.inputs != 1 {
			t.Errorf("plugin %d events = %d trees, %d inputs; want 3, 1", i, state.trees, state.inputs)
		}
	}
	c.Disconnect()
	for _, state := range identities {
		if state.initial.IsValid() {
			t.Error("initial tree handle remained valid after disconnect")
		}
	}
}

func joinCommandTree(t *testing.T, srv *server.Server, name string) []byte {
	t.Helper()
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Login(name)
	c.Configure()
	var data []byte
	c.ReadJoin(func(id int32, payload []byte) {
		if id == protocol.IDPlayCommands {
			data = slices.Clone(payload)
		}
	})
	c.MarkLoaded()
	if len(data) == 0 {
		t.Fatal("no command tree")
	}
	return data
}

type recordingSender struct {
	level    int
	messages []plugin.Text
}

func (*recordingSender) Name() string                { return "Custom" }
func (s *recordingSender) OpLevel() int              { return s.level }
func (*recordingSender) HasPermission(string) bool   { return false }
func (*recordingSender) PermissionSet(string) bool   { return false }
func (s *recordingSender) SendMessage(t plugin.Text) { s.messages = append(s.messages, t) }

func TestPluginDispatch(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var h plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "dispatch", Plugin: &testPlugin{enable: func(s plugin.Server) error { h = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Runner")
	servertest.HoldTicks(t, srv)
	h.Scheduler().RunTask(func() {
		if h.Console().Name() != "Server" || h.Console().OpLevel() != 4 {
			t.Error("console identity")
		}
		if n, err := h.Dispatch(h.Console(), "list"); n != 1 || err != nil {
			t.Errorf("console result = %d, %v", n, err)
		}
		p, _ := h.PlayerNamed("Runner")
		if n, err := h.Dispatch(p, "list"); n != 1 || err != nil {
			t.Errorf("player result = %d, %v", n, err)
		}
		if !p.RunCommand("list") || p.RunCommand("unknown") {
			t.Error("RunCommand result")
		}
		sender := &recordingSender{}
		if n, err := h.Dispatch(sender, "seed"); n != 0 || err == nil {
			t.Error("custom sender bypassed op level")
		}
		if len(sender.messages) != 2 || sender.messages[0].PlainString() == "" {
			t.Error("custom sender parse failure")
		}
		sender.level, sender.messages = 4, nil
		if n, err := h.Dispatch(sender, "execute run list"); n != 1 || err != nil || len(sender.messages) != 1 {
			t.Errorf("custom sender nested dispatch = %d, %v, %v", n, err, sender.messages)
		}
		if _, err := h.Dispatch(nil, "list"); err == nil {
			t.Error("nil sender accepted")
		}
	})
	servertest.RunTicks(t, srv, 1)
	checkPluginList(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "1", "Runner")
}

func currentGoroutine() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	return strings.Fields(string(buf[:n]))[1]
}

func TestPluginPlayerCommandEvents(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var h plugin.Server
	var tickGoroutine string
	var commandTicks []int64
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "events", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		plugin.Listen(s, func(*plugin.ServerTickStartEvent) { tickGoroutine = currentGoroutine() })
		plugin.Listen(s, func(e *plugin.PlayerCommandSendEvent) {
			if !e.Player.IsValid() || e.Player.Name() == "" {
				t.Error("tree event has invalid joining player")
			}
			s.Players()
			e.Hide = []string{"list", "msg"}
		})
		plugin.Listen(s, func(e *plugin.PlayerCommandEvent) {
			if tickGoroutine != currentGoroutine() {
				t.Error("command event outside tick goroutine")
			}
			commandTicks = append(commandTicks, s.Ticks().Current())
			if e.Command == "cancel" {
				e.SetCancelled(true)
				return
			}
			if e.Command == "rewrite" {
				e.Command = "list"
			}
			if e.Command == "dispatch" {
				s.Dispatch(e.Player, "list")
				e.SetCancelled(true)
			}
		})
		return nil
	}}}}, servertest.WriteOpsFile(t))
	data := joinCommandTree(t, srv, "Hidden")
	nodes := servertest.DecodeCommands(t, data)
	for _, child := range nodes[0].Children {
		if nodes[child].Name == "list" || nodes[child].Name == "msg" {
			t.Error("hidden root was sent")
		}
	}
	for _, node := range nodes {
		if node.Name == "w" && (node.Redirect < 0 || nodes[node.Redirect].Name != "msg" || len(nodes[node.Redirect].Children) == 0) {
			t.Error("hiding a root broke a retained alias")
		}
	}
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Actor")
	for _, line := range []string{"list", "rewrite", "dispatch"} {
		c.ChatCommand(line)
		checkPluginList(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "2", "Hidden, Actor")
	}
	c.ChatCommand("cancel")
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat))
	servertest.HoldTicks(t, srv)
	if len(commandTicks) != 4 {
		t.Errorf("command events = %v", commandTicks)
	}
	// The hide list applies on permission changes, and a listener may dispatch without another input event.
	h.Scheduler().RunTask(func() { h.Dispatch(h.Console(), "op Actor") })
	nodes = servertest.DecodeCommands(t, c.ExpectWithinTicks(srv, 4, protocol.IDPlayCommands))
	for _, child := range nodes[0].Children {
		if nodes[child].Name == "list" {
			t.Error("resend ignored hide list")
		}
	}
}

// vanilla: RconClient.run, RconConsoleSource
func TestPluginRemoteCommandEvent(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.RconPassword = "secret"
	var tickGoroutine string
	var seen []string
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "remote", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(*plugin.ServerTickStartEvent) { tickGoroutine = currentGoroutine() })
		plugin.Listen(s, func(e *plugin.RemoteServerCommandEvent) {
			if tickGoroutine != currentGoroutine() {
				t.Error("remote event outside tick goroutine")
			}
			s.Players()
			if e.Address == nil || e.Address.Network() != "pipe" {
				t.Error("missing remote address")
			}
			seen = append(seen, e.Command)
			if e.Command == "cancel" {
				e.SetCancelled(true)
			} else {
				e.Command = "list"
			}
		})
		return nil
	}}}})
	client, remote := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); srv.ServeRcon(remote) }()
	t.Cleanup(func() { client.Close(); <-done })
	request := func(kind int32, text string) string {
		t.Helper()
		frame := make([]byte, len(text)+14)
		binary.LittleEndian.PutUint32(frame, uint32(len(text)+10))
		binary.LittleEndian.PutUint32(frame[4:], 7)
		binary.LittleEndian.PutUint32(frame[8:], uint32(kind))
		copy(frame[12:], text)
		if _, err := client.Write(frame); err != nil {
			t.Fatal(err)
		}
		var head [4]byte
		if _, err := io.ReadFull(client, head[:]); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, binary.LittleEndian.Uint32(head[:]))
		if _, err := io.ReadFull(client, body); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(body) != 7 {
			t.Error("RCON response id")
		}
		return string(body[8 : len(body)-2])
	}
	request(3, "secret")
	if out := request(2, "/rewrite"); !strings.Contains(out, "players online") {
		t.Errorf("rewrite reply = %q", out)
	}
	if out := request(2, "cancel"); out != "" {
		t.Errorf("cancel reply = %q", out)
	}
	servertest.HoldTicks(t, srv)
	if !slices.Equal(seen, []string{"rewrite", "cancel"}) {
		t.Errorf("remote events = %v", seen)
	}
}

func checkPluginList(t *testing.T, data []byte, count, names string) {
	t.Helper()
	r := protocol.NewReader(data)
	text, ok := protocol.ReadText(r)
	if !ok || text.Translate != "commands.list.players" || len(text.With) != 3 || text.With[0].Plain() != count || text.With[2].Plain() != names || r.ReadBool() || r.Err() != nil {
		t.Errorf("list feedback = %+v", text)
	}
}

func TestPluginPermissionSharedDependencies(t *testing.T) {
	var h plugin.Server
	visits := make(map[string]int)
	servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "dependencies", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		for layer := range 10 {
			children := map[string]bool{"leaf": true}
			if layer < 9 {
				children = map[string]bool{fmt.Sprintf("layer%dA", layer+1): true, fmt.Sprintf("layer%dB", layer+1): true}
			}
			for _, side := range []string{"A", "B"} {
				s.Permissions().Register(plugin.Permission{Name: fmt.Sprintf("layer%d%s", layer, side), Default: plugin.PermissionDefault{OpLevel: 5}, Children: children})
			}
		}
		s.Permissions().Register(plugin.Permission{Name: "leaf", Default: plugin.PermissionDefault{OpLevel: 5}})
		plugin.Provide[plugin.PermissionProvider](s, permissionProvider(func(_ plugin.CommandSender, node string) (bool, bool) {
			visits[node]++
			// Re-enter the registry without holding its lock.
			h.Permissions().Get(node)
			return node == "direct", node == "direct"
		}), 1)
		return nil
	}}}})
	if !h.Console().HasPermission("direct") || len(visits) != 1 || visits["direct"] != 1 {
		t.Fatalf("direct provider decision visited %v", visits)
	}
	for range 2 {
		clear(visits)
		if h.Console().HasPermission("leaf") {
			t.Fatal("denied graph granted leaf")
		}
		if len(visits) != 21 {
			t.Fatalf("visited %d names, want 21", len(visits))
		}
		for name, count := range visits {
			if count != 1 {
				t.Errorf("%s visited %d times, want once", name, count)
			}
		}
	}
	h.Permissions().Register(plugin.Permission{Name: "layer0A", Children: map[string]bool{"layer1A": true, "layer1B": true}})
	if !h.Console().HasPermission("leaf") {
		t.Error("replacing a dependency did not update its default")
	}
	h.Permissions().Register(plugin.Permission{Name: "layer0A"})
	if h.Console().HasPermission("leaf") {
		t.Error("removed dependency edges survived replacement")
	}
}

// vanilla: ServerPlayer.teleport, PlayerList.sendPlayerPermissionLevel
func TestPluginCommandDimensionChange(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var h plugin.Server
	var tickGoroutine string
	var commandListener plugin.Listener
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "crossing", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		plugin.Listen(s, func(*plugin.ServerTickStartEvent) { tickGoroutine = currentGoroutine() })
		plugin.Listen(s, func(e *plugin.PlayerCommandSendEvent) {
			if currentGoroutine() != tickGoroutine {
				t.Error("tree callback outside tick goroutine")
			}
			s.Players()
			// Synchronous dispatch from a tree callback must hold no writer lock.
			if _, err := s.Dispatch(e.Player, "list"); err != nil {
				t.Errorf("tree callback dispatch: %v", err)
			}
			e.Hide = []string{"list"}
		})
		commandListener = plugin.Listen(s, func(e *plugin.PlayerCommandEvent) {
			if e.Command == "dispatch" {
				if n, err := s.Dispatch(e.Player, "execute in minecraft:overworld run tp @s 0 100 0"); n != 1 || err != nil {
					t.Errorf("crossing dispatch = %d, %v", n, err)
				}
				e.SetCancelled(true)
			}
		})
		return nil
	}}}}, servertest.OpsListing(t, "Crosser"))
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Crosser")
	for _, step := range []struct{ line, dimension string }{
		{"execute in minecraft:the_end run tp @s 0 100 0", "minecraft:the_end"},
		{"dispatch", "minecraft:overworld"},
		{"execute in minecraft:the_end run tp @s 0 100 0", "minecraft:the_end"},
	} {
		c.ChatCommand(step.line)
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayRespawn))
		r.ReadVarInt()
		if dim := r.ReadString(32767); dim != step.dimension {
			t.Fatalf("respawn dimension = %s, want %s", dim, step.dimension)
		}
		nodes := servertest.DecodeCommands(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlayCommands))
		for _, child := range nodes[0].Children {
			if nodes[child].Name == "list" {
				t.Error("crossing ignored hidden root")
			}
		}
		c.ExpectWithinTicks(srv, 0, protocol.IDPlaySyncPosition)
		if dim := srv.PlayerDimension("Crosser"); dim != step.dimension {
			t.Fatalf("player dimension = %s", dim)
		}
		// The last crossing also covers a tree hook without an input-command hook.
		if step.line == "dispatch" {
			commandListener.Unregister()
		}
	}
	// The console also enters through a connection-to-tick handoff with only the tree hook.
	out := srv.RunLine("execute in minecraft:overworld run tp Crosser 0 100 0")
	if len(out) != 1 || out[0].Translate != "commands.teleport.success.location.single" {
		t.Errorf("console crossing feedback = %v", out)
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayRespawn)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayCommands)
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)
	h.Scheduler().RunTask(func() {
		p, _ := h.PlayerNamed("Crosser")
		if n, err := h.Dispatch(p, "execute in minecraft:the_end run tp @s 0 100 0"); n != 1 || err != nil {
			t.Errorf("scheduled crossing dispatch = %d, %v", n, err)
		}
	})
	servertest.RunTicks(t, srv, 1)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayRespawn)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayCommands)
}
