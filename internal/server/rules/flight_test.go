package rules

import (
	"testing"
)

func hovering() FloatingStep { return FloatingStep{NoBlocksAround: true} }

func ticksToKick(t *testing.T, step FloatingStep, exempt bool) int {
	t.Helper()
	var f FlightCheck
	f.Moved(step)
	for n := 1; n <= 200; n++ {
		if f.Tick(exempt, MaxFlyingTicks(0.08)) {
			return n
		}
	}
	return 0
}

func TestFlightCheckKicksOnTheEightyFirstTick(t *testing.T) {
	t.Parallel()
	if got := ticksToKick(t, hovering(), false); got != 81 {
		t.Fatalf("kicked on tick %d, want 81", got)
	}
}
