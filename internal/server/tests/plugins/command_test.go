package plugins_test

import (
	"encoding/binary"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestPluginPermissionAttachments(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var hostA, hostB plugin.Server
	var ran int
	var pRejoinedA, pRejoinedB plugin.Player
	var attDisable plugin.PermissionAttachment
	var checkedDisable bool
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{
		{Name: "attachA", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			hostA = s
			s.Permissions().Register(plugin.Permission{Name: "everyone", Default: plugin.PermissionDefault{OpLevel: 0}})
			s.Permissions().Register(plugin.Permission{Name: "nobody", Default: plugin.PermissionDefault{OpLevel: 5}})
			s.Permissions().Register(plugin.Permission{
				Name:     "parent",
				Default:  plugin.PermissionDefault{OpLevel: 5},
				Children: map[string]bool{"child.grant": true, "child.deny": false},
			})
			s.Permissions().Register(plugin.Permission{Name: "child.deny", Default: plugin.PermissionDefault{OpLevel: 0}})
			if err := s.Commands().Register(plugin.Command{
				Name:       "gated",
				Permission: "cmd.gated",
				Run: func(*plugin.CommandContext) (int, error) {
					ran++
					return 1, nil
				},
			}); err != nil {
				return err
			}
			return nil
		}, disable: func() {
			// attachB depends on attachA, so attachB is already disabled and
			// cleaned up here while Regular is still online.
			checkedDisable = true
			if !pRejoinedB.IsValid() {
				t.Error("player was no longer online when attachA disabled")
			}
			if pRejoinedA.HasPermission("parent") || pRejoinedA.PermissionSet("parent") {
				t.Error("disabled plugin's grant attachment was not released")
			}
			if !pRejoinedA.HasPermission("everyone") || pRejoinedA.PermissionSet("everyone") {
				t.Error("disabled plugin's deny attachment was not released")
			}
			if pRejoinedB.AddAttachment() != nil {
				t.Error("AddAttachment on closed plugin handle returned non-nil")
			}
			attDisable.Set("parent", true)
			if len(attDisable.Permissions()) != 0 || pRejoinedA.HasPermission("parent") {
				t.Error("released attachment still mutated permissions")
			}
		}}},
		{Name: "attachB", Depends: []string{"attachA"}, Plugin: &testPlugin{enable: func(s plugin.Server) error {
			hostB = s
			return nil
		}}},
	}, servertest.WriteOpsFile(t, servertest.OpRecord{UUID: protocol.OfflineUUID("OpPlayer").String(), Name: "OpPlayer", Level: 4}))
	servertest.FreezeUntilHeld(t, srv)

	cNonOp := servertest.DialServer(t, srv)
	cNonOp.ViewAsk = config.MinDistance
	cNonOp.Join("Regular")

	cOp := servertest.DialServer(t, srv)
	cOp.ViewAsk = config.MinDistance
	cOp.Join("OpPlayer")

	run := func(f func()) {
		done := make(chan struct{})
		hostA.Scheduler().RunTask(func() {
			defer close(done)
			f()
		})
		<-done
	}

	var attA, attB plugin.PermissionAttachment
	var pRegularA, pRegularB, pOp plugin.Player
	run(func() {
		pRegularA, _ = hostA.PlayerNamed("Regular")
		pRegularB, _ = hostB.PlayerNamed("Regular")
		pOp, _ = hostA.PlayerNamed("OpPlayer")

		// Baseline defaults before any attachment.
		if pRegularA.HasPermission("nobody") || pRegularA.PermissionSet("nobody") {
			t.Fatal("non-op unexpectedly has nobody")
		}
		if !pRegularA.HasPermission("everyone") || pRegularA.PermissionSet("everyone") {
			t.Fatal("non-op baseline everyone mismatch")
		}
		if !pOp.HasPermission("unknown") || pOp.PermissionSet("unknown") {
			t.Fatal("op baseline unknown mismatch")
		}

		// Attach true/false values and verify HasPermission, PermissionSet, and Permissions snapshot.
		attA = pRegularA.AddAttachment()
		if attA == nil {
			t.Fatal("AddAttachment returned nil for online player")
		}
		attA.Set("nobody", true)
		attA.Set("everyone", false)
		attA.Set("cmd.gated", true)
		if !pRegularA.HasPermission("nobody") || !pRegularA.PermissionSet("nobody") {
			t.Error("attachment grant not honoured")
		}
		if pRegularA.HasPermission("everyone") || !pRegularA.PermissionSet("everyone") {
			t.Error("attachment deny not honoured")
		}
		if !pRegularB.HasPermission("nobody") || !pRegularB.PermissionSet("nobody") {
			t.Error("other plugin handle did not see attachment")
		}
		snap := attA.Permissions()
		if len(snap) != 3 || !snap["nobody"] || snap["everyone"] || !snap["cmd.gated"] {
			t.Errorf("unexpected Permissions snapshot: %v", snap)
		}
		snap["nobody"] = false
		if !pRegularA.HasPermission("nobody") {
			t.Error("mutating Permissions snapshot changed attachment")
		}

		// Command execution honours attached permission.
		if !pRegularA.RunCommand("gated") || ran != 1 {
			t.Errorf("gated command failed with attached permission: ran=%d", ran)
		}

		// Deny on an op player overrides op level.
		opAtt := pOp.AddAttachment()
		opAtt.Set("unknown", false)
		if pOp.HasPermission("unknown") || !pOp.PermissionSet("unknown") {
			t.Error("attachment deny did not override op level")
		}
		opAtt.Remove()
		if !pOp.HasPermission("unknown") || pOp.PermissionSet("unknown") {
			t.Error("removing op attachment did not restore op level")
		}

		// Parent inheritance and direct child override.
		attA.Set("parent", true)
		if !pRegularA.HasPermission("child.grant") || pRegularA.HasPermission("child.deny") {
			t.Error("attached parent children not honoured")
		}
		attA.Set("child.deny", true)
		if !pRegularA.HasPermission("child.deny") {
			t.Error("direct child attachment did not override parent child deny")
		}
		attA.Unset("child.deny")
		if pRegularA.HasPermission("child.deny") || pRegularA.PermissionSet("child.deny") {
			t.Error("Unset child.deny did not revert to parent")
		}

		// Second attachment from another plugin overrides first; Unset and Remove reveal first.
		attB = pRegularB.AddAttachment()
		attB.Set("nobody", false)
		if pRegularA.HasPermission("nobody") || !pRegularA.PermissionSet("nobody") {
			t.Error("later attachment did not override earlier attachment")
		}
		attB.Unset("nobody")
		if !pRegularA.HasPermission("nobody") || !pRegularA.PermissionSet("nobody") {
			t.Error("unsetting later attachment did not reveal earlier attachment")
		}
		attB.Set("nobody", false)
		attB.Remove()
		attB.Remove() // idempotent
		if !pRegularA.HasPermission("nobody") {
			t.Error("removing later attachment did not reveal earlier attachment")
		}
		attB.Set("nobody", false)
		if !pRegularA.HasPermission("nobody") || len(attB.Permissions()) != 0 {
			t.Error("removed attachment still mutated permissions")
		}

		// Provider service takes precedence when it decides, and falls through when undecided.
		plugin.Provide[plugin.PermissionProvider](hostB, permissionProvider(func(sender plugin.CommandSender, node string) (bool, bool) {
			if node == "nobody" {
				return false, true
			}
			return false, false
		}), 1)
		if pRegularA.HasPermission("nobody") {
			t.Error("provider decision did not take precedence over attachment")
		}
		if pRegularA.HasPermission("everyone") || !pRegularA.PermissionSet("everyone") {
			t.Error("undecided provider did not fall through to attachment")
		}

		// Unset reverts cmd.gated and command is refused.
		attA.Unset("cmd.gated")
		if pRegularA.HasPermission("cmd.gated") || pRegularA.PermissionSet("cmd.gated") {
			t.Error("Unset did not clear cmd.gated")
		}
		if pRegularA.RunCommand("gated") || ran != 1 {
			t.Errorf("gated command ran after Unset: ran=%d", ran)
		}
		// Keep "parent" and "everyone" attached until the player leaves.
	})

	// Leave clears attachments; old handle and attachment are inactive; rejoin starts clean.
	cNonOp.Conn.Close()
	cNonOp.AwaitServed()
	run(func() {
		if pRegularA.HasPermission("parent") || pRegularA.PermissionSet("parent") {
			t.Error("left player still had attached permission")
		}
		if pRegularA.AddAttachment() != nil {
			t.Error("AddAttachment on left player returned non-nil")
		}
		attA.Set("after.leave", true)
		if len(attA.Permissions()) != 0 {
			t.Error("attachment retained entries after player left")
		}
	})

	cRejoin := servertest.DialServer(t, srv)
	cRejoin.ViewAsk = config.MinDistance
	cRejoin.Join("Regular")
	run(func() {
		var ok bool
		pRejoinedA, ok = hostA.PlayerNamed("Regular")
		if !ok {
			t.Fatal("rejoined player not found on hostA")
		}
		pRejoinedB, ok = hostB.PlayerNamed("Regular")
		if !ok {
			t.Fatal("rejoined player not found on hostB")
		}
		if pRejoinedA.HasPermission("parent") || pRejoinedA.PermissionSet("parent") || !pRejoinedA.HasPermission("everyone") || pRejoinedA.PermissionSet("everyone") {
			t.Error("rejoined player inherited stale attachments from previous session")
		}
		attDisable = pRejoinedB.AddAttachment()
		if attDisable == nil {
			t.Fatal("AddAttachment returned nil for rejoined player")
		}
		attDisable.Set("parent", true)
		attDisable.Set("everyone", false)
		if !pRejoinedA.HasPermission("parent") || !pRejoinedA.PermissionSet("parent") || pRejoinedA.HasPermission("everyone") || !pRejoinedA.PermissionSet("everyone") {
			t.Error("attachment before plugin disable did not take effect")
		}
	})
	srv.Shutdown()
	if !checkedDisable {
		t.Fatal("plugin disable check did not run")
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
