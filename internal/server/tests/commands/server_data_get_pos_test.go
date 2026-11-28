package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A function that moves an entity and reads its Pos in the same tick sees the
// new position, whether the read comes from the selector's snapshot or not.
// vanilla: EntityDataAccessor.getData
func TestDataGetPosAfterTeleportInOneFunction(t *testing.T) {
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for path, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/t/function/f.mcfunction": "execute as @e[type=marker] run tp @s 5 70 5\n" +
			"execute as @e[type=marker] store result score #m y run data get entity @s Pos[1]\n" +
			"tp @s 5 71 5\n" +
			"execute store result score #p y run data get entity @s Pos[1]\n",
	} {
		full := filepath.Join(pack, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Mover", servertest.OpsListing(t, "Mover"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("scoreboard objectives add y dummy")
	c.ChatCommand("summon marker 0 64 0")
	c.ChatCommand("function t:f")
	c.Settle()
	for holder, want := range map[string]int32{"#m": 70, "#p": 71} {
		if got, _ := srv.ScoreOf(holder, "y"); got != want {
			t.Errorf("%s y = %d, want %d", holder, got, want)
		}
	}
}
