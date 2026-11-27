package advancements

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestOldAdvancementFileKeepsRenamedAdvancement: an advancements file saved
// at DataVersion 4189 holds nether/use_lodestone, which 26.3 calls
// adventure/use_lodestone.
// vanilla: DataFixers v4311, PlayerAdvancements.load
func TestOldAdvancementFileKeepsRenamedAdvancement(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	path := filepath.Join(cfg.LevelName, "players", "advancements", protocol.OfflineUUID("Tester").String()+".json")
	file := `{"minecraft:nether/use_lodestone": {"criteria": {"use_lodestone": "2026-10-31 12:34:56 +0100"}, "done": true}, "DataVersion": 4189}`
	if err := advancement.WriteFile(path, []byte(file)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	srv, c := joinOp(t, cfg, "Tester")
	servertest.HoldTicks(t, srv)
	c.Settle()
	c.ChatCommand("execute if entity @s[advancements={minecraft:adventure/use_lodestone=true}]")
	count := int64(1)
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &count})
}
