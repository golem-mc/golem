package server

import (
	"math"
	"time"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginWorld is the plugin handle to one loaded dimension.
type pluginWorld struct {
	srv  *Server
	dim  *dimension
	host *pluginHost
}

func (h *pluginHost) Worlds() []plugin.World {
	worlds := make([]plugin.World, len(h.srv.allDims()))
	for i, d := range h.srv.allDims() {
		worlds[i] = pluginWorld{srv: h.srv, dim: d, host: h}
	}
	return worlds
}
func (h *pluginHost) World(key plugin.Key) (plugin.World, bool) {
	for _, d := range h.srv.allDims() {
		if d.name == key.String() {
			return pluginWorld{srv: h.srv, dim: d, host: h}, true
		}
	}
	return nil, false
}

// pluginWorldByName answers the handle of a dimension by its name.
func (h *pluginHost) pluginWorldByName(name string) plugin.World {
	return pluginWorld{srv: h.srv, dim: h.srv.dimensionByName(name), host: h}
}
func (h *pluginHost) Overworld() plugin.World {
	if len(h.srv.allDims()) == 0 {
		return nil
	}
	return pluginWorld{srv: h.srv, dim: h.srv.allDims()[0], host: h}
}
func (w pluginWorld) Key() plugin.Key { k, _ := plugin.ParseKey(w.dim.name); return k }
func (w pluginWorld) Dimension() plugin.Key {
	r, _ := registry.Get("minecraft:dimension_type")
	k, _ := plugin.ParseKey(registryEntry(r.Entries, int64(w.dim.typeID)))
	return k
}
func (w pluginWorld) Seed() int64 { return w.dim.w.SeedValue() }
func (w pluginWorld) MinY() int   { return int(w.dim.w.MinY()) }
func (w pluginWorld) Height() int { return int(w.dim.w.Height()) }
func (w pluginWorld) Time() int64 { age, _ := w.dim.w.Time(); return age }
func (w pluginWorld) DayTime() int64 {
	clock, ok := w.dim.w.DefaultClock()
	if !ok {
		return 0
	}
	_, t, _ := w.dim.w.ClockTime(clock)
	return t
}
func (w pluginWorld) SetDayTime(t int64) {
	clock, ok := w.dim.w.DefaultClock()
	if ok && w.srv.skipClock(w.dim.w, plugin.SkipPlugin, func() bool { return w.dim.w.SetClockTime(clock, t) }) {
		w.srv.broadcastTime(clock)
	}
}
func (w pluginWorld) Weather() plugin.Weather {
	if w.dim.w.Thundering() {
		return plugin.WeatherThunder
	}
	if w.dim.w.Raining() {
		return plugin.WeatherRain
	}
	return plugin.WeatherClear
}

func pluginDurationTicks(d time.Duration) int64 { return max(int64(d/(time.Second/20)), 0) }

func (w pluginWorld) SetWeather(weather plugin.Weather, rain, thunder time.Duration) {
	var dice world.Dice
	switch weather {
	case plugin.WeatherClear:
		dice = world.RainDelay
	case plugin.WeatherRain:
		dice = world.RainDuration
	case plugin.WeatherThunder:
		dice = world.ThunderDuration
	default:
		return
	}
	// vanilla: WeatherCommand.getDuration, MinecraftServer.setWeatherParameters
	ticks := func(d time.Duration) int32 { return int32(min(max(pluginDurationTicks(d), 1), math.MaxInt32)) }
	var rainTime int32
	if rain > 0 {
		rainTime = ticks(rain)
	} else {
		rainTime = dice.Roll(w.srv.world)
	}
	thunderTime := rainTime
	if thunder > 0 {
		thunderTime = ticks(thunder)
	}
	if weather == plugin.WeatherClear {
		w.srv.setWeatherAllowed(false, false, 0, 0, rainTime)
	} else {
		w.srv.setWeatherAllowed(true, weather == plugin.WeatherThunder, rainTime, thunderTime, 0)
	}
}

// vanilla: ServerLevelData.setThundering, setThunderTime; the roll is advanceWeatherTimers'.
func (w pluginWorld) SetThundering(on bool, d time.Duration) {
	var thunderTime int32
	switch {
	case d > 0:
		thunderTime = int32(min(max(pluginDurationTicks(d), 1), math.MaxInt32))
	case on:
		thunderTime = world.ThunderDuration.Roll(w.srv.world)
	default:
		thunderTime = world.ThunderDelay.Roll(w.srv.world)
	}
	w.srv.setThunderAllowed(on, thunderTime)
}

// vanilla: ServerLevelData.setRaining, setRainTime; the roll is advanceWeatherTimers'.
func (w pluginWorld) SetStorm(on bool, d time.Duration) {
	var rainTime int32
	switch {
	case d > 0:
		rainTime = int32(min(max(pluginDurationTicks(d), 1), math.MaxInt32))
	case on:
		rainTime = world.RainDuration.Roll(w.srv.world)
	default:
		rainTime = world.RainDelay.Roll(w.srv.world)
	}
	w.srv.setRainAllowed(on, rainTime)
}
func (w pluginWorld) RainDuration() time.Duration {
	rain, _ := w.srv.world.WeatherTimers()
	return time.Duration(rain) * (time.Second / 20)
}
func (w pluginWorld) ThunderDuration() time.Duration {
	_, thunder := w.srv.world.WeatherTimers()
	return time.Duration(thunder) * (time.Second / 20)
}
func (w pluginWorld) Difficulty() plugin.Difficulty {
	return plugin.Difficulty(w.dim.w.Difficulty())
}
func (w pluginWorld) GameRule(id plugin.GameRule) int {
	if id < 0 || int(id) >= len(world.GameRuleTable) {
		return 0
	}
	if world.GameRuleTable[id].Kind == world.RuleInt {
		return int(w.srv.world.IntRule(world.GameRuleIndex(id)))
	}
	if w.srv.world.BoolRule(world.GameRuleIndex(id)) {
		return 1
	}
	return 0
}
func (w pluginWorld) SpawnLimit(c registry.MobCategory) int {
	return w.dim.spawnTuning.Limit(c % registry.MobCategoryCount)
}
func (w pluginWorld) SetSpawnLimit(c registry.MobCategory, n int) {
	w.dim.spawnTuning.SetLimit(c%registry.MobCategoryCount, n)
}
func (w pluginWorld) TicksPerSpawn(c registry.MobCategory) int {
	return w.dim.spawnTuning.Ticks(c % registry.MobCategoryCount)
}
func (w pluginWorld) SetTicksPerSpawn(c registry.MobCategory, n int) {
	w.dim.spawnTuning.SetTicks(c%registry.MobCategoryCount, n)
}
func (w pluginWorld) SetGameRule(id plugin.GameRule, v int) {
	if id < 0 || int(id) >= len(world.GameRuleTable) {
		return
	}
	r := world.GameRuleTable[id]
	if r.Kind == world.RuleInt {
		w.srv.world.SetIntRule(world.GameRuleIndex(id), int32(min(max(int64(v), int64(r.Min)), int64(r.Max))))
	} else {
		w.srv.world.SetBoolRule(world.GameRuleIndex(id), v != 0)
	}
	(worldScope{srv: w.srv}).RuleChanged(r.ID)
}
func (w pluginWorld) SpawnPoint() plugin.Location {
	// Return the stored spawn; the client receives the border-adjusted one.
	// vanilla: Level.getRespawnData
	if w.dim.plugin {
		p, _ := w.dim.w.SpawnIn()
		return plugin.Location{World: w, Pos: plugin.Vec3{X: float64(p.X) + .5, Y: float64(p.Y), Z: float64(p.Z) + .5}, Yaw: p.Yaw, Pitch: p.Pitch}
	}
	p, name := w.srv.world.SpawnIn()
	dim := w.srv.dimensionByName(name)
	return plugin.Location{World: pluginWorld{srv: w.srv, dim: dim, host: w.host}, Pos: plugin.Vec3{X: float64(p.X) + .5, Y: float64(p.Y), Z: float64(p.Z) + .5}, Yaw: p.Yaw, Pitch: p.Pitch}
}

func (w pluginWorld) Players() []plugin.Player {
	var players []plugin.Player
	for _, c := range w.srv.playerClientsIn(w.dim.name) {
		p := pluginPlayer{host: w.host, c: c}
		if p.IsValid() {
			players = append(players, p)
		}
	}
	return players
}
func (w pluginWorld) SetSpawnPoint(loc plugin.Location) {
	p := loc.Pos.Block()
	if w.dim.plugin {
		spot := world.SpawnPos{X: int32(p.X), Y: int32(p.Y), Z: int32(p.Z), Yaw: world.WrapDegrees(loc.Yaw), Pitch: max(min(loc.Pitch, 90), -90)}
		prev, _ := w.dim.w.SpawnIn()
		w.dim.w.SetOwnSeedAndSpawn(w.dim.w.SeedValue(), spot)
		w.dim.w.SavePluginMeta(world.PluginMeta{Seed: w.dim.w.SeedValue(), Spawn: spot})
		w.srv.fireSpawnChange(w.dim, prev)
		return
	}
	prev, _ := w.srv.world.SpawnIn()
	w.srv.world.SetSpawnIn(world.SpawnPos{X: int32(p.X), Y: int32(p.Y), Z: int32(p.Z), Yaw: world.WrapDegrees(loc.Yaw), Pitch: max(min(loc.Pitch, 90), -90)}, w.dim.name)
	w.srv.fireSpawnChange(w.dim, prev)
	w.srv.broadcastSpawnPosition()
}
func (w pluginWorld) Save(flush bool) { w.srv.SaveAll(flush) }
func (w pluginWorld) Border() plugin.WorldBorder {
	return pluginBorder{borderView{srv: w.srv, dim: w.dim}}
}

type pluginBorder struct{ view borderView }

func (b pluginBorder) Center() (float64, float64) {
	s := b.view.State()
	return s.CenterX, s.CenterZ
}
func (b pluginBorder) SetCenter(x, z float64) { b.view.SetCenter(x, z) }
func (b pluginBorder) Size() float64          { return b.view.State().Size }
func (b pluginBorder) SetSize(size float64, d time.Duration) {
	if ticks := pluginDurationTicks(d); ticks > 0 {
		b.view.LerpSize(b.Size(), size, ticks)
	} else {
		b.view.SetSize(size)
	}
}
func (b pluginBorder) DamagePerBlock() float64       { damage, _ := b.view.DamageSettings(); return damage }
func (b pluginBorder) SetDamagePerBlock(d float64)   { b.view.SetDamagePerBlock(d) }
func (b pluginBorder) WarningDistance() int          { return int(b.view.State().WarningBlocks) }
func (b pluginBorder) SetWarningDistance(n int)      { b.view.SetWarningBlocks(int32(n)) }
func (b pluginBorder) Contains(pos plugin.Vec3) bool { return b.view.dim.border.Contains(pos.X, pos.Z) }
