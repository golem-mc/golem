package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestWardenWarningSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	srv.SetBlock(6, 64, 4, servertest.MustState(t, "minecraft:sculk_shrieker",
		map[string]string{"can_summon": "true"}))
	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 100)
	expectShrieker(t, srv, "true", "false")
	srv.Shutdown()

	readTracker := func() rules.WardenSpawnTracker {
		t.Helper()
		root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Actor"))
		tag, ok := nbt.Get[nbt.Compound](root, "warden_spawn_tracker")
		if !ok {
			t.Fatal("saved player has no warden_spawn_tracker")
		}
		for _, field := range []string{"ticks_since_last_warning", "warning_level", "cooldown_ticks"} {
			if _, ok := nbt.Get[nbt.Int](tag, field); !ok {
				t.Fatalf("saved tracker field %s = %v, want an int", field, tag[field])
			}
		}
		return rules.WardenSpawnTrackerFromTag(tag)
	}
	before := readTracker()
	if before.WarningLevel != 1 || before.CooldownTicks <= 0 || before.TicksSinceLastWarning+before.CooldownTicks != 200 {
		t.Fatalf("saved warning = %+v, want level 1 with the remaining cooldown", before)
	}

	restarted := *cfg
	again, reconnect := servertest.Start(t, &restarted)
	a := reconnect().Join("Actor")
	servertest.HoldTicks(t, again)
	if !again.SaveAll(false) {
		t.Fatal("could not save the rejoined player")
	}
	after := readTracker()
	if after.WarningLevel != 1 || after.TicksSinceLastWarning < before.TicksSinceLastWarning ||
		after.CooldownTicks <= 8 || after.CooldownTicks > before.CooldownTicks ||
		after.TicksSinceLastWarning+after.CooldownTicks != 200 {
		t.Fatalf("restarted warning = %+v, saved %+v; want the warning and timers to resume", after, before)
	}

	// A second vibration is refused until the saved cooldown runs out.
	start := again.TicksRun()
	a.BreakBlock(2, 64, 4)
	a.Settle()
	servertest.RunTicks(t, again, 8)
	expectShrieker(t, again, "true", "false")
	// The break's click leaves the sensor deaf through its active and
	// cooldown phases (40 ticks), and the rejoin ran on the free loop, so
	// under load less warning cooldown than that may be left.
	servertest.UntilTicks(t, again, 40, "the sensor listens again", func() bool {
		return registry.StateValue(again.BlockAt(4, 64, 4), registry.PropSculkSensorPhase) == "inactive"
	})
	servertest.RunTicks(t, again, after.CooldownTicks-int(again.TicksRun()-start))
	placeStoneAt(a, 2)
	servertest.RunTicks(t, again, 5)
	expectShrieker(t, again, "true", "true")
	heard, _, _ := servertest.CollectSoundsTicks(a, again, 90)
	if got := servertest.CountSounds(heard, "minecraft:entity.warden.nearby_closer", protocol.SoundCategoryHostile); got != 1 {
		t.Errorf("heard level 2 warning %d times after restart, want 1", got)
	}
}
