package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func worldPlugin(t *testing.T, cfg *config.Config) (*server.Server, plugin.Server, func(func())) {
	t.Helper()
	var h plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "world", Plugin: &testPlugin{enable: func(s plugin.Server) error { h = s; return nil }}}})
	servertest.HoldTicks(t, srv)
	return srv, h, func(f func()) {
		t.Helper()
		ran := false
		h.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}
}

func TestPluginWorldDiscovery(t *testing.T) {
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		worlds := h.Worlds()
		if len(worlds) != 3 {
			t.Errorf("worlds=%d", len(worlds))
			return
		}
		if worlds[0].Key() != plugin.MinecraftKey("overworld") {
			t.Error("overworld not first")
		}
		for _, w := range worlds {
			found, ok := h.World(w.Key())
			if !ok || found != w {
				t.Errorf("world %v missing", w.Key())
			}
			if w.MinY()+w.Height() <= w.MinY() {
				t.Error("invalid height")
			}
			if w.Dimension() != w.Key() {
				t.Errorf("dimension type=%v, want %v", w.Dimension(), w.Key())
			}
			if w.Seed() != worlds[0].Seed() {
				t.Error("different seeds")
			}
		}
		if h.Overworld() != worlds[0] {
			t.Error("wrong overworld")
		}
		if _, ok := h.World(plugin.MinecraftKey("absent")); ok {
			t.Error("unknown world exists")
		}
	})
}

