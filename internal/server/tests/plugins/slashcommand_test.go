package plugins_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// A plugin command named "/set" is typed "//set": the client tree has the
// root literal "/set" and the leading slash of the line is stripped once.
func TestPluginSlashCommand(t *testing.T) {
	t.Parallel()
	var bad []string
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "worldedit", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		for _, name := range []string{"", "/", "//set", "/a/b", "a/b", "a b", "a:b", "/a:b", "/a b"} {
			if s.Commands().Register(plugin.Command{Name: name}) == nil {
				bad = append(bad, name)
			}
		}
		return s.Commands().Register(plugin.Command{Name: "/set", Children: []plugin.Node{
			{Literal: "stone", Run: func(ctx *plugin.CommandContext) (int, error) {
				ctx.Sender.SendMessage(plugin.Plain("set stone"))
				return 1, nil
			}},
			{Literal: "stick"},
		}})
	}}}})
	if len(bad) > 0 {
		t.Errorf("invalid names registered: %q", bad)
	}
	if out := srv.RunLine("//set stone"); len(out) != 1 || out[0].Plain() != "set stone" {
		t.Errorf("//set stone = %v", out)
	}
	if out := srv.RunLine("execute run /set stone"); !strings.Contains(fmt.Sprint(out), "set stone") {
		t.Errorf("execute run /set stone = %v", out)
	}
	servertest.FreezeUntilHeld(t, srv)
	nodes := servertest.DecodeCommands(t, joinCommandTree(t, srv, "Tester"))
	for _, name := range []string{"/set", "worldedit:/set"} {
		if !slices.ContainsFunc(nodes, func(n servertest.TestCommandNode) bool { return n.Name == name }) {
			t.Errorf("missing root %s", name)
		}
	}
	c := registrationClient(t, srv)
	c.CommandSuggestion(5, "//set st")
	_, start, _, matches := registrationSuggestions(t, srv, c)
	slices.Sort(matches)
	if start != 6 || !slices.Equal(matches, []string{"stick", "stone"}) {
		t.Errorf("suggest //set st = start %d %v", start, matches)
	}
	c.ChatCommand("/set stone")
	registrationFeedback(t, srv, c, "set stone")
}
