package plugins_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

func TestPluginMinecraftNamesAndUsage(t *testing.T) {
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "usage", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		return s.Commands().Register(plugin.Command{Name: "warp", Usage: "/warp <name>", Run: func(*plugin.CommandContext) (int, error) { return 1, nil }})
	}}}})
	if out := srv.RunLine("minecraft:seed"); len(out) != 1 || out[0].Translate != "commands.seed.success" {
		t.Fatalf("minecraft:seed produced %v", out)
	}
	var vanilla, warp *plugin.CommandInfo
	infos := host.Commands().All()
	for i := range infos {
		switch infos[i].Name {
		case "seed":
			vanilla = &infos[i]
		case "warp":
			warp = &infos[i]
		}
	}
	if vanilla == nil || !slices.Contains(vanilla.Aliases, "minecraft:seed") {
		t.Fatalf("seed info: %+v", vanilla)
	}
	if warp == nil || warp.Usage != "/warp <name>" {
		t.Fatalf("warp info: %+v", warp)
	}
}

func TestPluginRegisterAlias(t *testing.T) {
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "alias", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		if err := plugin.RegisterAlias(s, "sd", "seed"); err != nil {
			return err
		}
		return plugin.RegisterAlias(s, "bad", "gamemode survival Nobody")
	}}}})
	if out := srv.RunLine("sd"); len(out) != 1 || !strings.Contains(fmt.Sprint(out), "commands.seed.success") {
		t.Fatalf("alias produced %v", out)
	}
	out := srv.RunLine("bad")
	if !notFound(out) || len(out) != 1 {
		t.Fatalf("failing alias produced %v", out)
	}
}