// vanilla: Level.getDefaultClockTime, MinecraftServer.setWeatherParameters,
// MinecraftServer.onGameRuleChanged, PlayerList.addWorldborderListener.
func TestPluginWorldControlsAndPackets(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Join("WorldTester")
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	var over, end, nether plugin.World
	run(func() {
		over = h.Overworld()
		end, _ = h.World(plugin.MinecraftKey("the_end"))
		nether, _ = h.World(plugin.MinecraftKey("the_nether"))
	})
	run(func() {
		over.SetDayTime(6000)
		end.SetDayTime(12345)
		nether.SetDayTime(1000)
		if over.DayTime() != 6000 || end.DayTime() != 12345 || nether.DayTime() != 0 {
			t.Error("dimension clocks not independent")
		}
		if over.Time() != end.Time() || over.Time() != nether.Time() {
			t.Error("different world ages")
		}
		if over.Difficulty() != plugin.DifficultyEasy {
			t.Errorf("difficulty=%v", over.Difficulty())
		}
	})
	clocks := map[int32]int64{}
	for range 2 {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayUpdateTime))
		r.ReadI64()
		for n := r.ReadVarInt(); n > 0; n-- {
			id := r.ReadVarInt()
			clocks[id] = r.ReadVarLong()
			r.ReadF32()
			r.ReadF32()
		}
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
	}
	if clocks[registry.MustID("minecraft:world_clock", "minecraft:overworld")] != 6000 || clocks[registry.MustID("minecraft:world_clock", "minecraft:the_end")] != 12345 {
		t.Fatalf("clock packets=%v", clocks)
	}
	var rule plugin.GameRule
	run(func() {
		rule, _ = h.Registries().GameRule(plugin.MinecraftKey("immediate_respawn"))
		end.SetGameRule(rule, 1)
		if over.GameRule(rule) != 1 || nether.GameRule(rule) != 1 {
			t.Error("game rule not shared")
		}
		for _, value := range []int{0, 1} {
			id, _ := h.Registries().GameRule(plugin.MinecraftKey("keep_inventory"))
			over.SetGameRule(id, value)
			if over.GameRule(id) != value {
				t.Errorf("bool rule=%d", over.GameRule(id))
			}
		}
		id, _ := h.Registries().GameRule(plugin.MinecraftKey("random_tick_speed"))
		over.SetGameRule(id, 7)
		if over.GameRule(id) != 7 {
			t.Error("integer rule did not change")
		}
		over.SetGameRule(id, -1)
		if over.GameRule(id) != 0 {
			t.Error("integer rule was not clamped")
		}
		over.SetGameRule(plugin.GameRule(256), 99)
		if over.GameRule(plugin.GameRule(256)) != 0 {
			t.Error("invalid rule read")
		}
	})
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayGameEvent))
	if r.ReadU8() != protocol.GameEventImmediateRespawn || r.ReadF32() != 1 {
		t.Fatal("missing immediate respawn event")
	}
	run(func() { over.SetGameRule(rule, 0) })
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlayGameEvent))
	if r.ReadU8() != protocol.GameEventImmediateRespawn || r.ReadF32() != 0 {
		t.Fatal("missing reset event")
	}
	run(func() {
		end.SetSpawnPoint(plugin.Location{Pos: plugin.Vec3{X: -2.2, Y: 80.9, Z: 3.2}, Yaw: 450, Pitch: 100})
		p := over.SpawnPoint()
		if p.World != end || p.Pos != (plugin.Vec3{X: -2.5, Y: 80, Z: 3.5}) || p.Yaw != 90 || p.Pitch != 90 {
			t.Errorf("spawn=%+v", p)
		}
	})
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySpawnPosition))
	dim := r.ReadString(32767)
	x, y, z := r.ReadPosition()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if dim != "minecraft:the_end" || x != -3 || y != 80 || z != 3 || yaw != 90 || pitch != 90 || r.Err() != nil {
		t.Fatal("incorrect spawn packet")
	}
	run(func() {
		b := over.Border()
		b.SetCenter(2, 3)
		b.SetSize(20, 0)
		b.SetWarningDistance(9)
		b.SetDamagePerBlock(.7)
		if x, z := b.Center(); x != 2 || z != 3 {
			t.Error("border center")
		}
		if b.Size() != 20 || b.WarningDistance() != 9 || b.DamagePerBlock() != .7 {
			t.Error("border settings")
		}
		if !b.Contains(plugin.Vec3{X: -8, Z: -7}) || b.Contains(plugin.Vec3{X: 12, Z: 3}) {
			t.Error("border edges")
		}
		if end.Border().Size() == 20 {
			t.Error("border leaked to another dimension")
		}
	})
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderCenter))
	if r.ReadF64() != 2 || r.ReadF64() != 3 {
		t.Fatal("border center packet")
	}
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderSize))
	if r.ReadF64() != 20 {
		t.Fatal("border size packet")
	}
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderWarningDistance))
	if r.ReadVarInt() != 9 {
		t.Fatal("border warning packet")
	}
	run(func() { over.Border().SetSize(40, time.Second) })
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderLerpSize))
	if r.ReadF64() != 20 || r.ReadF64() != 40 || r.ReadVarLong() != 20 {
		t.Fatal("border lerp packet")
	}
	srv.SetTickFrozen(false)
	servertest.RunTicks(t, srv, 20)
	srv.SetTickFrozen(true)
	run(func() {
		if over.Border().Size() != 40 {
			t.Errorf("lerp size=%v", over.Border().Size())
		}
		over.SetDayTime(-1)
		if over.DayTime() != 0 {
			t.Error("negative clock not clamped")
		}
	})
}

