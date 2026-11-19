package admin

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSafeModeEnablesOnlyVanilla restarts a world whose level.dat enables a
// pack under datapacks/ in safe mode: /datapack lists only vanilla as enabled
// and the world's pack as available.
// vanilla: MinecraftServer.configurePackRepository
func TestSafeModeEnablesOnlyVanilla(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "p", "x/function/f.mcfunction", "return 1\n")
	first, _ := servertest.Start(t, cfg)
	first.Shutdown()

	safe := *cfg
	safe.SafeMode = true
	_, c := servertest.Join(t, &safe, "Testificate", servertest.OpsListing(t, "Testificate"))
	for _, list := range []struct{ cmd, key, want string }{
		{"datapack list enabled", "commands.datapack.list.enabled.success", "vanilla"},
		{"datapack list available", "commands.datapack.list.available.success", "file/p"},
	} {
		c.ChatCommand(list.cmd)
		got := servertest.FeedbackComponent(t, c)
		if got.Translate != list.key {
			t.Fatalf("%s said %q, want %q", list.cmd, got.Translate, list.key)
		}
		if ids := listedPacks(got.With[1]); !slices.Equal(ids, []string{list.want}) {
			t.Errorf("%s lists %v, want [%s]", list.cmd, ids, list.want)
		}
	}
}

// listedPacks reads the ids out of a /datapack list line's pack list.
func listedPacks(line protocol.Text) []string {
	links := line.Extra
	if len(links) == 0 {
		links = []protocol.Text{line}
	}
	var ids []string
	for _, l := range links {
		if l.Translate == "chat.square_brackets" {
			ids = append(ids, l.With[0].With[0].Text)
		}
	}
	return ids
}
