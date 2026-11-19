package plugins_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

func said(out []protocol.Text, word string) bool {
	return strings.Contains(fmt.Sprint(out), "text:"+word)
}

func notFound(out []protocol.Text) bool {
	return strings.Contains(fmt.Sprint(out), "argument.entity.notfound.player")
}

func overrideGamemode(s plugin.Server, say string) plugin.Command {
	return plugin.Command{Name: "gamemode", Override: true, Children: []plugin.Node{{Literal: "spin", Run: func(ctx *plugin.CommandContext) (int, error) {
		ctx.Sender.SendMessage(plugin.Plain(say))
		return 7, nil
	}}}}
}

func TestPluginCommandOverride(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var host plugin.Server
	var second error
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{
		{Name: "first", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			host = s
			return s.Commands().Register(overrideGamemode(s, "plugin"))
		}}},
		{Name: "second", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			second = s.Commands().Register(overrideGamemode(s, "other"))
			return nil
		}}},
	}, servertest.WriteOpsFile(t, servertest.OpRecord{UUID: protocol.OfflineUUID("Watcher").String(), Name: "Watcher", Level: 2}))
	if second == nil {
		t.Error("a second override of the same name registered")
	}
	if out := srv.RunLine("gamemode spin"); len(out) != 1 || !said(out, "plugin") {
		t.Fatalf("bare name did not run the override: %v", out)
	}
	if out := srv.RunLine("minecraft:gamemode survival Nobody"); !notFound(out) {
		t.Fatalf("minecraft: name did not run vanilla: %v", out)
	}
	if out := srv.RunLine("execute run gamemode spin"); len(out) != 1 || !said(out, "plugin") {
		t.Fatalf("execute run did not resolve the override: %v", out)
	}
	if out := srv.RunLine("execute run minecraft:gamemode survival Nobody"); !notFound(out) {
		t.Fatalf("execute run minecraft: produced %v", out)
	}

	names := func(data []byte) map[string][]string {
		nodes := servertest.DecodeCommands(t, data)
		out := make(map[string][]string)
		for _, ci := range nodes[0].Children {
			for _, gi := range nodes[ci].Children {
				out[nodes[ci].Name] = append(out[nodes[ci].Name], nodes[gi].Name)
			}
			if _, ok := out[nodes[ci].Name]; !ok {
				out[nodes[ci].Name] = nil
			}
		}
		return out
	}
	tree := names(joinCommandTree(t, srv, "Watcher"))
	if got := tree["gamemode"]; len(got) != 1 || got[0] != "spin" {
		t.Errorf("client's gamemode children = %v, want [spin]", got)
	}
	if got := tree["minecraft:gamemode"]; len(got) != 1 || got[0] != "gamemode" {
		t.Errorf("client's minecraft:gamemode children = %v, want vanilla's [gamemode]", got)
	}

	host.Commands().Unregister("gamemode")
	servertest.RunTicks(t, srv, 1)
	if out := srv.RunLine("gamemode spin"); said(out, "plugin") || !strings.Contains(fmt.Sprint(out), "argument.gamemode.invalid") {
		t.Fatalf("vanilla did not return after unregister: %v", out)
	}
}

func TestPluginCommandOverrideAliasWithArguments(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	run := func(ctx *plugin.CommandContext) (int, error) {
		ctx.Sender.SendMessage(plugin.Plain("plugin"))
		return 1, nil
	}
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{
		{Name: "essentials", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			return s.Commands().Register(plugin.Command{Name: "kick", Aliases: []string{"ekick"}, Override: true,
				Run: run, Children: []plugin.Node{{Arg: plugin.GreedyArg("args"), Run: run}}})
		}}},
	})
	for _, line := range []string{"kick Nobody x", "ekick Nobody x", "essentials:kick Nobody x", "essentials:ekick Nobody x", "ekick", "essentials:kick"} {
		if out := srv.RunLine(line); !said(out, "plugin") {
			t.Errorf("%q did not run the plugin's command: %v", line, out)
		}
	}
}
