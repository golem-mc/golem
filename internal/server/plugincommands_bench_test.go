package server

import (
	"errors"
	"testing"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/plugin"
)

func BenchmarkInactivePluginCommandTreePacket(b *testing.B) {
	c := &client{srv: &Server{}}
	command.ForLevel(command.PermLevelOwners)
	b.ReportAllocs()
	for b.Loop() {
		c.commandTreePacket(command.PermLevelOwners)
	}
}

// BenchmarkRegisteredPluginCommands measures the active registry, including
// permission filtering, parsing and plugin context conversion.
func BenchmarkRegisteredPluginCommands(b *testing.B) {
	s, _ := newCommandBenchServer(b, 0)
	fixture := &registeredCommandBenchPlugin{}
	s.loadPlugins([]plugin.Registration{{Name: "bench", Plugin: fixture}})
	s.enablePlugins()
	if !fixture.registered {
		b.Fatal("benchmark command was not registered")
	}
	c := &client{srv: s, player: &player.Player{Name: "Bench"}}
	src := s.consoleSource()
	b.Run("TreePacket", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			c.commandTreePacket(command.PermLevelOwners)
		}
	})
	b.Run("Dispatch", func(b *testing.B) {
		const input = `benchcmd "two words"`
		b.ReportAllocs()
		for b.Loop() {
			result, err := fixture.host.Dispatch(fixture.host.Console(), input)
			if err != nil || result != 1 {
				b.Fatalf("dispatch=%d, %v", result, err)
			}
		}
	})
	b.Run("Suggestions", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, matches := command.Suggest(src.CommandTree(), "/benchcmd b", src)
			if len(matches) != 1 {
				b.Fatalf("suggestions=%v", matches)
			}
		}
	})
}

type registeredCommandBenchPlugin struct {
	registered bool
	host       plugin.Server
}

func (p *registeredCommandBenchPlugin) Enable(s plugin.Server) error {
	p.host = s
	s.Permissions().Register(plugin.Permission{Name: "bench.use"})
	err := s.Commands().Register(plugin.Command{Name: "benchcmd", Permission: "bench.use", Children: []plugin.Node{{
		Arg: plugin.StringArg("value"),
		Run: func(ctx *plugin.CommandContext) (int, error) {
			if plugin.Arg[string](ctx, "value") != "two words" {
				return 0, errors.New("unexpected argument")
			}
			return 1, nil
		},
		Suggest: func(*plugin.CommandContext, string) []string { return []string{"blue"} },
	}}})
	p.registered = err == nil
	return err
}

func (*registeredCommandBenchPlugin) Disable() {}
