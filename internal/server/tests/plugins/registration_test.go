package plugins_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
)

func TestPluginCommandRegistration(t *testing.T) {
	ready := false
	var h plugin.Server
	var location plugin.Location
	permissionCalls := 0
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "example", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		plugin.Provide[plugin.PermissionProvider](s, permissionProvider(func(plugin.CommandSender, string) (bool, bool) {
			permissionCalls++
			return true, true
		}), 1)
		if err := s.Commands().Register(plugin.Command{Name: "hello", Permission: "hello.use", Run: func(ctx *plugin.CommandContext) (int, error) {
			location = ctx.Location
			ctx.Sender.SendMessage(plugin.Plain("registered"))
			return 7, nil
		}}); err != nil {
			return err
		}
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("command registration did not complete")
	}
	if out := srv.RunLine("hello"); len(out) != 1 || out[0].Plain() != "registered" {
		t.Fatalf("command feedback = %v", out)
	}
	registrationTask(t, srv, h, func() {
		sender := &recordingSender{level: 4}
		permissionCalls = 0
		if result, err := h.Dispatch(sender, "hello"); result != 7 || err != nil {
			t.Errorf("custom dispatch=%d, %v", result, err)
		}
		// The root and its namespaced alias are filtered once for this invocation.
		if permissionCalls != 2 {
			t.Errorf("permission calls=%d, want 2", permissionCalls)
		}
		sender.messages = nil
		result, err := h.Dispatch(sender, "execute positioned 1 2 3 run hello")
		if result != 1 || err != nil || len(sender.messages) != 1 || sender.messages[0].PlainString() != "registered" {
			t.Errorf("nested custom dispatch=%d, %v, %v", result, err, sender.messages)
		}
		if location.Pos != (plugin.Vec3{X: 1.5, Y: 2, Z: 3.5}) {
			t.Errorf("execute location=%v", location)
		}
	})

}

