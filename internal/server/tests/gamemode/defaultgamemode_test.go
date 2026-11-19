package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestDefaultGameModeSetsTheNextJoin runs /defaultgamemode creative: the next
// player to join starts in creative while the one already online keeps their
// mode, and the runner gets the success line.
func TestDefaultGameModeSetsTheNextJoin(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Alice"))
	alice := connect()
	alice.JoinAs("Alice")

	alice.ChatCommand("defaultgamemode creative")
	var chat []byte
	alice.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			chat = data
		}
	})
	want := servertest.PlaySoundFeedback("commands.defaultgamemode.success", protocol.Text{Translate: "gameMode.creative"})
	if !bytes.Equal(chat, want) {
		t.Errorf("feedback = % X, want % X", chat, want)
	}

	bob := connect()
	bob.JoinAs("Bob")
	if got := srv.PlayerGameMode("Bob"); got != protocol.GameModeCreative {
		t.Errorf("Bob joined in mode %d, want %d (creative)", got, protocol.GameModeCreative)
	}
	if got := srv.PlayerGameMode("Alice"); got != protocol.GameModeSurvival {
		t.Errorf("Alice's mode = %d, want survival: force-gamemode is off", got)
	}
}

// TestForceGameModeOverridesASavedModeOnJoin joins a player whose file saved
// creative: the default replaces it with force-gamemode on, and the saved mode
// stays with it off.
func TestForceGameModeOverridesASavedModeOnJoin(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{true, false} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			t.Parallel()
			cfg := servertest.SurvivalTestConfig()
			cfg.LevelName = t.TempDir()
			cfg.ForceGameMode = force
			servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Returner"), nbt.Compound{
				"playerGameType": nbt.Int(protocol.GameModeCreative),
			})
			srv, _, _ := servertest.JoinAs(t, cfg, "Returner")
			want := int32(protocol.GameModeCreative)
			if force {
				want = protocol.GameModeSurvival
			}
			if got := srv.PlayerGameMode("Returner"); got != want {
				t.Errorf("mode = %d, want %d", got, want)
			}
		})
	}
}
