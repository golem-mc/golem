package world

import (
	"errors"
	"log/slog"

	"github.com/golem-mc/golem/nbt"
)

// weatherFile is the saved-data file the weather lives in,
// world/data/minecraft/weather.dat; a level.dat carries none of it.
// vanilla: WeatherData.TYPE
const weatherFile = "data/minecraft/weather.dat"

// loadWeather reads weather.dat into level, preferring it over an older world's
// level.dat fields, which remain the fallback without the file.
// vanilla: WeatherData.CODEC
func loadWeather(dir string, level *LevelData) {
	data, err := readSavedData(dir, weatherFile)
	if err != nil {
		if !errors.Is(err, errNoSavedData) {
			slog.Warn("could not read weather.dat, using the level.dat fields", "error", err)
		}
		return
	}
	applyWeather(data, level)
}

// applyWeather sets the weather fields data carries on level.
func applyWeather(data nbt.Compound, level *LevelData) {
	if v, ok := nbt.Get[nbt.Int](data, "clear_weather_time"); ok {
		level.ClearWeatherTime = int32(v)
	}
	if v, ok := nbt.Get[nbt.Int](data, "rain_time"); ok {
		level.RainTime = int32(v)
	}
	if v, ok := nbt.Get[nbt.Int](data, "thunder_time"); ok {
		level.ThunderTime = int32(v)
	}
	if v, ok := nbt.Get[nbt.Byte](data, "raining"); ok {
		level.Raining = v != 0
	}
	if v, ok := nbt.Get[nbt.Byte](data, "thundering"); ok {
		level.Thundering = v != 0
	}
}

// saveWeather writes the weather flags and timers to weather.dat so either
// server reads the same weather, building the compound under the tick's lock.
// vanilla: WeatherData.CODEC
func (w *World) saveWeather() error {
	w.timeMu.Lock()
	data := w.weatherCompound()
	w.timeMu.Unlock()
	return writeSavedData(w.dir, weatherFile, data)
}

// weatherCompound is the weather.dat compound; timeMu must be held.
func (w *World) weatherCompound() nbt.Compound {
	return nbt.Compound{
		"clear_weather_time": nbt.Int(w.level.ClearWeatherTime),
		"rain_time":          nbt.Int(w.level.RainTime),
		"thunder_time":       nbt.Int(w.level.ThunderTime),
		"raining":            nbt.Byte(boolByte(w.level.Raining)),
		"thundering":         nbt.Byte(boolByte(w.level.Thundering)),
	}
}

// Dice is a UniformInt: a roll from Min to Max inclusive.
type Dice struct{ Min, Max int32 }

// Roll throws the dice on the world's seeded random, vanilla's level random.
func (d Dice) Roll(w *World) int32 { return d.Min + int32(w.rollN(int(d.Max-d.Min+1))) }

// The weather timers' dice in ticks: ServerLevel's RAIN_DELAY, RAIN_DURATION,
// THUNDER_DELAY and THUNDER_DURATION.
var (
	RainDelay       = Dice{12000, 180000}
	RainDuration    = Dice{12000, 24000}
	ThunderDelay    = Dice{12000, 180000}
	ThunderDuration = Dice{3600, 15600}
)

// skyLevels is the rain and thunder levels clients draw (Level's rainLevel and
// thunderLevel) with the previous tick's; the weather flags only set their
// direction.
type skyLevels struct {
	oRain, rain       float32
	oThunder, thunder float32
}

// rainLevel is Level.getRainLevel(1): Mth.lerp(1, oRain, rain).
func (s *skyLevels) rainLevel() float32 { return s.oRain + (s.rain - s.oRain) }

// thunderLevel is Level.getThunderLevel(1), which the rain scales.
func (s *skyLevels) thunderLevel() float32 {
	return (s.oThunder + (s.thunder - s.oThunder)) * s.rainLevel()
}

// raining is Level.isRaining: the rain level, not the flag, past 0.2.
func (s *skyLevels) raining() bool { return float64(s.rainLevel()) > 0.2 }

// thundering is Level.isThundering: the thunder level (already scaled by rain)
// past 0.9.
func (s *skyLevels) thundering() bool { return float64(s.thunderLevel()) > 0.9 }

// stepLevel moves a level 0.01 toward 1 or 0, in float32.
// vanilla: ServerLevel.advanceWeatherCycle
func stepLevel(level float32, up bool) float32 {
	if up {
		level += 0.01
	} else {
		level -= 0.01
	}
	return min(max(level, 0), 1)
}

// prepareWeather is ServerLevel.prepareWeather: a world with weather loaded
// mid-storm starts at full strength instead of fading in.
func (w *World) prepareWeather() {
	if !w.CanHaveWeather() {
		return
	}
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	if t.level.Raining {
		w.sky.rain = 1
		if t.level.Thundering {
			w.sky.thunder = 1
		}
		t.envSeq.Add(1)
	}
	w.raining.Store(w.sky.raining() && w.CanHaveWeather())
}

// SkyChange is what one weather tick did to the levels the clients draw.
type SkyChange struct {
	Rain, Thunder           float32
	RainMoved, ThunderMoved bool
	// Began and Ended are the rain level crossing 0.2 one way or the other.
	Began, Ended bool
}

