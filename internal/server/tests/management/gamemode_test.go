package management

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: DedicatedServer.setDifficulty, setGameMode, setForceGameMode,
// MinecraftServer.enforceGameTypeForPlayers
func TestServerSettingsDifficultyAndGameMode(t *testing.T) {
	t.Parallel()
	cfg := servertest.ManagementConfig(offline())
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))
	c := servertest.ManagementClientFor(t, srv)
	bob := connect()
	bob.JoinAs("Bob")

	sameJSON(t, "difficulty/set", result(t, c, "serversettings/difficulty/set", []any{"hard"}), `"hard"`)
	if got := protocol.NewReader(bob.ExpectWithinTicks(srv, 20, protocol.IDPlayDifficulty)).ReadU8(); got != 3 {
		t.Errorf("change difficulty = %d, want 3 (hard)", got)
	}
	props, err := os.ReadFile(srv.ConfigFile())
	if err != nil {
		t.Fatalf("read server.properties: %v", err)
	}
	if !strings.Contains("\n"+string(props), "\ndifficulty=hard\n") {
		t.Error("server.properties after difficulty/set lacks difficulty=hard")
	}
	sameJSON(t, "difficulty", result(t, c, "serversettings/difficulty", nil), `"hard"`)
	// vanilla: Codec.stringResolver matches the wire name exactly.
	errorData(t, c, "serversettings/difficulty/set", []any{"HARD"}, -32602, "Unknown element name:HARD")

	gameModeChange := func(id int32, data []byte) bool {
		return id == protocol.IDPlayGameEvent && protocol.NewReader(data).ReadU8() == protocol.GameEventChangeGameMode
	}
	sameJSON(t, "game_mode/set", result(t, c, "serversettings/game_mode/set", []any{"creative"}), `"creative"`)
	// Without force-gamemode the default only applies to new players.
	bob.ExpectNoneWithinTicks(srv, 5, gameModeChange)

	sameJSON(t, "force_game_mode/set", result(t, c, "serversettings/force_game_mode/set", []any{true}), `true`)
	var mode float32 = -1
	if !bob.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		if !gameModeChange(id, data) {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadU8()
		mode = r.ReadF32()
		return true
	}) {
		t.Fatal("no change game mode event within 20 ticks of force_game_mode/set true")
	}
	if mode != 1 {
		t.Errorf("change game mode to %v, want 1 (creative)", mode)
	}

	// The new default reaches level.dat, which the next save writes.
	srv.RunLine("save-all")
	level := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "level.dat"), "Data")
	if got, ok := nbt.Get[nbt.Int](level, "GameType"); !ok || got != 1 {
		t.Errorf("level.dat GameType after game_mode/set = %v, %v; want 1 (creative)", got, ok)
	}

	// /defaultgamemode changes the world's default, not the properties'.
	srv.RunLine("defaultgamemode adventure")
	sameJSON(t, "game_mode after /defaultgamemode", result(t, c, "serversettings/game_mode", nil), `"creative"`)
}
