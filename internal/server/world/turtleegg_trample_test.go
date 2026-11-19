package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestDecreaseTurtleEggRunsTheShapePass checks that taking one of two eggs is
// a flag-2 write whose shape pass pulses the observer watching the cell.
// vanilla: TurtleEggBlock.decreaseEggs
func TestDecreaseTurtleEggRunsTheShapePass(t *testing.T) {
	w, p, clock := observerCircuit(t)
	egg := registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": "2", "hatch": "0"})
	setBlock(t, w, p, 13, 63, 9, egg)
	for range 12 {
		*clock++
		p.Tick(*clock)
	}
	if props := observerProps(t, w, 14, 63, 9); props["powered"] == "true" {
		t.Fatalf("setup: the observer was pulsing before the egg changed: %v", props)
	}

	if out := p.DecreaseTurtleEgg(13, 63, 9, egg); !out.EggsRemain {
		t.Fatal("the egg did not stay with one egg left")
	}
	if pulse := observerPulseTicks(t, w, p, clock); pulse != 2 {
		t.Errorf("the observer pulsed for %d ticks, want the two-tick pulse", pulse)
	}
}
