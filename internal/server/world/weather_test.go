package world

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestWeatherLoadsFromVanillaWeatherDat checks that a weather.dat in the
// vanilla shape — the flags and timers under WeatherData's codec field
// names — is read, and preferred over the older level.dat fields.
func TestWeatherLoadsFromVanillaWeatherDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX":      nbt.Int(0),
		"SpawnY":      nbt.Int(GroundLevel),
		"SpawnZ":      nbt.Int(0),
		"raining":     nbt.Byte(0),
		"rainTime":    nbt.Int(999),
		"thundering":  nbt.Byte(0),
		"thunderTime": nbt.Int(888),
	})
	if err := writeSavedData(dir, weatherFile, nbt.Compound{
		"clear_weather_time": nbt.Int(3),
		"rain_time":          nbt.Int(42),
		"thunder_time":       nbt.Int(7),
		"raining":            nbt.Byte(1),
		"thundering":         nbt.Byte(1),
	}); err != nil {
		t.Fatalf("write weather.dat: %v", err)
	}

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if !w.Raining() || !w.Thundering() {
		t.Errorf("flags = %v, %v; want true, true (from weather.dat)", w.Raining(), w.Thundering())
	}
	if !w.IsRaining() {
		t.Error("loaded rain must start at full strength")
	}
	if w.level.RainTime != 42 || w.level.ThunderTime != 7 || w.level.ClearWeatherTime != 3 {
		t.Errorf("timers = %d, %d, %d; want 42, 7, 3 (from weather.dat)",
			w.level.RainTime, w.level.ThunderTime, w.level.ClearWeatherTime)
	}
}

// TestWeatherSurvivesSaveAndReload checks that the weather flags and timers
// are written to data/minecraft/weather.dat on Save and read back on the next
// load, and that the level.dat the same save writes carries none of the
// weather — a vanilla level.dat keeps no field of it.
func TestWeatherSurvivesSaveAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	w.SetWeather(true, true, 1234, 567, 0)

	w.Save()
	w.Close()

	if _, err := os.Stat(filepath.Join(dir, weatherFile)); err != nil {
		t.Fatalf("weather.dat was not written: %v", err)
	}
	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	for _, key := range []string{"raining", "rainTime", "thundering", "thunderTime", "clearWeatherTime"} {
		if _, ok := level.raw[key]; ok {
			t.Errorf("level.dat carries %q, want the weather out of it", key)
		}
	}

	again, _ := NewWorld(dir, NewFlatGenerator())
	defer again.Close()
	if !again.Raining() || !again.Thundering() {
		t.Errorf("flags after reload = %v, %v; want true, true", again.Raining(), again.Thundering())
	}
	if again.level.RainTime != 1234 || again.level.ThunderTime != 567 {
		t.Errorf("timers after reload = %d, %d; want 1234, 567", again.level.RainTime, again.level.ThunderTime)
	}
}

// TestAnOlderWorldMigratesTheWeatherToWeatherDat checks the upgrade path: a
// world whose weather lived only in level.dat gets a weather.dat on the next
// save, carrying those values, and the level.dat loses them.
func TestAnOlderWorldMigratesTheWeatherToWeatherDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"SpawnX":           nbt.Int(0),
		"SpawnY":           nbt.Int(GroundLevel),
		"SpawnZ":           nbt.Int(0),
		"raining":          nbt.Byte(1),
		"rainTime":         nbt.Int(1234),
		"thundering":       nbt.Byte(1),
		"thunderTime":      nbt.Int(567),
		"clearWeatherTime": nbt.Int(0),
	})

	w, _ := NewWorld(dir, NewFlatGenerator())
	if !w.Raining() || !w.Thundering() || w.level.RainTime != 1234 || w.level.ThunderTime != 567 {
		t.Fatalf("weather from level.dat = %v, %v, %d, %d; want true, true, 1234, 567",
			w.Raining(), w.Thundering(), w.level.RainTime, w.level.ThunderTime)
	}
	w.Save()
	w.Close()

	data, err := readSavedData(dir, weatherFile)
	if err != nil {
		t.Fatalf("weather.dat after save: %v", err)
	}
	if got, ok := nbt.Get[nbt.Byte](data, "raining"); !ok || got != 1 {
		t.Errorf("weather.dat raining = %v, %v; want 1", got, ok)
	}
	if got, ok := nbt.Get[nbt.Int](data, "rain_time"); !ok || got != 1234 {
		t.Errorf("weather.dat rain_time = %v, %v; want 1234", got, ok)
	}
	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	for _, key := range []string{"raining", "rainTime", "thundering", "thunderTime", "clearWeatherTime"} {
		if _, ok := level.raw[key]; ok {
			t.Errorf("level.dat kept %q, want the weather migrated out", key)
		}
	}
}

// TestSkyFadesWithAdvanceWeatherOff checks that the rule holds the timers
// but not the fade: vanilla moves the levels outside the rule's gate.
func TestSkyFadesWithAdvanceWeatherOff(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetBoolRule(RuleAdvanceWeather, false)
	w.SetWeather(true, true, 5, 5, 0)
	for range 101 {
		w.TickWeather()
	}
	if raining, rain, thunder := w.Sky(); !raining || rain != 1 || thunder != 1 {
		t.Errorf("sky after 101 ticks = %v, %v, %v; want true, 1, 1", raining, rain, thunder)
	}
	if w.level.RainTime != 5 || w.level.ThunderTime != 5 {
		t.Errorf("timers = %d, %d; want held at 5, 5", w.level.RainTime, w.level.ThunderTime)
	}
}