// TickWeather is ServerLevel.advanceWeatherCycle: in a world with weather,
// timers run under advance_weather, and the levels fade toward the flags
// regardless. Every such world runs the overworld's shared timers, so a second
// one counts them down twice a tick, as vanilla's does.
func (w *World) TickWeather() SkyChange {
	if !w.CanHaveWeather() {
		return SkyChange{}
	}
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	wasRaining := w.sky.raining()
	if t.ruleOf(RuleAdvanceWeather) {
		w.advanceWeatherTimers(t.level)
	}
	sky := &w.sky
	prev := *sky
	sky.oThunder, sky.thunder = sky.thunder, stepLevel(sky.thunder, t.level.Thundering)
	sky.oRain, sky.rain = sky.rain, stepLevel(sky.rain, t.level.Raining)
	if *sky != prev {
		t.envSeq.Add(1)
	}
	change := SkyChange{
		Rain:         sky.rain,
		Thunder:      sky.thunder,
		RainMoved:    sky.oRain != sky.rain,
		ThunderMoved: sky.oThunder != sky.thunder,
	}
	if raining := sky.raining(); raining != wasRaining {
		change.Began, change.Ended = raining, !raining
	}
	w.raining.Store(w.sky.raining() && w.CanHaveWeather())
	return change
}

// advanceWeatherTimers counts the timers down: one at zero is rolled rather
// than flipped, and a forced clear leaves both timers to flip on the tick after
// it ends (so rain and thunder both start when a timed /weather clear runs
// out). The rolls come from w's random, the flags and timers from l.
// vanilla: ServerLevel.advanceWeatherCycle
func (w *World) advanceWeatherTimers(l *LevelData) {
	if l.ClearWeatherTime > 0 {
		l.ClearWeatherTime--
		l.ThunderTime = boolTime(!l.Thundering)
		l.RainTime = boolTime(!l.Raining)
		l.Thundering, l.Raining = false, false
		return
	}
	if l.ThunderTime > 0 {
		l.ThunderTime--
		if l.ThunderTime == 0 {
			l.Thundering = !l.Thundering
		}
	} else if l.Thundering {
		l.ThunderTime = ThunderDuration.Roll(w)
	} else {
		l.ThunderTime = ThunderDelay.Roll(w)
	}
	if l.RainTime > 0 {
		l.RainTime--
		if l.RainTime == 0 {
			l.Raining = !l.Raining
		}
	} else if l.Raining {
		l.RainTime = RainDuration.Roll(w)
	} else {
		l.RainTime = RainDelay.Roll(w)
	}
}

func boolTime(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// Sky is what an arriving player is told (PlayerList.sendLevelInfo): whether
// rain falls, and the current rain and thunder levels.
func (w *World) Sky() (raining bool, rain, thunder float32) {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return w.CanHaveWeather() && w.sky.raining(), w.sky.rainLevel(), w.sky.thunderLevel()
}

// Raining reports the raining flag the rain level fades toward, which saving,
// /weather and the weather packets use.
func (w *World) Raining() bool {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return t.level.Raining
}

// Thundering reports the weather's thundering flag.
func (w *World) Thundering() bool {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return t.level.Thundering
}

// IsRaining is Level.isRaining: the fading rain level past 0.2, which gameplay
// gates on instead of the flag.
func (w *World) IsRaining() bool {
	return w.raining.Load()
}

// rainLevelNow is Level.getRainLevel(1), the fading level the clients draw.
func (w *World) rainLevelNow() float32 {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return w.sky.rainLevel()
}

// IsThundering is Level.isThundering: the fading thunder level, already scaled
// by rain, past 0.9.
func (w *World) IsThundering() bool {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return w.CanHaveWeather() && w.sky.thundering()
}

// WeatherTimers reports the rain and thunder timers.
func (w *World) WeatherTimers() (rain, thunder int32) {
	t := w.timeWorld()
	t.timeMu.Lock()
	defer t.timeMu.Unlock()
	return t.level.RainTime, t.level.ThunderTime
}

// SetWeather sets the flags and timers, clearTime holding a forced clear in
// place of the rain timer; the levels fade toward the flags over the following
// ticks.
func (w *World) SetWeather(raining, thundering bool, rainTime, thunderTime, clearTime int32) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.level.Raining = raining
	w.level.Thundering = thundering
	w.level.RainTime = rainTime
	w.level.ThunderTime = thunderTime
	w.level.ClearWeatherTime = clearTime
}

// SetThunder sets the thundering flag and timer, leaving rain alone and
// ending a forced clear.
// Bukkit: CraftWorld.setThundering
func (w *World) SetThunder(thundering bool, thunderTime int32) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.level.Thundering = thundering
	w.level.ThunderTime = thunderTime
	w.level.ClearWeatherTime = 0
}

// SetRain sets the raining flag and timer, leaving thunder alone and ending a
// forced clear.
// Bukkit: CraftWorld.setStorm
func (w *World) SetRain(raining bool, rainTime int32) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.level.Raining = raining
	w.level.RainTime = rainTime
	w.level.ClearWeatherTime = 0
}

// ResetWeatherCycle is ServerLevel.resetWeatherCycle, run when sleepers skip a
// rainy night: the flags drop and the next tick rolls both timers.
func (w *World) ResetWeatherCycle() {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.level.Raining = false
	w.level.Thundering = false
	w.level.RainTime = 0
	w.level.ThunderTime = 0
}
