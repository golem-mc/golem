package plugins_test

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: Commands.fillUsableCommands; Brigadier CommandDispatcher.parseNodes.
func TestPluginCommandRedirects(t *testing.T) {
	var host plugin.Server
	ready := false
	cfg := servertest.BlockTestConfig()
	cfg.CommandSpamThresholdSeconds = 100
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "redirects", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		run := func(ctx *plugin.CommandContext) (int, error) {
			if len(ctx.Args) != 1 || plugin.Arg[string](ctx, "word") != "hello" {
				t.Errorf("redirect arguments = %#v", ctx.Args)
			}
			if ctx.Input == "destination hello" && ctx.Location.Pos != (plugin.Vec3{X: 10.5, Y: 20, Z: 30.5}) {
				t.Errorf("redirected execute position = %v", ctx.Location.Pos)
			}
			ctx.Sender.SendMessage(plugin.Plain(ctx.Input))
			return 7, nil
		}
		for _, c := range []plugin.Command{
			{Name: "destination", Run: func(ctx *plugin.CommandContext) (int, error) {
				ctx.Sender.SendMessage(plugin.Plain("bare"))
				return 3, nil
			}, Children: []plugin.Node{{Arg: plugin.WordArg("word"), Run: run}}},
			{Name: "link", Aliases: []string{"shortcut"}, Redirect: "destination"},
			{Name: "ending", Children: []plugin.Node{{Arg: plugin.WordArg("first"), Redirect: "destination", Run: func(ctx *plugin.CommandContext) (int, error) {
				if len(ctx.Args) != 1 || plugin.Arg[string](ctx, "first") != "hello" {
					t.Errorf("unfollowed redirect arguments = %#v", ctx.Args)
				}
				ctx.Sender.SendMessage(plugin.Plain("own handler"))
				return 1, nil
			}}}},
			{Name: "routes", Children: []plugin.Node{{Literal: "native", Redirect: "list"}, {Literal: "time", Redirect: "time"}, {Literal: "exec", Redirect: "execute"}, {Arg: plugin.BlockPosArg("pos"), Redirect: "redirects:destination"}}},
		} {
			if err := s.Commands().Register(c); err != nil {
				t.Error(err)
				return err
			}
		}
		for _, c := range []plugin.Command{
			{Name: "missing", Redirect: "notregistered"},
			{Name: "cycle", Redirect: "cycle"},
			{Name: "aliascycle", Aliases: []string{"back"}, Redirect: "back"},
			{Name: "children", Redirect: "destination", Children: []plugin.Node{{Literal: "bad"}}},
		} {
			if err := s.Commands().Register(c); err == nil {
				t.Errorf("invalid redirect registered: %s", c.Name)
			}
		}
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("redirects were not registered")
	}
	c := registrationClient(t, srv)
	for _, name := range []string{"link", "redirects:link", "shortcut", "redirects:shortcut"} {
		c.ChatCommand(name)
		text, _ := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
		if !textHasTranslation(text, "command.unknown.command") {
			t.Errorf("bare redirect refusal = %+v", text)
		}
		c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
		c.ChatCommand(name + " hello")
		registrationFeedback(t, srv, c, name+" hello")
	}
	c.ChatCommand("ending hello")
	registrationFeedback(t, srv, c, "own handler")
	c.ChatCommand("ending ignored hello")
	registrationFeedback(t, srv, c, "ending ignored hello")
	c.ChatCommand("routes 1 2 3 hello")
	registrationFeedback(t, srv, c, "routes 1 2 3 hello")
	c.ChatCommand("routes native uuids")
	text, _ := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
	if text.Translate != "commands.list.players" {
		t.Errorf("redirect to vanilla command = %+v", text)
	}
	c.CommandSuggestion(60, "/routes 1 2 3 h")
	// Word arguments have no default completions; redirecting must still finish.
	_, _, _, matches := registrationSuggestions(t, srv, c)
	if len(matches) != 0 {
		t.Errorf("word completions = %v", matches)
	}
	registrationTask(t, srv, host, func() {
		sender := &recordingSender{level: 4}
		if result, err := host.Dispatch(sender, "routes 1 2 3 hello"); result != 7 || err != nil {
			t.Errorf("redirect dispatch=%d,%v", result, err)
		}
		if result, err := host.Dispatch(sender, "routes time query time"); result < 0 || err != nil {
			t.Errorf("native argument dispatch=%d,%v", result, err)
		}
		sender.messages = nil
		if result, err := host.Dispatch(sender, "routes exec positioned 10 20 30 run destination hello"); result != 1 || err != nil {
			t.Errorf("execute redirect dispatch=%d,%v", result, err)
		}
		host.Commands().Unregister("destination")
	})
	c.ChatCommand("link hello")
	c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
	c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
}

func TestPluginCommandRedirectSuggestionsAndPermission(t *testing.T) {
	ready := false
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "redirects", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		s.Permissions().Register(plugin.Permission{Name: "hidden", Default: plugin.PermissionDefault{OpLevel: 5}})
		for _, c := range []plugin.Command{
			{Name: "choose", Permission: "hidden", Children: []plugin.Node{{Arg: plugin.WordArg("word"), Suggest: func(*plugin.CommandContext, string) []string { t.Error("denied redirect completion ran"); return nil }}}},
			{Name: "choosealias", Redirect: "choose"},
			{Name: "again", Children: []plugin.Node{{Literal: "again", Redirect: "again"}, {Literal: "done", Run: func(ctx *plugin.CommandContext) (int, error) {
				ctx.Sender.SendMessage(plugin.Plain("done"))
				return 1, nil
			}}}},
		} {
			if err := s.Commands().Register(c); err != nil {
				return err
			}
		}
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("redirects were not registered")
	}
	servertest.FreezeUntilHeld(t, srv)
	nodes := servertest.DecodeCommands(t, joinCommandTree(t, srv, "Tree"))
	for _, name := range []string{"choose", "choosealias", "redirects:choose", "redirects:choosealias"} {
		if slices.ContainsFunc(nodes, func(n servertest.TestCommandNode) bool { return n.Name == name }) {
			t.Errorf("denied redirect visible: %s", name)
		}
	}
	c := registrationClient(t, srv)
	c.CommandSuggestion(61, "/choosealias h")
	_, _, _, matches := registrationSuggestions(t, srv, c)
	if len(matches) != 0 {
		t.Errorf("denied redirect completions = %v", matches)
	}
	c.ChatCommand("again again again done")
	registrationFeedback(t, srv, c, "done")
	c.CommandSuggestion(62, "/again again d")
	_, _, _, matches = registrationSuggestions(t, srv, c)
	if !slices.Equal(matches, []string{"done"}) {
		t.Errorf("recursive redirect completions = %v", matches)
	}
}
