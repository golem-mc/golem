package world

import "testing"

// BenchmarkSkyDarken reads the overworld's darkening at dusk in a settled
// thunderstorm, where the day track and both weather layers apply; the
// monster spawn reads it at every position it tries.
func BenchmarkSkyDarken(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	w.SetClockTime(OverworldClock, 12500)
	w.SetWeather(true, true, 100000, 100000, 0)
	for range 600 {
		w.TickWeather()
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = w.SkyDarken()
	}
}
