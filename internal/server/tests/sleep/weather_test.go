package sleep

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// expectWeatherEvent reads packets until a Game Event of the given kind
// arrives or n ticks pass, answering keep-alives and stepping over
// the background traffic the world makes around the player. The other
// weather events on the way are taken as they come, since the sky announces
// itself in a run of events rather than one.
func expectWeatherEvent(srv *server.Server, t *testing.T, c *servertest.Client, n int, event uint8) servertest.WeatherEvent {
	t.Helper()
	var found servertest.WeatherEvent
	if c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayGameEvent {
			r := protocol.NewReader(data)
			got := servertest.WeatherEvent{Event: r.ReadU8(), Value: r.ReadF32()}
			if got.Event == event {
				found = got
				return true
			}
		}
		return false
	}) {
		return found
	}
	t.Fatalf("no game event %d arrived within %d ticks", event, n)
	return servertest.WeatherEvent{}
}

// readWeather reads packets until n weather events (begin, end, rain level,
// thunder level) have arrived, in the order they came.
func readWeather(srv *server.Server, t *testing.T, c *servertest.Client, n int) []servertest.WeatherEvent {
	t.Helper()
	var events []servertest.WeatherEvent
	if c.ReadTicks(srv, 100, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayGameEvent {
			return false
		}
		r := protocol.NewReader(data)
		e := servertest.WeatherEvent{Event: r.ReadU8(), Value: r.ReadF32()}
		switch e.Event {
		case protocol.GameEventBeginRaining, protocol.GameEventEndRaining,
			protocol.GameEventRainLevel, protocol.GameEventThunderLevel:
			events = append(events, e)
		}
		return len(events) == n
	}) {
		return events
	}
	t.Fatalf("got %d weather events, want %d: %v", len(events), n, events)
	return nil
}

// fadeLevels is what a level steps through on its way from `from` to 0 or
// 1: vanilla's advanceWeatherCycle moves it by 0.01f a tick, in float32, and
// clamps it.
func fadeLevels(from float32, up bool) []float32 {
	var levels []float32
	for l := from; ; {
		if up {
			l += 0.01
		} else {
			l -= 0.01
		}
		l = min(max(l, 0), 1)
		levels = append(levels, l)
		if l == 0 || l == 1 {
			return levels
		}
	}
}

// fadeEvents is the events a fade sends, tick by tick: the rain level, the
// thunder level when it moves too, and the begin or end with both levels
// again on the tick the rain crosses 0.2 (vanilla's Level.isRaining).
func fadeEvents(rain []float32, thunder []float32, up bool) []servertest.WeatherEvent {
	var events []servertest.WeatherEvent
	was := !up
	for i, r := range rain {
		events = append(events, servertest.WeatherEvent{Event: protocol.GameEventRainLevel, Value: r})
		var th float32
		if thunder != nil {
			th = thunder[i]
			events = append(events, servertest.WeatherEvent{Event: protocol.GameEventThunderLevel, Value: th})
		}
		if now := float64(r) > 0.2; now != was {
			flip := uint8(protocol.GameEventEndRaining)
			if now {
				flip = protocol.GameEventBeginRaining
			}
			events = append(events, servertest.WeatherEvent{Event: flip, Value: 0},
				servertest.WeatherEvent{Event: protocol.GameEventRainLevel, Value: r},
				servertest.WeatherEvent{Event: protocol.GameEventThunderLevel, Value: th})
			was = now
		}
	}
	return events
}

// tickWeather runs n weather ticks by hand.
func tickWeather(srv *server.Server, n int) {
	for range n {
		srv.TickWeather()
	}
}

