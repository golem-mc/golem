package blocklogic

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

func TestWireStateRoundTrip(t *testing.T) {
	names := [3]string{"none", "side", "up"}
	for power := range 16 {
		for east := range 3 {
			for north := range 3 {
				for south := range 3 {
					for west := range 3 {
						state := WireState(power, east, north, south, west)
						if got, _ := registry.StateInt(state, registry.PropPower); got != power {
							t.Fatalf("power %d became %d", power, got)
						}
						for _, side := range []struct {
							prop registry.Prop
							want int
						}{{registry.PropEast, east}, {registry.PropNorth, north}, {registry.PropSouth, south}, {registry.PropWest, west}} {
							if got := registry.StateValue(state, side.prop); got != names[side.want] {
								t.Fatalf("side %v = %q, want %q", side.prop, got, names[side.want])
							}
						}
					}
				}
			}
		}
	}
}

func TestWireIsolatedPair(t *testing.T) {
	for _, tc := range []struct{ in, want [4]int }{
		{[4]int{}, [4]int{1, 1, 1, 1}},
		{[4]int{2, 0, 0, 0}, [4]int{2, 1, 0, 0}},
		{[4]int{1, 0, 1, 0}, [4]int{1, 0, 1, 0}},
		{[4]int{0, 0, 1, 0}, [4]int{0, 0, 1, 1}},
	} {
		if got := WireIsolatedPair(tc.in); got != tc.want {
			t.Errorf("%v -> %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestWireDirectionalSources(t *testing.T) {
	for _, tc := range []struct {
		name        string
		face        int
		known, want bool
	}{
		{"minecraft:redstone_wire", 2, false, true},
		{"minecraft:repeater", 2, true, true},
		{"minecraft:repeater", 3, true, true},
		{"minecraft:repeater", 4, true, false},
		{"minecraft:repeater", 2, false, false},
		{"minecraft:observer", 2, true, true},
		{"minecraft:observer", 3, true, false},
		{"minecraft:stone", 2, true, false},
	} {
		var props map[string]string
		if tc.name == "minecraft:repeater" || tc.name == "minecraft:observer" {
			props = map[string]string{"facing": "north"}
		}
		state := registry.MustState(tc.name, props)
		if got := WireConnectsTo(state, tc.face, tc.known); got != tc.want {
			t.Errorf("%s face %d known %v = %v", tc.name, tc.face, tc.known, got)
		}
	}
}

type cells map[[3]int32]uint16

func (c cells) BlockAt(x, y, z int32) uint16 { return c[[3]int32{x, y, z}] }

func TestFireStateAt(t *testing.T) {
	planks := registry.MustState("minecraft:oak_planks", nil)
	state := FireStateAt(cells{{0, 5, -1}: planks, {1, 5, 0}: planks}, 0, 5, 0, 0, 7)
	for _, p := range []registry.Prop{registry.PropNorth, registry.PropEast} {
		if yes := registry.StateBool(state, p); !yes {
			t.Errorf("missing lean %v", p)
		}
	}
	if age, _ := registry.StateInt(state, registry.PropAge); age != 7 {
		t.Errorf("age = %d", age)
	}
	if got := FireStateAt(cells{{0, 4, 0}: registry.MustState("minecraft:soul_sand", nil)}, 0, 5, 0, 0, 7); got != SoulFire {
		t.Error("soul fire base lost")
	}
	if got := FireStateAt(cells{{0, 4, 0}: planks}, 0, 5, 0, 0, 7); registry.BlockOf(got) != registry.BlockFire || registry.StateBool(got, registry.PropNorth) {
		t.Error("fuel floor lost")
	}
	stone := registry.MustState("minecraft:stone", nil)
	beside := cells{{0, 4, 0}: stone, {0, 5, -1}: planks}
	if got := FireStateAt(beside, 0, 5, 0, 0, 7); registry.StateBool(got, registry.PropNorth) {
		t.Error("a sturdy floor still leaned the fire")
	}
	if got := FireStateAt(beside, 0, 5, 0, 5, 7); !registry.StateBool(got, registry.PropNorth) {
		t.Error("a floor at or below supportY held the fire up")
	}
}