func TestPluginWeatherAndWorldSaveReload(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "1234"
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Join("WeatherTester")
	servertest.HoldTicks(t, srv)
	run(func() {
		h.Overworld().SetWeather(plugin.WeatherRain, 50*time.Second, 0)
		if h.Overworld().Weather() != plugin.WeatherRain {
			t.Error("rain flag")
		}
	})
	var begin bool
	c.ReadTicks(srv, 30, func(id int32, data []byte) bool {
		if id == protocol.IDPlayGameEvent {
			r := protocol.NewReader(data)
			if r.ReadU8() == protocol.GameEventBeginRaining {
				begin = true
			}
		}
		return begin
	})
	if !begin {
		t.Fatal("client did not receive rain")
	}
	run(func() {
		w := h.Overworld()
		w.SetWeather(plugin.WeatherThunder, 50*time.Second, 0)
		if w.Weather() != plugin.WeatherThunder {
			t.Error("thunder flag")
		}
		w.SetWeather(plugin.WeatherClear, 50*time.Second, 0)
		if w.Weather() != plugin.WeatherClear {
			t.Error("clear flag")
		}
	})
	var ended bool
	c.ReadTicks(srv, 30, func(id int32, data []byte) bool {
		if id == protocol.IDPlayGameEvent {
			r := protocol.NewReader(data)
			if r.ReadU8() == protocol.GameEventEndRaining {
				ended = true
			}
		}
		return ended
	})
	if !ended {
		t.Fatal("client did not receive clear weather")
	}
	srv.SetTickFrozen(true)
	run(func() {
		w := h.Overworld()
		w.SetWeather(plugin.WeatherThunder, 0, 0)
		if w.Weather() != plugin.WeatherThunder {
			t.Error("default duration")
		}
		w.SetWeather(plugin.Weather(255), time.Second, 0)
		if w.Weather() != plugin.WeatherThunder {
			t.Error("invalid weather mutated flags")
		}
		w.SetDayTime(8000)
		w.SetSpawnPoint(plugin.Location{Pos: plugin.Vec3{X: 10, Y: 80, Z: 20}, Yaw: 45})
		w.Border().SetSize(100, 0)
		w.Border().SetDamagePerBlock(.7)
		w.Border().SetWarningDistance(8)
		id, _ := h.Registries().GameRule(plugin.MinecraftKey("keep_inventory"))
		w.SetGameRule(id, 1)
		w.Save(true)
	})
	// Check the explicit save before shutdown gets a chance to save again.
	level, err := world.LoadLevel(cfg.LevelName)
	if err != nil || level.Spawn.X != 10 || level.Spawn.Yaw != 45 {
		t.Fatalf("saved spawn: %v, %v", level, err)
	}
	rules, ok, err := world.ReadSavedData(cfg.LevelName, "data/minecraft/game_rules.dat", world.MaxSavedData)
	if err != nil || !ok || rules["minecraft:keep_inventory"] != nbt.Byte(1) {
		t.Fatalf("saved keep_inventory=%v: %v", rules["minecraft:keep_inventory"], err)
	}
	clocks, ok, err := world.ReadSavedData(cfg.LevelName, "data/minecraft/world_clocks.dat", world.MaxSavedData)
	if err != nil || !ok {
		t.Fatalf("saved clocks: %v", err)
	}
	clock, _ := nbt.Get[nbt.Compound](clocks, "minecraft:overworld")
	if clock["total_ticks"] != nbt.Long(8000) {
		t.Errorf("saved clock=%v", clock)
	}
	srv.Shutdown()
	_, h, run = worldPlugin(t, cfg)
	run(func() {
		w := h.Overworld()
		if w.Seed() != 1234 || w.DayTime() < 8000 || w.DayTime() > 8010 || w.Weather() != plugin.WeatherThunder {
			t.Errorf("reloaded seed=%v,time=%v,weather=%v", w.Seed(), w.DayTime(), w.Weather())
		}
		p := w.SpawnPoint()
		if p.Pos != (plugin.Vec3{X: 10.5, Y: 80, Z: 20.5}) || p.Yaw != 45 {
			t.Errorf("reloaded spawn=%+v", p)
		}
		if w.Border().Size() != 100 || w.Border().DamagePerBlock() != .7 || w.Border().WarningDistance() != 8 {
			t.Error("border did not persist")
		}
		id, _ := h.Registries().GameRule(plugin.MinecraftKey("keep_inventory"))
		if w.GameRule(id) != 1 {
			t.Error("rule did not persist")
		}
		w.Save(false)
	})
}