func compareWeather(t *testing.T, got, want []servertest.WeatherEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d weather events, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("weather event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestRainFadesInAndOut checks that /weather rain and /weather clear move the
// rain level 0.01 a tick, one Game Event per tick, with the begin and end
// events where the level crosses 0.2. The values pinned below are what a
// vanilla server sent for the same commands.
func TestRainFadesInAndOut(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	srv.StopTick()

	c.ChatCommand("weather rain 6000")
	c.ExpectCommandFeedback("commands.weather.set.rain")
	if !srv.Raining() {
		t.Fatal("the world is not raining after /weather rain")
	}
	up := fadeLevels(0, true)
	for i, want := range map[int]float32{0: 0.01, 4: 0.049999997, 19: 0.20000002, 99: 0.99999934, 100: 1} {
		if up[i] != want {
			t.Fatalf("fade step %d = %v, vanilla sent %v", i, up[i], want)
		}
	}
	tickWeather(srv, len(up)+5)
	compareWeather(t, readWeather(srv, t, c, len(up)+3), fadeEvents(up, nil, true))
	// The loop is stopped, so the barrier alone reads what is in flight.
	if extra := servertest.DrainWeather(c, srv, 0); len(extra) != 0 {
		t.Errorf("a settled rain sent %v", extra)
	}

	c.ChatCommand("weather clear 6000")
	c.ExpectCommandFeedback("commands.weather.set.clear")
	down := fadeLevels(1, false)
	if down[80] != 0.19000068 || down[99] != 6.7055225e-07 {
		t.Fatalf("fade down steps 80, 99 = %v, %v; vanilla sent 0.19000068, 6.7055225e-07", down[80], down[99])
	}
	tickWeather(srv, len(down)+5)
	compareWeather(t, readWeather(srv, t, c, len(down)+3), fadeEvents(down, nil, false))
	if srv.Raining() {
		t.Error("the world is still raining after /weather clear")
	}
	c.Disconnect()
}

// TestThunderFadesWithTheRain checks /weather thunder from a clear sky: the
// rain and thunder levels climb together, and fall together after a clear.
func TestThunderFadesWithTheRain(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	srv.StopTick()

	c.ChatCommand("weather thunder 6000")
	c.ExpectCommandFeedback("commands.weather.set.thunder")
	up := fadeLevels(0, true)
	tickWeather(srv, len(up))
	compareWeather(t, readWeather(srv, t, c, 2*len(up)+3), fadeEvents(up, up, true))
	if !srv.Thundering() {
		t.Error("the world is not thundering after /weather thunder")
	}

	c.ChatCommand("weather clear 6000")
	c.ExpectCommandFeedback("commands.weather.set.clear")
	down := fadeLevels(1, false)
	tickWeather(srv, len(down))
	compareWeather(t, readWeather(srv, t, c, 2*len(down)+3), fadeEvents(down, down, false))
	c.Disconnect()
}

// TestJoinMidFadeGetsTheCurrentLevels checks what a player arriving into a
// half-built storm is told: the begin event, the rain level as it stands, and
// the thunder level scaled by the rain (vanilla's getThunderLevel).
func TestJoinMidFadeGetsTheCurrentLevels(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Rainer"))
	c := connect().Join("Rainer")
	srv.StopTick()

	c.ChatCommand("weather thunder 6000")
	c.ExpectCommandFeedback("commands.weather.set.thunder")
	const ticks = 50
	tickWeather(srv, ticks)
	readWeather(srv, t, c, 2*ticks+3)
	level := fadeLevels(0, true)[ticks-1]

	late := connect()
	late.Login("Late")
	late.Configure()
	var got []servertest.WeatherEvent
	late.ReadJoin(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlayGameEvent {
			r := protocol.NewReader(data)
			e := servertest.WeatherEvent{Event: r.ReadU8(), Value: r.ReadF32()}
			if e.Event != protocol.GameEventStartWaitingForChunks {
				got = append(got, e)
			}
		}
	})
	compareWeather(t, got, []servertest.WeatherEvent{
		{Event: protocol.GameEventBeginRaining, Value: 0},
		{Event: protocol.GameEventRainLevel, Value: level},
		{Event: protocol.GameEventThunderLevel, Value: level * level},
	})

	// The fade goes on for the newcomer from where it stood.
	srv.TickWeather()
	next := fadeLevels(0, true)[ticks]
	compareWeather(t, readWeather(srv, t, late, 2), []servertest.WeatherEvent{
		{Event: protocol.GameEventRainLevel, Value: next},
		{Event: protocol.GameEventThunderLevel, Value: next},
	})
	c.Disconnect()
	late.Disconnect()
}

// TestRainFadeStaysInTheOverworld checks who hears a fade: the level ticks
// go to the overworld's players alone, while the begin event and the levels
// sent with it reach every dimension.
// vanilla: PlayerList.broadcastAll
func TestRainFadeStaysInTheOverworld(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// A small view distance keeps the nether's chunk wait inside the
	// race-time budget.
	cfg.ViewDistance = 2
	srv, c := servertest.Join(t, cfg, "Netherling", servertest.OpsListing(t, "Netherling"))
	srv.StopTick()
	srv.MovePlayerToDimension("Netherling", world.NetherName, 0.5, 64, 0.5)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived")
	}

	// Chunk packets may still trail the feedback, so the command is synced
	// with a ping instead.
	c.ChatCommand("weather rain 6000")
	c.Settle()
	tickWeather(srv, 30)
	begin := fadeLevels(0, true)[19]
	compareWeather(t, readWeather(srv, t, c, 3), []servertest.WeatherEvent{
		{Event: protocol.GameEventBeginRaining, Value: 0},
		{Event: protocol.GameEventRainLevel, Value: begin},
		{Event: protocol.GameEventThunderLevel, Value: 0},
	})
	// The loop is stopped, so the barrier alone reads what is in flight.
	if extra := servertest.DrainWeather(c, srv, 0); len(extra) != 0 {
		t.Errorf("a player in the nether heard the fade: %v", extra)
	}
	c.Disconnect()
}