// vanilla: Commands.fillUsableCommands, Commands.finishParsing; Brigadier argument types.
func TestPluginCommandArgumentsAndSuggestions(t *testing.T) {
	var h plugin.Server
	ready := false
	cases := []struct {
		name  string
		arg   plugin.Argument
		input string
		want  any
	}{
		{"word", plugin.WordArg("value"), "hello", "hello"},
		{"string", plugin.StringArg("value"), `"hello  world"`, "hello  world"},
		{"greedy", plugin.GreedyArg("value"), "hello  world  ", "hello  world  "},
		{"bool", plugin.BoolArg("value"), "true", true},
		{"int", plugin.IntArg("value", -2, 5), "5", int32(5)},
		{"double", plugin.FloatArg("value", -2, 5), "1.23456789", float64(1.23456789)},
		{"distance", plugin.FloatArg("value", -59999968, 59999968), "1", float64(1)},
		{"key", plugin.KeyArg("value"), "stone", plugin.MinecraftKey("stone")},
	}
	cfg := servertest.BlockTestConfig()
	cfg.CommandSpamThresholdSeconds = 100 // This test deliberately sends a burst of commands.
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "example", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		var children []plugin.Node
		for _, tc := range cases {
			children = append(children, plugin.Node{Literal: tc.name, Children: []plugin.Node{{Arg: tc.arg, Run: func(ctx *plugin.CommandContext) (int, error) {
				s.Players()
				s.Permissions().Get("anything") // callbacks must hold no server or registry lock.
				if !reflect.DeepEqual(ctx.Args["value"], tc.want) {
					t.Errorf("%s value = %#v, want %#v", tc.name, ctx.Args["value"], tc.want)
				}
				if (ctx.Sender.Name() == "Tester" && ctx.Entity == nil) || ctx.Location.World.Key() != plugin.MinecraftKey("overworld") {
					t.Error("command context lacks sender, entity or world")
				}
				ctx.Sender.SendMessage(plugin.Plain("parsed"))
				return 7, nil
			}}}})
		}
		children = append(children, plugin.Node{Literal: "choose", Children: []plugin.Node{{Arg: plugin.StringArg("first"), Children: []plugin.Node{{Arg: plugin.StringArg("second"), Suggest: func(ctx *plugin.CommandContext, partial string) []string {
			if len(ctx.Args) != 1 || plugin.Arg[string](ctx, "first") != "two words" || (partial != "b" && partial != `"three wo`) || ctx.Input != `p choose "two words" `+partial {
				t.Errorf("suggest context: %#v partial=%q", ctx.Args, partial)
			}
			s.Players()
			if partial != "b" {
				return []string{`"three words"`}
			}
			return []string{"blue", "black"}
		}, Run: func(*plugin.CommandContext) (int, error) { return 1, nil }}}}}})
		children = append(children, plugin.Node{Literal: "quote", Children: []plugin.Node{{Arg: plugin.StringArg("value"), Suggest: func(ctx *plugin.CommandContext, partial string) []string {
			if len(ctx.Args) != 0 || partial != `"two wo` || ctx.Input != `p quote "two wo` {
				t.Errorf("quoted suggest context: %#v partial=%q input=%q", ctx.Args, partial, ctx.Input)
			}
			return []string{`"two words"`}
		}, Run: func(*plugin.CommandContext) (int, error) { return 1, nil }}}})
		if err := s.Commands().Register(plugin.Command{Name: "parse", Aliases: []string{"p"}, Children: children}); err != nil {
			return err
		}
		if err := s.Commands().Register(plugin.Command{Name: "failure", Run: func(*plugin.CommandContext) (int, error) {
			return 0, plugin.CommandError{Message: plugin.Plain("refused").Bold()}
		}}); err != nil {
			return err
		}
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("registration failed")
	}
	c := registrationClient(t, srv)
	for _, name := range []string{"parse", "example:parse", "p", "example:p"} {
		for _, tc := range cases {
			if name != "parse" && tc.name != "word" {
				continue
			}
			c.ChatCommand(name + " " + tc.name + " " + tc.input)
			registrationFeedback(t, srv, c, "parsed")
		}
	}
	c.CommandSuggestion(19, `/p choose "two words" b`)
	id, start, length, matches := registrationSuggestions(t, srv, c)
	if id != 19 || start != 22 || length != 1 || !slices.Equal(matches, []string{"black", "blue"}) {
		t.Fatalf("custom suggestions: %d %d %d %v", id, start, length, matches)
	}
	for _, tc := range []struct {
		input, match string
		start        int32
	}{
		{`/p quote "two wo`, `"two words"`, 9},
		{`/p choose "two words" "three wo`, `"three words"`, 22},
	} {
		c.CommandSuggestion(21, tc.input)
		id, start, length, matches := registrationSuggestions(t, srv, c)
		if id != 21 || start != tc.start || length != int32(len(tc.input))-tc.start || !slices.Equal(matches, []string{tc.match}) {
			t.Fatalf("quoted suggestions for %q: %d %d %d %v", tc.input, id, start, length, matches)
		}
	}
	c.CommandSuggestion(20, "/parse bool f")
	_, _, _, matches = registrationSuggestions(t, srv, c)
	if !slices.Equal(matches, []string{"false"}) {
		t.Fatalf("bool suggestions: %v", matches)
	}
	for _, tc := range []struct{ line, want string }{
		{"parse word @bad", ""},
		{"parse int 6", ""},
		{"parse double -3", "Double must not be less than -2.0: found -3.0"},
		{"parse double 6", "Double must not be more than 5.0: found 6.0"},
		{"parse distance -60000000", "Double must not be less than -5.9999968E7: found -6.0E7"},
		{"parse distance 60000000", "Double must not be more than 5.9999968E7: found 6.0E7"},
		{"parse bool TRUE", ""},
		{`parse string "unclosed`, ""},
	} {
		c.ChatCommand(tc.line)
		data := c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
		text, _ := protocol.ReadText(protocol.NewReader(data))
		if text.Color != protocol.ColorRed || strings.Contains(text.Plain(), "parsed") {
			t.Fatalf("invalid input %q: %v", tc.line, text)
		}
		if tc.want != "" && text.Plain() != tc.want {
			t.Fatalf("feedback for %q=%q, want %q", tc.line, text.Plain(), tc.want)
		}
		c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat) // syntax context
	}
	c.ChatCommand("failure")
	data := c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
	text, _ := protocol.ReadText(protocol.NewReader(data))
	root, _ := nbt.CompoundView(protocol.TextTag(text))
	extra, _ := root["extra"].(nbt.List)
	if root["color"] != nbt.String("red") || len(extra.Items) != 1 {
		t.Fatalf("CommandError: %v", root)
	}
	child, _ := nbt.CompoundView(extra.Items[0])
	if child["text"] != nbt.String("refused") || child["bold"] != nbt.Byte(1) {
		t.Fatalf("CommandError style: %v", child)
	}

	registrationTask(t, srv, h, func() {
		if result, err := h.Dispatch(h.Console(), "parse bool true"); result != 7 || err != nil {
			t.Errorf("dispatch result=%d err=%v", result, err)
		}
	})
}

