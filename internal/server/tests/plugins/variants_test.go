package plugins_test

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

func TestPluginCommandsAll(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		err := h.Commands().Register(plugin.Command{Name: "warp", Aliases: []string{"w2"}, Description: "go", Permission: "warps.use",
			Run: func(*plugin.CommandContext) (int, error) { return 1, nil }})
		if err != nil {
			t.Fatal(err)
		}
		var warp, tp *plugin.CommandInfo
		all := h.Commands().All()
		for i, c := range all {
			switch c.Name {
			case "warp":
				warp = &all[i]
			case "teleport":
				tp = &all[i]
			}
		}
		if warp == nil || warp.Description != "go" || warp.Permission != "warps.use" || warp.Plugin != "world" || len(warp.Aliases) != 1 || warp.Aliases[0] != "w2" {
			t.Errorf("warp=%+v", warp)
		}
		if tp == nil || tp.Plugin != "" || !slices.Contains(tp.Aliases, "tp") {
			t.Errorf("teleport=%+v", tp)
		}
	})
}
