package redstone

// The sculk shrieker and the amethyst resonance: a sensor's click makes a
// shrieker shriek for ninety ticks, the shriek warns and darkens, the warning
// climbs once per 200 ticks; an activating sensor makes the amethyst beside
// it chime and vibrate in turn.
// vanilla: SculkShriekerBlock, SculkShriekerBlockEntity, WardenSpawnTracker,
// SculkSensorBlock.tryResonateVibration

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// shriekerScene stands a sensor at (4, 64, 4) and a shrieker at (6, 64, 4).
func shriekerScene(t *testing.T, canSummon string) (*server.Server, *servertest.Client) {
	t.Helper()
	return shriekerSceneWithWarning(t, canSummon, 0)
}

func shriekerSceneWithWarning(t *testing.T, canSummon string, warning int32) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	if warning != 0 {
		cfg.LevelName = t.TempDir()
		servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Actor"), nbt.Compound{
			"warden_spawn_tracker": nbt.Compound{
				"warning_level": nbt.Int(warning), "cooldown_ticks": nbt.Int(0), "ticks_since_last_warning": nbt.Int(0),
			},
		})
	}
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	srv.SetBlock(6, 64, 4, servertest.MustState(t, "minecraft:sculk_shrieker",
		map[string]string{"can_summon": canSummon}))
	servertest.HoldTicks(t, srv)
	return srv, c
}

func expectShrieker(t *testing.T, srv *server.Server, canSummon, shrieking string) {
	t.Helper()
	servertest.ExpectBlockNamed(t, srv, 6, 64, 4, "minecraft:sculk_shrieker",
		map[string]string{"shrieking": shrieking, "can_summon": canSummon})
}

// TestASensorsClickMakesAShriekerShriek has the player place a block beside a
// sensor: the sensor clicks, the click travels two blocks to the shrieker,
// which shrieks for ninety ticks.
// vanilla: SculkShriekerBlockEntity.shriek, SculkShriekerBlock.tick
func TestASensorsClickMakesAShriekerShriek(t *testing.T) {
	t.Parallel()
	srv, c := shriekerScene(t, "false")

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 2)
	expectShrieker(t, srv, "false", "false")
	_, counts, _ := servertest.CollectSoundsTicks(c, srv, 4)
	expectShrieker(t, srv, "false", "true")
	if counts[protocol.IDPlayWorldEvent] == 0 {
		t.Error("no world event told the shriek's particles")
	}
	servertest.RunTicks(t, srv, 86)
	expectShrieker(t, srv, "false", "true")
	servertest.RunTicks(t, srv, 2)
	expectShrieker(t, srv, "false", "false")
}

// TestAShriekWarnsAndDarkensWhenItCanSummon: at the end of a shriek the
// player hears the level 1 warning and takes darkness for 260 ticks; a
// shrieker that cannot summon does neither.
// vanilla: SculkShriekerBlockEntity.tryRespond, Warden.applyDarknessAround
func TestAShriekWarnsAndDarkensWhenItCanSummon(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		canSummon string
		survival  bool
		sound     int
		darkness  int
	}{
		{"true", true, 1, 1},
		{"false", true, 0, 0},
	} {
		t.Run(fmt.Sprintf("can_summon=%s,survival=%v", tc.canSummon, tc.survival), func(t *testing.T) {
			t.Parallel()
			srv, c := shriekerScene(t, tc.canSummon)
			placeStoneAt(c, 2)
			servertest.RunTicks(t, srv, 5)
			expectShrieker(t, srv, tc.canSummon, "true")
			if tc.survival {
				c.SendChangeGameMode(protocol.GameModeSurvival)
				c.Settle()
			}
			heard, counts, _ := servertest.CollectSoundsTicks(c, srv, 90)
			if got := servertest.CountSounds(heard, "minecraft:entity.warden.nearby_close", protocol.SoundCategoryHostile); got != tc.sound {
				t.Errorf("heard the level 1 warning %d times, want %d", got, tc.sound)
			}
			if got := counts[protocol.IDPlayUpdateMobEffect]; got != tc.darkness {
				t.Errorf("darkness was told %d times, want %d", got, tc.darkness)
			}
		})
	}
}

// TestSteppingOnAShriekerSetsItOff stands the player on a shrieker.
// vanilla: SculkShriekerBlock.stepOn
func TestSteppingOnAShriekerSetsItOff(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.SetBlock(0, 63, 4, servertest.MustState(t, "minecraft:sculk_shrieker", nil))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 63.5, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	servertest.ExpectBlockNamed(t, srv, 0, 63, 4, "minecraft:sculk_shrieker",
		map[string]string{"shrieking": "true", "can_summon": "false"})
}

// TestAnActivatingSensorMakesAnAmethystBlockResonate puts an amethyst block
// beside a sensor and a second sensor out of the first block's earshot: the
// second one hears the amethyst's resonance, with the frequency of the event
// that lit the first, and the amethyst chimes.
// vanilla: SculkSensorBlock.tryResonateVibration
func TestAnActivatingSensorMakesAnAmethystBlockResonate(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	srv.SetBlock(5, 64, 4, servertest.MustState(t, "minecraft:amethyst_block", nil))
	srv.SetBlock(11, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 1)
	heard, _, _ := servertest.CollectSoundsTicks(c, srv, 2)
	if got := servertest.CountSounds(heard, "minecraft:block.amethyst_block.resonate", protocol.SoundCategoryBlock); got != 1 {
		t.Errorf("the amethyst chimed %d times, want 1", got)
	}
	expectSensor(t, srv, 11, 64, 4, "inactive", "0")
	servertest.RunTicks(t, srv, 5)
	// 15 - floor(15/8 * 6)
	expectSensor(t, srv, 11, 64, 4, "active", "4")
}
