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

// TestDefaultGameModeIsForcedOnOnlinePlayers runs /defaultgamemode with
// force-gamemode on: the player already online is moved too.
func TestDefaultGameModeIsForcedOnOnlinePlayers(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ForceGameMode = true
	srv, c, _ := servertest.JoinAs(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.ChatCommand("defaultgamemode adventure")
	c.Settle()
	if got := srv.PlayerGameMode("Alice"); got != protocol.GameModeAdventure {
		t.Errorf("Alice's mode = %d, want %d (adventure)", got, protocol.GameModeAdventure)
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

// stopSound is a Stop Sound packet as a client reads it.
type stopSound struct {
	flags  byte
	source int32
	sound  string
}

func decodeStopSound(t *testing.T, data []byte) stopSound {
	t.Helper()
	r := protocol.NewReader(data)
	var s stopSound
	s.flags = r.ReadU8()
	if s.flags&1 != 0 {
		s.source = r.ReadVarInt()
	}
	if s.flags&2 != 0 {
		s.sound = r.ReadString(32767)
	}
	if err := r.Err(); err != nil || r.Remaining() != 0 {
		t.Fatalf("stop sound packet: %v, %d bytes left", err, r.Remaining())
	}
	return s
}

// TestStopSoundCommand runs each form of /stopsound and checks the flags the
// Stop Sound packet carries and the feedback key.
func TestStopSoundCommand(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	for _, tc := range []struct {
		line   string
		flags  byte
		source int32
		sound  string
		key    string
		with   []protocol.Text
	}{
		{"stopsound @s", 0, 0, "", "commands.stopsound.success.sourceless.any", nil},
		{"stopsound @s * minecraft:entity.cow.ambient", 2, 0, "minecraft:entity.cow.ambient",
			"commands.stopsound.success.sourceless.sound", []protocol.Text{{Text: "minecraft:entity.cow.ambient"}}},
		{"stopsound @s music", 1, protocol.SoundCategoryMusic, "", "commands.stopsound.success.source.any",
			[]protocol.Text{{Text: "music"}}},
		{"stopsound @s ui minecraft:ui.button.click", 3, protocol.SoundCategoryUI, "minecraft:ui.button.click",
			"commands.stopsound.success.source.sound",
			[]protocol.Text{{Text: "minecraft:ui.button.click"}, {Text: "ui"}}},
	} {
		var packet, chat []byte
		c.ChatCommand(tc.line)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayStopSound:
				packet = data
			case protocol.IDPlaySystemChat:
				chat = data
			}
		})
		if packet == nil {
			t.Errorf("%q sent no Stop Sound packet", tc.line)
			continue
		}
		got := decodeStopSound(t, packet)
		if got.flags != tc.flags || got.source != tc.source || got.sound != tc.sound {
			t.Errorf("%q: flags %d source %d sound %q, want %d %d %q",
				tc.line, got.flags, got.source, got.sound, tc.flags, tc.source, tc.sound)
		}
		if want := servertest.PlaySoundFeedback(tc.key, tc.with...); !bytes.Equal(chat, want) {
			t.Errorf("%q feedback = % X, want % X", tc.line, chat, want)
		}
	}
}
