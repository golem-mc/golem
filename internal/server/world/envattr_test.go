package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestTestTimelinesFollowWeatherAndRestore checks the common attribute reader
// and the cached sky layers, including replacing an enclosing environment.
func TestTestTimelinesFollowWeatherAndRestore(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetClockTime(OverworldClock, 6000)
	w.SetWeather(true, true, 100000, 100000, 0)
	for range 110 {
		w.TickWeather()
	}
	const name = "minecraft:gameplay/sky_light_level"
	before := w.EnvironmentFloat(name, 0, 64, 0)
	if before != 9.199219 {
		t.Fatalf("settled thunder's sky = %v, want 9.199219", before)
	}
	layer := func(op registry.EnvFloatOp, value float32) registry.EnvTimeline {
		return registry.EnvTimeline{Clock: OverworldClock, Tracks: map[string]registry.EnvFloatTrack{
			name: {Op: op, Keyframes: []registry.EnvKeyframe{{Value: value, Alpha: 1}}},
		}}
	}
	check := func(want float32, dark int) {
		t.Helper()
		if got := w.EnvironmentFloat(name, 0, 64, 0); got != want {
			t.Errorf("sky level = %v, want %v", got, want)
		}
		if got := w.SkyDarken(); got != dark {
			t.Errorf("sky darken = %d, want %d", got, dark)
		}
	}
	restore := w.SetTestTimelines([]registry.EnvTimeline{layer(registry.EnvOverride, 2)})
	check(2, 13)
	// A new system starts from defaults, without the enclosing override.
	restoreInner := w.SetTestTimelines(nil)
	check(before, 5)
	restoreInner()
	check(2, 13)
	// Clamp after all layers: 20 - 7 is 13, not 15 - 7.
	restoreClamp := w.SetTestTimelines([]registry.EnvTimeline{
		layer(registry.EnvOverride, 20), layer(registry.EnvSubtract, 7),
	})
	check(13, 2)
	restoreClamp()
	check(2, 13)
	restore()
	check(before, 5)
}
