package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestHardcoreWorldFlagsLoginHardAndSpectatesOnRespawn checks the hardcore
// world: Login (play) carries the flag, the difficulty is hard and cannot be
// changed, a death respawns into spectator, and level.dat keeps the flag.
// vanilla: PlayerList.placeNewPlayer, MinecraftServer.setDifficulty,
// ServerGamePacketListenerImpl.handleClientCommand
func TestHardcoreWorldFlagsLoginHardAndSpectatesOnRespawn(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.Hardcore = true
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Hardy"))
	if got := srv.Difficulty(); got != 3 {
		t.Fatalf("hardcore difficulty = %d, want 3 (hard)", got)
	}
	c := connect()
	c.Login("Hardy")
	c.Configure()
	var entityID int32
	var hardcore bool
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayLogin && entityID == 0 {
			r := protocol.NewReader(data)
			entityID = r.ReadI32()
			hardcore = r.ReadBool()
		}
	})
	if !hardcore {
		t.Error("login hardcore flag = false, want true")
	}
	c.MarkLoaded()

	c.ChatCommand("difficulty peaceful")
	c.ExpectComponentFeedback("commands.difficulty.success",
		protocol.Text{Translate: "options.difficulty.peaceful"})
	event := protocol.NewReader(c.ExpectPlay(protocol.IDPlayDifficulty))
	if got := event.ReadU8(); got != 3 {
		t.Errorf("change difficulty = %d, want 3: a hardcore world stays hard", got)
	}
	if got := srv.Difficulty(); got != 3 {
		t.Errorf("difficulty after /difficulty peaceful = %d, want 3", got)
	}

	servertest.KillAndRespawn(t, c, entityID, "Hardy")
	c.Settle()
	if got := srv.PlayerGameMode("Hardy"); got != protocol.GameModeSpectator {
		t.Errorf("game mode after respawn = %d, want spectator", got)
	}

	srv.Shutdown()
	root := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "level.dat"), "Data")
	settings, _ := nbt.Get[nbt.Compound](root, "difficulty_settings")
	if got, ok := nbt.Get[nbt.Byte](settings, "hardcore"); !ok || got != 1 {
		t.Errorf("level.dat hardcore = %v, %v; want 1", got, ok)
	}
}
