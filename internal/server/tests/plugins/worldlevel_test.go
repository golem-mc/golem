package plugins_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

func createNamedWorld(t *testing.T, run func(func()), h plugin.Server, path string) (w plugin.World) {
	t.Helper()
	run(func() {
		var err error
		opts := flatOptions(&flatGen{})
		opts.Key = plugin.Key{Namespace: "test", Path: path}
		if w, err = h.CreateWorld(opts); err != nil {
			t.Errorf("CreateWorld %s: %v", path, err)
		}
	})
	if w == nil {
		t.FailNow()
	}
	return w
}

// vanilla: ServerLevel.advanceWeatherCycle, MinecraftServer.setDifficulty
func TestPluginWorldsKeepOwnLevel(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	a := createNamedWorld(t, run, h, "a")
	b := createNamedWorld(t, run, h, "b")
	run(func() {
		a.SetDifficulty(plugin.DifficultyHard)
		b.SetDifficulty(plugin.DifficultyPeaceful)
		a.SetWeather(plugin.WeatherThunder, 100*time.Second, 100*time.Second)
		a.SetDayTime(6000)
		b.SetDayTime(18000)
	})
	servertest.RunTicks(t, srv, 5)
	run(func() {
		ow := h.Overworld()
		if a.Difficulty() != plugin.DifficultyHard || b.Difficulty() != plugin.DifficultyPeaceful || ow.Difficulty() == plugin.DifficultyPeaceful {
			t.Errorf("difficulties a=%v b=%v overworld=%v", a.Difficulty(), b.Difficulty(), ow.Difficulty())
		}
		if a.Weather() != plugin.WeatherThunder || b.Weather() != plugin.WeatherClear || ow.Weather() != plugin.WeatherClear {
			t.Errorf("weather a=%v b=%v overworld=%v", a.Weather(), b.Weather(), ow.Weather())
		}
		if a.DayTime() < 6000 || a.DayTime() > 6010 || b.DayTime() < 18000 || b.DayTime() > 18010 {
			t.Errorf("day time a=%d b=%d", a.DayTime(), b.DayTime())
		}
		if ow.DayTime() >= 6000 && ow.DayTime() < 18010 {
			t.Errorf("overworld day time %d followed a plugin world", ow.DayTime())
		}
		if a.Time() == 0 || a.Time() > ow.Time() {
			t.Errorf("a's age %d, overworld's %d", a.Time(), ow.Time())
		}
	})
	servertest.RunTicks(t, srv, 1)
	run(func() {
		if err := h.UnloadWorld(a, true); err != nil {
			t.Error(err)
		}
		if err := h.UnloadWorld(b, true); err != nil {
			t.Error(err)
		}
	})
	a = createNamedWorld(t, run, h, "a")
	b = createNamedWorld(t, run, h, "b")
	run(func() {
		if a.Difficulty() != plugin.DifficultyHard || b.Difficulty() != plugin.DifficultyPeaceful {
			t.Errorf("reloaded difficulties a=%v b=%v", a.Difficulty(), b.Difficulty())
		}
		if a.Weather() != plugin.WeatherThunder || a.RainDuration() < 90*time.Second || b.Weather() != plugin.WeatherClear {
			t.Errorf("reloaded weather a=%v (%v) b=%v", a.Weather(), a.RainDuration(), b.Weather())
		}
		if a.DayTime() < 6000 || a.DayTime() > 6100 || b.DayTime() < 18000 || b.DayTime() > 18100 {
			t.Errorf("reloaded day time a=%d b=%d", a.DayTime(), b.DayTime())
		}
		if a.Time() == 0 {
			t.Error("reloaded age is 0")
		}
	})
}

// vanilla: ServerLevel.advanceWeatherCycle, PlayerList.broadcastAll(packet, dimension)
func TestPluginWorldPlayersHearOwnLevel(t *testing.T) {
	t.Parallel()
	srv, h, run, c := worldEffectsPlugin(t)
	srv.SetOpsFile(filepath.Join(t.TempDir(), "ops.json"))
	srv.SetTickFrozen(false) // the weather and the clocks only run while the game does
	a := createNamedWorld(t, run, h, "a")
	b := createNamedWorld(t, run, h, "b")
	srv.MovePlayerToDimension("Effects", "test:a", 8.5, 4, 8.5)
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(func(int32, []byte) {})

	level := func(id int32, data []byte) bool {
		return id == protocol.IDPlayDifficulty || id == protocol.IDPlayGameEvent
	}
	run(func() {
		b.SetDifficulty(plugin.DifficultyHard)
		b.SetWeather(plugin.WeatherRain, 100*time.Second, 0)
	})
	c.ExpectNoneWithinTicks(srv, 30, level)

	run(func() { a.SetDifficulty(plugin.DifficultyPeaceful) })
	data := c.ExpectWithinTicks(srv, 1, protocol.IDPlayDifficulty)
	if len(data) < 1 || data[0] != 0 {
		t.Errorf("Change Difficulty = % X, want peaceful", data)
	}
	run(func() { a.SetWeather(plugin.WeatherRain, 100*time.Second, 0) })
	began := false
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id == protocol.IDPlayGameEvent && len(data) > 0 && data[0] == protocol.GameEventBeginRaining {
			began = true
		}
		return began
	})
	if !began {
		t.Error("the player in the raining world never got Begin Raining")
	}
}