// vanilla: CommandDispatcher.getCompletionSuggestions, ArgumentCommandNode.listSuggestions
func TestPluginGreedyCommandSuggestions(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	greedy := func(prefix string, preceding map[string]any) plugin.Node {
		return plugin.Node{Arg: plugin.GreedyArg("value"), Suggest: func(ctx *plugin.CommandContext, partial string) []string {
			if len(ctx.Args) != len(preceding) || (len(preceding) > 0 && !reflect.DeepEqual(ctx.Args, preceding)) || ctx.Input != prefix+partial {
				t.Errorf("greedy suggest context: args=%#v partial=%q input=%q", ctx.Args, partial, ctx.Input)
			}
			if strings.HasPrefix("two words", partial) {
				return []string{"two words"}
			}
			return nil
		}, Run: func(*plugin.CommandContext) (int, error) { return 1, nil }}
	}
	ready := false
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "example", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		if err := s.Commands().Register(plugin.Command{Name: "reviewgreedy", Children: []plugin.Node{
			greedy("reviewgreedy ", nil),
			{Literal: "after", Children: []plugin.Node{{Arg: plugin.StringArg("first"), Children: []plugin.Node{
				greedy(`reviewgreedy after "one word" `, map[string]any{"first": "one word"}),
			}}}},
		}}); err != nil {
			return err
		}
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("registration failed")
	}
	c := registrationClient(t, srv)
	for _, prefix := range []string{"/reviewgreedy ", `/reviewgreedy after "one word" `} {
		for _, partial := range []string{"tw", "two ", "two wo"} {
			c.CommandSuggestion(22, prefix+partial)
			id, start, length, matches := registrationSuggestions(t, srv, c)
			if id != 22 || start != int32(len(prefix)) || length != int32(len(partial)) || !slices.Equal(matches, []string{"two words"}) {
				t.Fatalf("greedy suggestions for %q: %d %d %d %v", prefix+partial, id, start, length, matches)
			}
		}
	}
}

func TestPluginCommandClashesAndCleanup(t *testing.T) {
	var live, failed plugin.Server
	ready := false
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{
		{Name: "failed", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			failed = s
			if err := s.Commands().Register(plugin.Command{Name: "released"}); err != nil {
				return err
			}
			return errors.New("disable this plugin")
		}}},
		{Name: "live", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			live = s
			run := plugin.Node{Run: func(ctx *plugin.CommandContext) (int, error) {
				ctx.Sender.SendMessage(plugin.Plain("alive"))
				return 1, nil
			}}
			for _, c := range []plugin.Command{{Name: "list"}, {Name: "other", Aliases: []string{"list"}}} {
				if err := s.Commands().Register(c); err == nil {
					t.Errorf("clash accepted: %s", c.Name)
				}
			}
			if err := s.Commands().Register(plugin.Command{Name: "released", Aliases: []string{"alias"}, Node: run}); err != nil {
				return err
			}
			if err := s.Commands().Register(plugin.Command{Name: "alias"}); err == nil {
				t.Error("plugin clash accepted")
			}
			if err := s.Commands().Register(plugin.Command{Name: "other", Aliases: []string{"released"}}); err == nil {
				t.Error("alias clash accepted")
			}
			ready = true
			return nil
		}}},
	})
	if !ready {
		t.Fatal("registration failed")
	}
	if err := failed.Commands().Register(plugin.Command{Name: "late"}); err == nil {
		t.Fatal("disabled owner registered command")
	}
	c := registrationClient(t, srv)
	c.ChatCommand("live:alias")
	registrationFeedback(t, srv, c, "alive")
	registrationTask(t, srv, live, func() { failed.Commands().Unregister("released") })
	c.ChatCommand("released")
	registrationFeedback(t, srv, c, "alive")
	registrationTask(t, srv, live, func() { live.Commands().Unregister("released") })
	nodes := servertest.DecodeCommands(t, c.ExpectWithinTicks(srv, 4, protocol.IDPlayCommands))
	for _, n := range nodes {
		if slices.Contains([]string{"released", "alias", "live:released", "live:alias", "failed:released", "other", "late"}, n.Name) {
			t.Errorf("removed or failed root remains: %s", n.Name)
		}
	}
	c.ChatCommand("alias")
	c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
	c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
}

