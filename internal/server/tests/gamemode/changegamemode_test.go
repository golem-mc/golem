package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestChangeGameModeAsOperator has an operator send the Change Game Mode
// packet: the server applies it the way /gamemode does, so the game event,
// abilities, tab-list update and "Set own game mode to ..." feedback all
// arrive, and the mode is changed.
func TestChangeGameModeAsOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Alice"))
	playerUUID := c.Login("Alice")
	c.Configure()
	c.EnterWorld("Alice")

	c.SendChangeGameMode(protocol.GameModeCreative)
	c.ExpectModePackets(playerUUID, protocol.GameModeCreative, true)

	if got := srv.PlayerGameMode("Alice"); got != protocol.GameModeCreative {
		t.Errorf("Alice's game mode = %d, want %d (creative)", got, protocol.GameModeCreative)
	}
}

// TestChangeGameModeWithoutPermissionIsIgnored has a player who is not an
// operator send the Change Game Mode packet: vanilla refuses it, so
// the mode is left alone and nothing about it goes out to the client.
func TestChangeGameModeWithoutPermissionIsIgnored(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Visitor")

	c.SendChangeGameMode(protocol.GameModeCreative)
	var told []int32
	c.SettleAndCollect(func(id int32, data []byte) { told = append(told, id) })
	for _, id := range told {
		switch id {
		case protocol.IDPlayGameEvent, protocol.IDPlayAbilities,
			protocol.IDPlayPlayerInfoUpdate, protocol.IDPlaySystemChat:
			t.Errorf("a non-operator's game mode request produced packet 0x%02X", id)
		}
	}
	if got := srv.PlayerGameMode("Visitor"); got != protocol.GameModeSurvival {
		t.Errorf("Visitor's game mode = %d, want %d (survival)", got, protocol.GameModeSurvival)
	}
}