// vanilla: WeatherCommand, TimeCommand, DifficultyCommand
func TestPluginWorldCommandsScopedToWorld(t *testing.T) {
	t.Parallel()
	srv, h, run, c := worldEffectsPlugin(t)
	srv.SetOpsFile(filepath.Join(t.TempDir(), "ops.json"))
	srv.SetTickFrozen(false)
	a := createNamedWorld(t, run, h, "a")
	b := createNamedWorld(t, run, h, "b")
	run(func() {
		h.Overworld().SetDifficulty(plugin.DifficultyNormal)
		a.SetDifficulty(plugin.DifficultyNormal)
		b.SetDifficulty(plugin.DifficultyNormal)
		h.Overworld().SetDayTime(1000)
		a.SetDayTime(1000)
		b.SetDayTime(1000)
		if p, ok := h.PlayerNamed("Effects"); ok {
			p.SetOpLevel(4)
		}
	})
	srv.MovePlayerToDimension("Effects", "test:a", 8.5, 4, 8.5)
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(func(int32, []byte) {})

	c.ChatCommand("weather rain 100s")
	c.ChatCommand("time set 6000")
	c.ChatCommand("difficulty peaceful")
	c.Settle()

	run(func() {
		ow := h.Overworld()
		if a.Weather() != plugin.WeatherRain || b.Weather() != plugin.WeatherClear || ow.Weather() != plugin.WeatherClear {
			t.Errorf("weather a=%v b=%v overworld=%v", a.Weather(), b.Weather(), ow.Weather())
		}
		if a.DayTime() < 6000 || a.DayTime() > 6020 {
			t.Errorf("a day time = %d, want ~6000", a.DayTime())
		}
		if b.DayTime() >= 6000 || ow.DayTime() >= 6000 {
			t.Errorf("other worlds followed /time set: b=%d overworld=%d", b.DayTime(), ow.DayTime())
		}
		if a.Difficulty() != plugin.DifficultyPeaceful || b.Difficulty() != plugin.DifficultyNormal || ow.Difficulty() != plugin.DifficultyNormal {
			t.Errorf("difficulties a=%v b=%v overworld=%v", a.Difficulty(), b.Difficulty(), ow.Difficulty())
		}
	})

	// Running the same commands in the overworld touches only the overworld group.
	srv.MovePlayerToDimension("Effects", "minecraft:overworld", 0.5, 65, 0.5)
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(func(int32, []byte) {})

	c.ChatCommand("weather thunder 100s")
	c.ChatCommand("time set 18000")
	c.ChatCommand("difficulty hard")
	c.Settle()

	run(func() {
		ow := h.Overworld()
		if ow.Weather() != plugin.WeatherThunder || a.Weather() != plugin.WeatherRain || b.Weather() != plugin.WeatherClear {
			t.Errorf("after overworld /weather: ow=%v a=%v b=%v", ow.Weather(), a.Weather(), b.Weather())
		}
		if ow.DayTime() < 18000 || a.DayTime() >= 18000 {
			t.Errorf("after overworld /time: ow=%d a=%d", ow.DayTime(), a.DayTime())
		}
		if ow.Difficulty() != plugin.DifficultyHard || a.Difficulty() != plugin.DifficultyPeaceful || b.Difficulty() != plugin.DifficultyNormal {
			t.Errorf("after overworld /difficulty: ow=%v a=%v b=%v", ow.Difficulty(), a.Difficulty(), b.Difficulty())
		}
	})
}

// vanilla: Player.hurtServer, DamageCommand.damage, SummonCommand
func TestPluginWorldDifficultyScalesDamage(t *testing.T) {
	t.Parallel()
	srv, h, run, c := worldEffectsPlugin(t)
	srv.SetOpsFile(filepath.Join(t.TempDir(), "ops.json"))
	srv.SetTickFrozen(false)
	a := createNamedWorld(t, run, h, "a")
	b := createNamedWorld(t, run, h, "b")
	run(func() {
		h.Overworld().SetDifficulty(plugin.DifficultyNormal)
		a.SetDifficulty(plugin.DifficultyHard)
		b.SetDifficulty(plugin.DifficultyPeaceful)
		if p, ok := h.PlayerNamed("Effects"); ok {
			p.SetOpLevel(4)
			p.SetGameMode(plugin.Survival)
		}
	})
	moveAndDrain := func(dim string, x, y, z float64) {
		srv.MovePlayerToDimension("Effects", dim, x, y, z)
		servertest.RunTicks(t, srv, 65) // past post-teleport spawn invulnerability
		c.SettleAndCollect(func(int32, []byte) {})
	}

	// Hard plugin world: 4 * 1.5 = 6 damage -> health 14.
	moveAndDrain("test:a", 8.5, 4, 8.5)
	srv.SetHealth("Effects", 20)
	c.ChatCommand("damage Effects 4 minecraft:explosion")
	c.Settle()
	if got := srv.PlayerHealth("Effects"); got != 14 {
		t.Errorf("hard plugin world health = %v, want 14", got)
	}

	// Peaceful plugin world: explosion scales to 0 -> health stays 20, and
	// summoning a hostile mob is refused even though the overworld is normal.
	moveAndDrain("test:b", 8.5, 4, 8.5)
	srv.SetHealth("Effects", 20)
	c.ChatCommand("damage Effects 4 minecraft:explosion")
	servertest.ExpectCommandError(t, c, "commands.damage.invulnerable")
	if got := srv.PlayerHealth("Effects"); got != 20 {
		t.Errorf("peaceful plugin world health = %v, want 20", got)
	}
	c.ChatCommand("summon minecraft:zombie 8.5 4 8.5")
	servertest.ExpectCommandError(t, c, "commands.summon.failed.peaceful")

	// Overworld (normal): 4 * 1.0 = 4 damage -> health 16.
	moveAndDrain("minecraft:overworld", 0.5, 65, 0.5)
	srv.SetHealth("Effects", 20)
	c.ChatCommand("damage Effects 4 minecraft:explosion")
	c.Settle()
	if got := srv.PlayerHealth("Effects"); got != 16 {
		t.Errorf("normal overworld health = %v, want 16", got)
	}
}
