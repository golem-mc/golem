package commands

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestReloadWhileTickFunctionsRun checks that /reload from several command
// goroutines at once, while another lists the packs and the loop runs the
// minecraft:tick tag, leaves the tick tag running and the selection as it
// was. Run it with -race: the reload swaps what the tick goroutine reads.
// vanilla: MinecraftServer.reloadResources, ServerFunctionManager.tick
func TestReloadWhileTickFunctionsRun(t *testing.T) {
	t.Parallel()
	cfg := packWorld(t)
	pack := filepath.Join(cfg.LevelName, "datapacks", "a", "data")
	writeFile(t, filepath.Join(pack, "x", "function", "count.mcfunction"), "scoreboard players add #t n 1\n")
	writeFile(t, filepath.Join(pack, "minecraft", "tags", "function", "tick.json"), `{"values":["x:count"]}`)
	srv, _ := joinPacks(t, cfg)
	srv.RunLine("scoreboard objectives add n dummy")
	before := enabledPacks(t, srv)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 25 {
				srv.RunLine("reload")
			}
		})
	}
	wg.Go(func() {
		for range 25 {
			srv.RunLine("datapack list")
		}
	})
	wg.Wait()

	servertest.HoldTicks(t, srv)
	start := tickScore(t, srv)
	servertest.RunTicks(t, srv, 5)
	if got := tickScore(t, srv); got != start+5 {
		t.Errorf("#t after 5 ticks = %d, want %d", got, start+5)
	}
	if after := enabledPacks(t, srv); !slices.Equal(after, before) {
		t.Errorf("enabled packs after the reloads = %v, want %v", after, before)
	}
}

// tickScore is #t's score in n.
func tickScore(t *testing.T, srv *server.Server) int64 {
	t.Helper()
	out := srv.RunLine("scoreboard players get #t n")
	if len(out) != 1 || out[0].Translate != "commands.scoreboard.players.get.success" {
		t.Fatalf("scoreboard players get #t n = %+v", out)
	}
	return number(out[0].With[1])
}

// enabledPacks is the ids /datapack list enabled names.
func enabledPacks(t *testing.T, srv *server.Server) []string {
	t.Helper()
	out := srv.RunLine("datapack list enabled")
	if len(out) != 1 || len(out[0].With) < 2 {
		t.Fatalf("datapack list enabled = %+v", out)
	}
	return packIDs(t, out[0].With[1])
}
