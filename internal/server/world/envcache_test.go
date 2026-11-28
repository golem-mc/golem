package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func assertEnvCacheMatches(t *testing.T, w *World, step string) {
	t.Helper()
	if got, want := w.SkyDarken(), w.skyDarkenNow(); got != want {
		t.Fatalf("%s: SkyDarken() = %d, want %d", step, got, want)
	}
	if got, want := w.MonstersBurn(), w.environmentGlobal(envMonstersBurn) != 0; got != want {
		t.Fatalf("%s: MonstersBurn() = %v, want %v", step, got, want)
	}
	if got, want := w.PiglinsZombify(), w.environmentGlobal(envPiglinsZombify) != 0; got != want {
		t.Fatalf("%s: PiglinsZombify() = %v, want %v", step, got, want)
	}
}

func TestEnvCacheMatchesUncached(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	nether := NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(0))
	t.Cleanup(nether.FlushSaves)
	// Warm nether's cache before attaching the time source to verify invalidation.
	assertEnvCacheMatches(t, nether, "nether initial")
	nether.SetTimeSource(w)
	assertEnvCacheMatches(t, nether, "nether after SetTimeSource")

	assertEnvCacheMatches(t, w, "initial")

	// Step through a full day and night, exercising rain and thunder transitions
	// across day, dusk, night and dawn.
	for tick := range ticksPerDay + 1000 {
		switch tick {
		case 2000:
			w.SetWeather(true, false, 100000, 100000, 0)
		case 5000:
			w.SetWeather(true, true, 100000, 100000, 0)
		case 12000:
			w.SetWeather(false, false, 0, 0, 100000)
		case 18000:
			w.SetWeather(true, true, 100000, 100000, 0)
		case 23000:
			w.SetWeather(false, false, 0, 0, 100000)
		}
		w.TickTime()
		w.TickWeather()
		assertEnvCacheMatches(t, w, "day/night tick")
	}

	// Clock jumps: /time set, /time add, marker jump, and sleep wake-up.
	for _, ticks := range []int64{0, 6000, 12500, 13000, 18000, 23500, 1000} {
		w.SetClockTime(OverworldClock, ticks)
		assertEnvCacheMatches(t, w, "SetClockTime")
	}
	w.AddClockTime(OverworldClock, 12000)
	assertEnvCacheMatches(t, w, "AddClockTime")
	w.MoveClockToMarker(OverworldClock, 1000, ticksPerDay)
	assertEnvCacheMatches(t, w, "MoveClockToMarker")
	w.WakeUpFromSleep(OverworldClock)
	assertEnvCacheMatches(t, w, "WakeUpFromSleep")

	// Weather fading to full rain, full thunder, and back to clear while time is fixed.
	w.SetClockTime(OverworldClock, 6000)
	assertEnvCacheMatches(t, w, "noon before weather")
	w.SetWeather(true, false, 100000, 100000, 0)
	for range 120 {
		w.TickWeather()
		assertEnvCacheMatches(t, w, "rain fade in")
	}
	w.SetWeather(true, true, 100000, 100000, 0)
	for range 120 {
		w.TickWeather()
		assertEnvCacheMatches(t, w, "thunder fade in")
	}
	w.SetWeather(false, false, 0, 0, 100000)
	for range 120 {
		w.TickWeather()
		assertEnvCacheMatches(t, w, "weather fade out")
	}

	// Test timeline overriding sky_light_level, monsters_burn and piglins_zombify,
	// then restoring via teardown.
	override := []registry.EnvTimeline{{
		Clock:  OverworldClock,
		Period: ticksPerDay,
		Tracks: map[string]registry.EnvFloatTrack{
			"minecraft:gameplay/sky_light_level": {
				Op:        registry.EnvOverride,
				Keyframes: []registry.EnvKeyframe{{Ticks: 0, Value: 4}},
			},
			"minecraft:gameplay/monsters_burn": {
				Op:        registry.EnvOverride,
				Keyframes: []registry.EnvKeyframe{{Ticks: 0, Value: 0}},
			},
			"minecraft:gameplay/piglins_zombify": {
				Op:        registry.EnvOverride,
				Keyframes: []registry.EnvKeyframe{{Ticks: 0, Value: 0}},
			},
		},
	}}
	restore := w.SetTestTimelines(override)
	assertEnvCacheMatches(t, w, "test timeline installed")
	if w.SkyDarken() != 11 || w.MonstersBurn() || w.PiglinsZombify() {
		t.Fatalf("test timeline did not take effect: darken=%d burn=%v zombify=%v",
			w.SkyDarken(), w.MonstersBurn(), w.PiglinsZombify())
	}
	restore()
	assertEnvCacheMatches(t, w, "test timeline restored")
	if w.SkyDarken() != 0 || !w.MonstersBurn() || !w.PiglinsZombify() {
		t.Fatalf("test timeline teardown did not restore: darken=%d burn=%v zombify=%v",
			w.SkyDarken(), w.MonstersBurn(), w.PiglinsZombify())
	}

	// Attached dimension sees overworld clock and test-timeline changes.
	netherRestore := nether.SetTestTimelines(override)
	assertEnvCacheMatches(t, nether, "nether test timeline installed")
	netherRestore()
	assertEnvCacheMatches(t, nether, "nether test timeline restored")
}
