package world

import "testing"

func TestVillageSiegeClockMarker(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()
	for _, total := range []int64{0, 13000, 17999, 18000, 18001, 42000} {
		w.SetClockTime(OverworldClock, total)
		if got, want := w.VillageSiegeTime(), total == 18000 || total == 42000; got != want {
			t.Fatalf("at %d: got %v want %v", total, got, want)
		}
	}
	// A paused clock on the marker still rolls on every server tick.
	w.SetClockTime(OverworldClock, 18000)
	w.SetClockPaused(OverworldClock, true)
	w.TickTime()
	if !w.VillageSiegeTime() {
		t.Fatal("paused clock lost marker")
	}
}
