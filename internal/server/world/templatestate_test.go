package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func stateOf(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	id, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state %s %v", name, props)
	}
	return id
}

func TestTemplateStateTransforms(t *testing.T) {
	tests := []struct {
		name     string
		block    string
		in, want map[string]string
		rot      Rotation
		mirror   Mirror
	}{
		{"wet stair keeps its other properties", "minecraft:oak_stairs", map[string]string{"facing": "north", "half": "top", "shape": "outer_left", "waterlogged": "true"}, map[string]string{"facing": "east", "half": "top", "shape": "outer_left", "waterlogged": "true"}, RotateClockwise90, MirrorNone},
		{"wall sides keep their height", "minecraft:cobblestone_wall", map[string]string{"north": "tall", "west": "low", "waterlogged": "true"}, map[string]string{"east": "tall", "north": "low", "waterlogged": "true"}, RotateClockwise90, MirrorNone},
		{"stair rotates", "minecraft:oak_stairs", map[string]string{"facing": "north"}, map[string]string{"facing": "east"}, RotateClockwise90, MirrorNone},
		{"stair corner swaps when turned", "minecraft:oak_stairs", map[string]string{"facing": "north", "shape": "inner_left"}, map[string]string{"facing": "south", "shape": "inner_right"}, RotateNone, MirrorLeftRight},
		{"stair corner stays when not turned", "minecraft:oak_stairs", map[string]string{"facing": "east", "shape": "inner_left"}, map[string]string{"facing": "east", "shape": "inner_left"}, RotateNone, MirrorLeftRight},
		{"door hinge swaps", "minecraft:oak_door", map[string]string{"facing": "east", "hinge": "left"}, map[string]string{"facing": "east", "hinge": "right"}, RotateNone, MirrorLeftRight},
		{"jigsaw turns both halves", "minecraft:jigsaw", map[string]string{"orientation": "north_up"}, map[string]string{"orientation": "east_up"}, RotateClockwise90, MirrorNone},
		{"jigsaw keeps a vertical front", "minecraft:jigsaw", map[string]string{"orientation": "up_east"}, map[string]string{"orientation": "up_south"}, RotateClockwise90, MirrorNone},
		{"crafter mirrors", "minecraft:crafter", map[string]string{"orientation": "west_up"}, map[string]string{"orientation": "east_up"}, RotateNone, MirrorFrontBack},
		{"log axis", "minecraft:oak_log", map[string]string{"axis": "x"}, map[string]string{"axis": "z"}, RotateCounterclockwise90, MirrorNone},
		{"log axis 180", "minecraft:oak_log", map[string]string{"axis": "x"}, map[string]string{"axis": "x"}, Rotate180, MirrorNone},
		{"fence sides", "minecraft:oak_fence", map[string]string{"north": "true"}, map[string]string{"east": "true"}, RotateClockwise90, MirrorNone},
		{"fence mirror", "minecraft:oak_fence", map[string]string{"north": "true"}, map[string]string{"south": "true"}, RotateNone, MirrorLeftRight},
		{"rail corner", "minecraft:rail", map[string]string{"shape": "south_east"}, map[string]string{"shape": "north_east"}, RotateNone, MirrorLeftRight},
		{"rail rotates", "minecraft:rail", map[string]string{"shape": "ascending_east"}, map[string]string{"shape": "ascending_north"}, RotateCounterclockwise90, MirrorNone},
		{"sign rotation", "minecraft:oak_sign", map[string]string{"rotation": "2"}, map[string]string{"rotation": "6"}, RotateClockwise90, MirrorNone},
		{"sign mirror", "minecraft:oak_sign", map[string]string{"rotation": "2"}, map[string]string{"rotation": "6"}, RotateNone, MirrorLeftRight},
		{"sign mirror front-back", "minecraft:oak_sign", map[string]string{"rotation": "2"}, map[string]string{"rotation": "14"}, RotateNone, MirrorFrontBack},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := stateOf(t, tc.block, tc.in)
			got := RotateState(MirrorState(in, tc.mirror), tc.rot)
			if want := stateOf(t, tc.block, tc.want); got != want {
				gn, gp, _ := registry.BlockStateName(got)
				wn, wp, _ := registry.BlockStateName(want)
				t.Errorf("got %s[%s], want %s[%s]", gn, gp, wn, wp)
			}
		})
	}
}
