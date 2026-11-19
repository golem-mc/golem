package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The die tick lands 60 to 99 ticks after the booking.
// vanilla: BaseCoralPlantTypeBlock.tryScheduleDieTick
const (
	coralEarliest = 60
	coralWindow   = 100
)

// tickFor runs the physics tick n times from clock, returning the last tick.
func tickFor(p *Physics, clock int64, n int) int64 {
	for range n {
		clock++
		p.Tick(clock)
	}
	return clock
}

func coralState(t *testing.T, name string, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, overrides)
	if !ok {
		t.Fatalf("no state for %s %v", name, overrides)
	}
	return state
}

// TestCoralDiesOnDryLand checks that a coral block, plant, fan and wall fan
// set down dry turn into their dead twins by the end of the die window, and
// not before the shortest delay.
// vanilla: CoralBlock.tick, CoralPlantBlock.tick, CoralWallFanBlock.tick
func TestCoralDiesOnDryLand(t *testing.T) {
	stone := registry.BlockStates["minecraft:stone"]
	cases := []struct {
		name, dead string
		overrides  map[string]string
	}{
		{"minecraft:tube_coral_block", "minecraft:dead_tube_coral_block", nil},
		{"minecraft:horn_coral", "minecraft:dead_horn_coral", map[string]string{"waterlogged": "false"}},
		{"minecraft:fire_coral_fan", "minecraft:dead_fire_coral_fan", map[string]string{"waterlogged": "false"}},
		{"minecraft:brain_coral_wall_fan", "minecraft:dead_brain_coral_wall_fan", map[string]string{"waterlogged": "false", "facing": "east"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			var clock int64
			setBlock(t, w, p, 0, GroundLevel, 0, stone)
			setBlock(t, w, p, 4, GroundLevel+1, 4, stone)
			pos := [3]int32{0, GroundLevel + 1, 0}
			if c.overrides["facing"] == "east" {
				// The wall fan hangs on the east face of the block west of it.
				pos = [3]int32{5, GroundLevel + 1, 4}
			}
			setBlock(t, w, p, pos[0], pos[1], pos[2], coralState(t, c.name, c.overrides))
			clock = tickFor(p, clock, coralEarliest-1)
			if got := registryStateName(w.BlockAt(pos[0], pos[1], pos[2])); got != c.name {
				t.Fatalf("after %d ticks the block is %s, want it still %s", clock, got, c.name)
			}
			tickFor(p, clock, coralWindow-coralEarliest+1)
			state := w.BlockAt(pos[0], pos[1], pos[2])
			name, props, _ := registry.BlockStateName(state)
			if name != c.dead {
				t.Fatalf("after %d ticks the block is %s, want %s", coralWindow, name, c.dead)
			}
			parsed := registry.ParseStateProps(props)
			if f := c.overrides["facing"]; f != "" && parsed["facing"] != f {
				t.Errorf("dead wall fan faces %s, want %s", parsed["facing"], f)
			}
			if parsed["waterlogged"] == "true" {
				t.Errorf("dead coral is waterlogged")
			}
		})
	}
}
