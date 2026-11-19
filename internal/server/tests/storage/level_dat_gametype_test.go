package storage

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestLevelDatRecordsTheDefaultGameModeAndName checks level.dat's GameType and
// LevelName: a new world is named after its folder and carries
// server.properties' gamemode, every start rewrites the mode, and
// /defaultgamemode reaches the next save.
// vanilla: Main.createNewWorldData, DedicatedServer.initServer,
// MinecraftServer.setDefaultGameType
func TestLevelDatRecordsTheDefaultGameModeAndName(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "Survival Land")
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = dir
	cfg.GameMode = "survival"

	levelDat := func(step string, wantMode int32) {
		t.Helper()
		data := servertest.ReadSavedFile(t, filepath.Join(dir, "level.dat"), "Data")
		if got, ok := nbt.Get[nbt.Int](data, "GameType"); !ok || int32(got) != wantMode {
			t.Errorf("%s: GameType = %v, %v; want %d", step, got, ok, wantMode)
		}
		if got, ok := nbt.Get[nbt.String](data, "LevelName"); !ok || string(got) != "Survival Land" {
			t.Errorf("%s: LevelName = %q, %v; want %q", step, got, ok, "Survival Land")
		}
	}

	srv, _ := servertest.Start(t, cfg)
	srv.Shutdown()
	levelDat("new survival world", 0)

	adventure := *cfg
	adventure.GameMode = "adventure"
	srv, _ = servertest.Start(t, &adventure)
	srv.Shutdown()
	levelDat("restart in adventure", 2)

	srv, _ = servertest.Start(t, &adventure)
	srv.RunLine("defaultgamemode spectator")
	srv.Shutdown()
	levelDat("after /defaultgamemode spectator", 3)
}
