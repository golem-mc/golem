package world

import "testing"

// TestATimeSourceWorldResolvesEveryTimeAccessor checks that the nether and
// the end, which have no level.dat of their own, read and advance the
// overworld's age and clocks through every accessor rather than a stale zero
// of their own: Time, ClockTime and ClockViews agree, and a tick or a set on
// the dimension lands on the overworld.
func TestATimeSourceWorldResolvesEveryTimeAccessor(t *testing.T) {
	t.Parallel()
	dimensions := []struct {
		name   string
		create func(dir string) *World
	}{
		{"nether", func(dir string) *World { return NewDimensionWorld(dir, true, NewNetherGenerator(0)) }},
		{"end", func(dir string) *World { return NewDimensionWorld(dir, false, NewEndGenerator(0)) }},
	}
	for _, d := range dimensions {
		t.Run(d.name, func(t *testing.T) {
			overworld, _ := NewWorld(t.TempDir(), NewFlatGenerator())
			t.Cleanup(overworld.FlushSaves)
			overworld.SetWorldAge(100)

			dim := d.create(t.TempDir())
			t.Cleanup(dim.FlushSaves)
			dim.SetTimeSource(overworld)

			if age, _ := dim.Time(); age != 100 {
				t.Errorf("Time age of a time-source world = %d, want the overworld's 100", age)
			}
			if age, total, ok := dim.ClockTime(OverworldClock); !ok || age != 100 || total != 0 {
				t.Errorf("ClockTime of a time-source world = (%d, %d, %v), want (100, 0, true)", age, total, ok)
			}
			if age, _ := dim.ClockViews(OverworldClock); age != 100 {
				t.Errorf("ClockViews age of a time-source world = %d, want 100", age)
			}

			dim.TickTime()
			if age, _ := overworld.Time(); age != 101 {
				t.Errorf("overworld age after ticking the time-source world = %d, want 101", age)
			}

			dim.SetWorldAge(500)
			if age, _ := overworld.Time(); age != 500 {
				t.Errorf("overworld age after SetWorldAge on the time-source world = %d, want 500", age)
			}

			dim.SetClockTime(OverworldClock, 42)
			if _, total, _ := overworld.ClockTime(OverworldClock); total != 42 {
				t.Errorf("overworld clock after SetClockTime on the time-source world = %d, want 42", total)
			}
		})
	}
}
