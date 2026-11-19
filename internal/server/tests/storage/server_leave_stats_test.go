package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLeavingTheGameCountsLeaveGame: a player's departure adds one to
// leave_game, and the count is in the stats file the departure saves.
// vanilla: PlayerList.remove
func TestLeavingTheGameCountsLeaveGame(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	_, connect := servertest.Start(t, cfg)
	for want := 1; want <= 2; want++ {
		c := connect().Join("Leaver")
		c.Disconnect()

		data, err := os.ReadFile(filepath.Join(cfg.LevelName, "players", "stats", protocol.OfflineUUID("Leaver").String()+".json"))
		if err != nil {
			t.Fatalf("read the stats file: %v", err)
		}
		var file struct {
			Stats map[string]map[string]int32 `json:"stats"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("parse the stats file: %v", err)
		}
		if got := file.Stats["minecraft:custom"]["minecraft:leave_game"]; got != int32(want) {
			t.Errorf("leave_game after %d departures = %d, want %d", want, got, want)
		}
	}
}