func TestIsRainingFollowsTheFadingSky(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetBoolRule(RuleAdvanceWeather, false)
	w.SetWeather(true, false, 5, 5, 0)
	if w.IsRaining() {
		t.Fatal("setting the rain flag skipped the fade")
	}
	for tick := 1; tick <= 101; tick++ {
		change := w.TickWeather()
		if got, want := w.IsRaining(), tick >= 20; got != want {
			t.Fatalf("rain fade-in tick %d: IsRaining = %v, want %v", tick, got, want)
		}
		if change.Began != (tick == 20) {
			t.Fatalf("rain fade-in tick %d: Began = %v", tick, change.Began)
		}
	}
	w.ResetWeatherCycle()
	if !w.IsRaining() {
		t.Fatal("resetting the rain flag skipped the fade")
	}
	for tick := 1; tick <= 101; tick++ {
		change := w.TickWeather()
		if got, want := w.IsRaining(), tick <= 80; got != want {
			t.Fatalf("rain fade-out tick %d: IsRaining = %v, want %v", tick, got, want)
		}
		if change.Ended != (tick == 81) {
			t.Fatalf("rain fade-out tick %d: Ended = %v", tick, change.Ended)
		}
	}
}

func TestIsRainingUsesEachDimensionsSky(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetBoolRule(RuleAdvanceWeather, false)
	w.SetWeather(true, true, 5, 5, 0)
	other := newTestWorld(t, NewFlatGenerator())
	other.SetTimeSource(w)
	if !other.IsRaining() || w.IsRaining() {
		t.Fatal("attaching a time source must prepare only the attached world's rain")
	}
	for _, nether := range []bool{true, false} {
		dim := NewDimensionWorld(t.TempDir(), nether, NewFlatGenerator())
		t.Cleanup(dim.FlushSaves)
		dim.SetTimeSource(w)
		dim.TickWeather()
		if dim.IsRaining() {
			t.Fatalf("%s must stay dry under shared rain flags", dim.Name())
		}
	}
	w.SetWeather(false, false, 5, 5, 0)
	for range 101 {
		w.TickWeather()
	}
	if !other.IsRaining() {
		t.Fatal("ticking the time source changed another world's rain level")
	}
	for range 101 {
		other.TickWeather()
	}
	if other.IsRaining() {
		t.Fatal("the attached world's rain did not fade out")
	}
}

// TestTimedClearEndsInAStorm checks vanilla's quirk: a /weather clear with a
// duration leaves both timers at one, so the tick after it runs out turns
// rain and thunder on (seen on a vanilla server).
func TestTimedClearEndsInAStorm(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetWeather(false, false, 0, 0, 3)
	for range 3 {
		w.TickWeather()
	}
	if w.Raining() || w.Thundering() {
		t.Fatal("the forced clear ended early")
	}
	w.TickWeather()
	if !w.Raining() || !w.Thundering() {
		t.Errorf("raining, thundering after the clear = %v, %v; want true, true", w.Raining(), w.Thundering())
	}
}

// TestSkyDarkenBlendsTheFadingWeather checks the darkening against the
// fading levels rather than the settled rain: halfway up the rain darkens a
// noon by two, not the settled three, and a thunder level of 0.43 under a
// settled rain lifts it to four without waiting for the thunder level's 0.9.
func TestSkyDarkenBlendsTheFadingWeather(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetClockTime(OverworldClock, 6000)
	w.SetWeather(true, false, 100000, 100000, 0)
	for range 60 {
		w.TickWeather()
	}
	_, rain, _ := w.Sky()
	if rain < 0.59 || rain > 0.61 {
		t.Fatalf("the rain fade stood at %v after sixty ticks, want about 0.6", rain)
	}
	if got := w.SkyDarken(); got != 2 {
		t.Errorf("darken under a %v rain = %d, want 2", rain, got)
	}

	// The rain settles, then the thunder fades up to a level of 0.43.
	for range 40 {
		w.TickWeather()
	}
	w.SetWeather(true, true, 100000, 100000, 0)
	for range 43 {
		w.TickWeather()
	}
	_, rain, thunder := w.Sky()
	if rain < 0.99 {
		t.Fatalf("the rain did not settle: %v", rain)
	}
	if got := w.SkyDarken(); got != 4 {
		t.Errorf("darken under a %v rain and thunder %v = %d, want 4", rain, thunder, got)
	}
}

// TestSpentTimerRollsTheDice checks that a timer at zero is rolled, not
// flipped, from the dice for the weather it holds.
func TestSpentTimerRollsTheDice(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetWeather(true, false, 0, 0, 0)
	w.TickWeather()
	if !w.Raining() || w.Thundering() {
		t.Fatal("a spent timer flipped the weather")
	}
	if r := w.level.RainTime; r < RainDuration.Min || r > RainDuration.Max {
		t.Errorf("rain timer = %d, want within %v", r, RainDuration)
	}
	if th := w.level.ThunderTime; th < ThunderDelay.Min || th > ThunderDelay.Max {
		t.Errorf("thunder timer = %d, want within %v", th, ThunderDelay)
	}
}
