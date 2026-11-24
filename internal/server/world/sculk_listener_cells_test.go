package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestSectionListenerCells keeps the listener mask equal to the cells that
// listen through a uniform fill, single writes and a recount.
func TestSectionListenerCells(t *testing.T) {
	t.Parallel()
	sensor := registry.MustState("minecraft:sculk_sensor", nil)
	var s section
	check := func(step string) {
		t.Helper()
		for i := range blocksPerSection {
			want := listensForVibrations(s.at(i))
			if got := s.listeners != nil && s.listeners.has(i); got != want {
				t.Fatalf("%s: cell %d listed %v, want %v", step, i, got, want)
			}
		}
	}
	s.setAll(sensor)
	check("uniform")
	for i := 0; i < blocksPerSection; i += 3 {
		s.set(i, airState, false)
	}
	check("cleared")
	s.set(7, sensor, false)
	check("re-placed")
	s.recalcCounts()
	check("recount")
	s.setAll(airState)
	check("air")
}
