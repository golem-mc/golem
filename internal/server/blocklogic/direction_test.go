package blocklogic

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDirectionNamesAndOpposites(t *testing.T) {
	names := [6]string{"down", "up", "north", "south", "west", "east"}
	opposites := [6]int{1, 0, 3, 2, 5, 4}
	for d, name := range names {
		if got := FaceName(d); got != name {
			t.Errorf("FaceName(%d) = %q, want %q", d, got, name)
		}
		if got := registry.FacingIndex(name); got != d {
			t.Errorf("FacingIndex(%q) = %d, want %d", name, got, d)
		}
		if got := Opposite(d); got != opposites[d] {
			t.Errorf("Opposite(%d) = %d, want %d", d, got, opposites[d])
		}
	}
}

func TestHorizontalTurns(t *testing.T) {
	for _, tc := range []struct{ d, clockwise, counterClockwise int }{
		{2, 5, 4}, {3, 4, 5}, {4, 2, 3}, {5, 3, 2},
	} {
		if got := Clockwise(tc.d); got != tc.clockwise {
			t.Errorf("Clockwise(%d) = %d, want %d", tc.d, got, tc.clockwise)
		}
		if got := CounterClockwise(tc.d); got != tc.counterClockwise {
			t.Errorf("CounterClockwise(%d) = %d, want %d", tc.d, got, tc.counterClockwise)
		}
	}
}

func TestHorizontalConnectionProperties(t *testing.T) {
	wire := WireState(0, 1, 2, 0, 1)
	for _, tc := range []struct {
		d, index int
		prop     registry.Prop
	}{
		{2, 0, registry.PropNorth}, {3, 1, registry.PropSouth},
		{4, 3, registry.PropWest}, {5, 2, registry.PropEast},
	} {
		if got := SideProp(tc.d); got != tc.prop {
			t.Errorf("SideProp(%d) = %v, want %v", tc.d, got, tc.prop)
		}
		if got := WireSideIndex(tc.d); got != tc.index {
			t.Errorf("WireSideIndex(%d) = %d, want %d", tc.d, got, tc.index)
		}
		names := [3]string{"none", "side", "up"}
		if got, want := registry.StateValue(wire, SideProp(tc.d)), names[registry.StateWireSide(wire, WireSideIndex(tc.d))]; got != want {
			t.Errorf("direction %d property %q disagrees with side %q", tc.d, got, want)
		}
	}
}