func TestPluginCommandPermissionTreeAndExecution(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(fmt.Sprint(provider), func(t *testing.T) {
			ready := false
			srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "permissions", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				s.Permissions().Register(plugin.Permission{Name: "allow"})
				s.Permissions().Register(plugin.Permission{Name: "deny", Default: plugin.PermissionDefault{OpLevel: 5}})
				if provider {
					plugin.Provide[plugin.PermissionProvider](s, permissionProvider(func(sender plugin.CommandSender, name string) (bool, bool) {
						s.Players()
						s.Permissions().Get(name)
						return name == "deny", true
					}), 1)
				}
				for _, permission := range []string{"allow", "deny"} {
					if err := s.Commands().Register(plugin.Command{Name: permission, Aliases: []string{permission + "alias"}, Permission: permission, Run: func(ctx *plugin.CommandContext) (int, error) {
						ctx.Sender.SendMessage(plugin.Plain(permission))
						return 1, nil
					}, Children: []plugin.Node{{Literal: "child", Permission: "deny", Run: func(ctx *plugin.CommandContext) (int, error) {
						ctx.Sender.SendMessage(plugin.Plain("child"))
						return 1, nil
					}}}}); err != nil {
						return err
					}
				}
				ready = true
				return nil
			}}}})
			if !ready {
				t.Fatal("registration failed")
			}
			servertest.FreezeUntilHeld(t, srv)
			nodes := servertest.DecodeCommands(t, joinCommandTree(t, srv, "Tree"))
			allowed, denied := "allow", "deny"
			if provider {
				allowed, denied = denied, allowed
			}
			for _, name := range []string{allowed, allowed + "alias", "permissions:" + allowed, "permissions:" + allowed + "alias"} {
				if !slices.ContainsFunc(nodes, func(n servertest.TestCommandNode) bool { return n.Name == name && n.Executable }) {
					t.Errorf("missing executable permitted root %s", name)
				}
			}
			for _, name := range []string{denied, denied + "alias", "permissions:" + denied, "permissions:" + denied + "alias"} {
				if slices.ContainsFunc(nodes, func(n servertest.TestCommandNode) bool { return n.Name == name }) {
					t.Errorf("denied root %s visible", name)
				}
			}
			c := registrationClient(t, srv)
			for _, name := range []string{allowed, allowed + "alias", "permissions:" + allowed} {
				c.ChatCommand(name)
				registrationFeedback(t, srv, c, allowed)
			}
			for _, name := range []string{denied, denied + "alias", "permissions:" + denied} {
				c.ChatCommand(name)
				c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
				c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
			}
			c.CommandSuggestion(1, "/"+denied)
			_, _, _, matches := registrationSuggestions(t, srv, c)
			if len(matches) != 0 {
				t.Fatalf("denied suggestions: %v", matches)
			}
			c.ChatCommand(allowed + " child")
			if provider {
				registrationFeedback(t, srv, c, "child")
			} else {
				c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
				c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
			}
		})
	}
}

func registrationClient(t *testing.T, srv *server.Server) *servertest.Client {
	t.Helper()
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Tester")
	return c
}

func registrationFeedback(t *testing.T, srv *server.Server, c *servertest.Client, want string) {
	t.Helper()
	text, ok := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
	if !ok || text.Plain() != want {
		t.Fatalf("feedback=%+v, want %q", text, want)
	}
}

func registrationTask(t *testing.T, srv *server.Server, h plugin.Server, fn func()) {
	t.Helper()
	servertest.HoldTicks(t, srv)
	h.Scheduler().RunTask(fn)
	servertest.RunTicks(t, srv, 1)
	servertest.ReleaseTicks(t, srv)
}

func registrationSuggestions(t *testing.T, srv *server.Server, c *servertest.Client) (id, start, length int32, matches []string) {
	t.Helper()
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlayCommandSuggestions))
	id, start, length = r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
	for range r.ReadVarInt() {
		matches = append(matches, r.ReadString(32767))
		r.ReadBool()
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	return
}