// TestOverworldRainLeavesABurningNetherPlayerAlight checks that the player's
// rain reads their own dimension: the nether ticks no sky levels, so an
// overworld storm must not douse a body burning there while another player
// holds the overworld chunk whose sky light the buggy read would find.
func TestOverworldRainLeavesABurningNetherPlayerAlight(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// A small view distance keeps the nether's chunk wait inside the
	// race-time budget.
	cfg.ViewDistance = 2
	srv, connect := servertest.Start(t, cfg)

	// The overworld player holds the spawn chunk open, the one the nether
	// player's coordinates fall in.
	keep := connect().Join("Keeper")

	burner := connect().Join("Burning")
	srv.StopTick()
	srv.MovePlayerToDimension("Burning", world.NetherName, 0.5, 64, 0.5)
	if _, ok := servertest.ReadUntilViewSent(t, burner); !ok {
		t.Fatal("the nether never arrived")
	}

	// The overworld's sky comes up to full rain while the nether's stays dry.
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	tickWeather(srv, 100)
	if !srv.Raining() {
		t.Fatal("the overworld is not raining")
	}

	srv.SetPlayerFire("Burning", 100)
	srv.TickSurvival()
	if fire := srv.PlayerFire("Burning"); fire <= 0 {
		t.Errorf("the burning nether player's fire = %d under the overworld's rain, want it alight", fire)
	}
	keep.Disconnect()
	burner.Disconnect()
}

