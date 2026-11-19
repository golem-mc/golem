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

// TestChangeGameModeOutOfRangeIsSurvival has an operator send an id past
// spectator: the packet's codec maps any out-of-range id to survival, so the
// request is handled as a normal switch to survival, feedback and all, rather
// than refused as a decode failure.
func TestChangeGameModeOutOfRangeIsSurvival(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Alice"))
	playerUUID := c.Login("Alice")
	c.Configure()
	c.EnterWorld("Alice")

	c.SendChangeGameMode(4)
	c.ExpectModePackets(playerUUID, protocol.GameModeSurvival, true)

	if got := srv.PlayerGameMode("Alice"); got != protocol.GameModeSurvival {
		t.Errorf("Alice's game mode = %d, want %d (survival)", got, protocol.GameModeSurvival)
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

// TestJoinSendsOperatorTheirPermissionLevel checks the entity event the join
// carries for an operator: statuses 24 to 28 are the permission levels 0 to
// 4, and a player on ops.json is sent their listed level. The client gates
// its F3+F4 switcher on this, so an operator who never hears it is left
// believing they have no permission.
func TestJoinSendsOperatorTheirPermissionLevel(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Alice"))
	c.Login("Alice")
	c.Configure()

	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayEntityEvent))
	r.ReadI32() // the player's own entity the event is played on
	if got, want := r.ReadU8(), uint8(protocol.EntityStatusOpLevelBase+cfg.OpPermissionLevel); got != want {
		t.Errorf("entity event status = %d, want %d (permission level %d)", got, want, cfg.OpPermissionLevel)
	}
}