// TestACreativePlayerAboveTheWorldSurvivesTheRain checks the rain read at a Y
// outside the light sections: a creative player flying at y=400 while it rains
// ticks without panicking through the habitat scan's sky light read.
func TestACreativePlayerAboveTheWorldSurvivesTheRain(t *testing.T) {
	t.Parallel()
	// /tp is a gamemaster's command, so the runner is listed as an operator.
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Flyer", servertest.OpsListing(t, "Flyer"))
	srv.StopTick()

	// Rain at full strength, so RainingAt reads the cell's sky light.
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	tickWeather(srv, 100)
	if !srv.IsRaining() {
		t.Fatal("the world is not raining")
	}

	c.ChatCommand("tp @s 0.5 400 0.5")
	c.Settle()
	if pos := srv.PlayerPosition("Flyer"); pos.Y < 352 {
		t.Fatalf("the player stands at y=%v, want above the light sections", pos.Y)
	}

	for range 5 {
		srv.TickSurvival()
	}
	c.Disconnect()
}

// TestWeatherCycleEndsTheRain checks the countdown: rain held for two
// seconds of game time stops on its own a little after, with the same
// end-of-rain events a /weather clear would send.
func TestWeatherCycleEndsTheRain(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The rain the test ends was set by a /weather, whose runner is
	// listed as an operator.
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("weather rain 40")
	c.ExpectCommandFeedback("commands.weather.set.rain")
	expectWeatherEvent(srv, t, c, 100, protocol.GameEventBeginRaining)
	expectWeatherEvent(srv, t, c, 100, protocol.GameEventRainLevel)

	// Forty ticks is two seconds; the end arrives a little after, since the
	// countdown runs on the tick loop.
	if !srv.Raining() {
		t.Fatal("the world is not raining after /weather rain")
	}
	expectWeatherEvent(srv, t, c, 160, protocol.GameEventEndRaining)
	if srv.Raining() {
		t.Error("the world is still raining after the rain ran out")
	}
	c.Disconnect()
}

// TestWeatherSurvivesARestart checks that the rain a server leaves behind is
// the rain the next one starts with, the way level.dat carries it.
func TestWeatherSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// /weather is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("weather rain 12345")
	c.ExpectCommandFeedback("commands.weather.set.rain")
	expectWeatherEvent(srv, t, c, 100, protocol.GameEventBeginRaining)
	expectWeatherEvent(srv, t, c, 100, protocol.GameEventRainLevel)

	// A shutdown writes the weather into weather.dat with the clocks and
	// the rules, the file a vanilla server keeps it in.
	srv.Shutdown()
	root := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "data", "minecraft", "weather.dat"), "data")
	if got, ok := nbt.Get[nbt.Byte](root, "raining"); !ok || got != 1 {
		t.Errorf("weather.dat raining = %v, %v; want 1", got, ok)
	}
	rainTime, ok := nbt.Get[nbt.Int](root, "rain_time")
	if !ok || rainTime < 1 || rainTime > 12345 {
		t.Errorf("weather.dat rain_time = %v, %v; want the countdown the command set", rainTime, ok)
	}
	if _, ok := nbt.Get[nbt.Byte](servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "level.dat"), "Data"), "raining"); ok {
		t.Error("level.dat carries a raining byte, want the weather out of it")
	}

	// And a restarted world is raining the way the file says. The events
	// that tell a client so are part of its join now, so they are read out
	// of the join stream rather than waited for after it.
	restarted := *cfg
	srv2, connect2 := servertest.Start(t, &restarted)
	if !srv2.Raining() {
		t.Error("the restarted world is not raining")
	}
	again := connect2()
	again.Login("Testificate")
	again.Configure()
	sawBegin := false
	rainLevel := float32(-1)
	again.ReadJoin(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlayGameEvent {
			return
		}
		r := protocol.NewReader(data)
		switch r.ReadU8() {
		case protocol.GameEventBeginRaining:
			sawBegin = true
		case protocol.GameEventRainLevel:
			rainLevel = r.ReadF32()
		}
	})
	if !sawBegin {
		t.Error("the restarted world never told the joining client it was raining")
	}
	// A world loaded mid-storm starts with the storm at full strength, so
	// the join's rain level is already 1.
	if rainLevel != 1 {
		t.Errorf("the restarted world rejoined the rain at level %v, want 1", rainLevel)
	}
	again.Disconnect()
}
