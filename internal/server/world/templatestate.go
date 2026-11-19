package world

import (
	"strings"

	"github.com/golem-mc/golem/registry"
)

// Rotation is a template rotation about the vertical axis.
// vanilla: Rotation
type Rotation int

// The rotations, in the order of vanilla's Rotation enum.
const (
	RotateNone Rotation = iota
	RotateClockwise90
	Rotate180
	RotateCounterclockwise90
)

// Mirror is a template mirror.
// vanilla: Mirror
type Mirror int

// The mirrors in Mirror enum order; left-right flips Z, front-back flips X.
const (
	MirrorNone Mirror = iota
	MirrorLeftRight
	MirrorFrontBack
)

// ParseRotation reads the serialized name of a rotation.
func ParseRotation(s string) (Rotation, bool) {
	switch s {
	case "none":
		return RotateNone, true
	case "clockwise_90":
		return RotateClockwise90, true
	case "180":
		return Rotate180, true
	case "counterclockwise_90":
		return RotateCounterclockwise90, true
	}
	return 0, false
}

// ParseMirror reads the serialized name of a mirror.
func ParseMirror(s string) (Mirror, bool) {
	switch s {
	case "none":
		return MirrorNone, true
	case "left_right":
		return MirrorLeftRight, true
	case "front_back":
		return MirrorFrontBack, true
	}
	return 0, false
}

// horizontals are the horizontal directions in clockwise order.
var horizontals = [4]string{"north", "east", "south", "west"}

func horizontalIndex(d string) int {
	for i, h := range horizontals {
		if h == d {
			return i
		}
	}
	return -1
}

// rotateDir turns a direction by the rotation; up and down stay.
// vanilla: Rotation.rotate(Direction)
func rotateDir(d string, r Rotation) string {
	i := horizontalIndex(d)
	if i < 0 {
		return d
	}
	return horizontals[(i+int(r))%4]
}

// mirrorDir flips a direction along the mirror's axis.
// vanilla: Mirror.mirror(Direction)
func mirrorDir(d string, m Mirror) string {
	i := horizontalIndex(d)
	if i < 0 || m == MirrorNone {
		return d
	}
	onZ := i%2 == 0
	if m == MirrorLeftRight && onZ || m == MirrorFrontBack && !onZ {
		return horizontals[(i+2)%4]
	}
	return d
}

// railDirs are the directions each rail shape names, ascending shapes
// carrying the way up.
var railDirs = map[string][2]string{
	"north_south": {"north", "south"}, "east_west": {"east", "west"},
	"ascending_east": {"east", ""}, "ascending_west": {"west", ""},
	"ascending_north": {"north", ""}, "ascending_south": {"south", ""},
	"south_east": {"south", "east"}, "south_west": {"south", "west"},
	"north_west": {"north", "west"}, "north_east": {"north", "east"},
}

// mapRailShape maps a rail shape's directions through f; a straight shape stays
// itself when its pair maps to the same axis.
// vanilla: BaseRailBlock.rotate, BaseRailBlock.mirror
func mapRailShape(shape string, f func(string) string) string {
	dirs, ok := railDirs[shape]
	if !ok {
		return shape
	}
	a, b := f(dirs[0]), dirs[1]
	if b != "" {
		b = f(b)
	}
	for name, d := range railDirs {
		if d == [2]string{a, b} || d == [2]string{b, a} {
			return name
		}
	}
	return shape
}

// rotateSteps turns an integer rotation of the given number of steps.
// vanilla: Rotation.rotate(int, int)
func rotateSteps(v int, r Rotation, steps int) int {
	return (v + int(r)*steps/4) % steps
}

// mirrorSteps flips an integer rotation of the given number of steps.
// vanilla: Mirror.mirror(int, int)
func mirrorSteps(v int, m Mirror, steps int) int {
	half := steps / 2
	c := v
	if v > half {
		c = v - steps
	}
	switch m {
	case MirrorFrontBack:
		return (steps - c) % steps
	case MirrorLeftRight:
		return (half - c + steps) % steps
	}
	return v
}

// RotateState rotates a state's orientation properties: facing, axis, the four
// sides of fences, walls, panes, vines and wire, a rail's shape, and the
// 16-step rotation of signs, banners and skulls, and the jigsaw and crafter
// orientation.
// vanilla: BlockState.rotate
func RotateState(state uint16, r Rotation) uint16 {
	if r == RotateNone {
		return state
	}
	if f := registry.StateValue(state, registry.PropFacing); f != "" {
		state, _ = registry.WithProp(state, registry.PropFacing, rotateDir(f, r))
	}
	if r == RotateClockwise90 || r == RotateCounterclockwise90 {
		switch registry.StateValue(state, registry.PropAxis) {
		case "x":
			state, _ = registry.WithProp(state, registry.PropAxis, "z")
		case "z":
			state, _ = registry.WithProp(state, registry.PropAxis, "x")
		}
	}
	if n, ok := registry.StateInt(state, registry.PropRotation); ok {
		state, _ = registry.WithInt(state, registry.PropRotation, rotateSteps(n, r, 16))
	}
	state = rotateSides(state, r, MirrorNone)
	if o := registry.StateValue(state, registry.PropOrientation); o != "" {
		state, _ = registry.WithProp(state, registry.PropOrientation, mapOrientation(o, func(d string) string { return rotateDir(d, r) }))
	}
	if shape := registry.StateValue(state, registry.PropShape); shape != "" {
		state, _ = registry.WithProp(state, registry.PropShape, mapRailShape(shape, func(d string) string { return rotateDir(d, r) }))
	}
	return state
}

// rotateSides moves side values in clockwise order, preserving the original reads.
func rotateSides(state uint16, r Rotation, m Mirror) uint16 {
	sides := [...]registry.Prop{registry.PropNorth, registry.PropEast, registry.PropSouth, registry.PropWest}
	before := state
	for i, prop := range sides {
		if v := registry.StateValue(before, prop); v != "" {
			to := i
			if m == MirrorLeftRight && i%2 == 0 || m == MirrorFrontBack && i%2 != 0 {
				to = (i + 2) % 4
			}
			state, _ = registry.WithProp(state, sides[(to+int(r))%4], v)
		}
	}
	return state
}

// MirrorState mirrors a state, RotateState's counterpart; a door's hinge and a
// stair's corner swap sides.
// vanilla: BlockState.mirror
func MirrorState(state uint16, m Mirror) uint16 {
	if m == MirrorNone {
		return state
	}
	if f := registry.StateValue(state, registry.PropFacing); f != "" {
		state, _ = registry.WithProp(state, registry.PropFacing, mirrorDir(f, m))
	}
	if n, ok := registry.StateInt(state, registry.PropRotation); ok {
		state, _ = registry.WithInt(state, registry.PropRotation, mirrorSteps(n, m, 16))
	}
	if h := registry.StateValue(state, registry.PropHinge); h != "" {
		state, _ = registry.WithProp(state, registry.PropHinge, swapLeftRight(h))
	}
	if shape := registry.StateValue(state, registry.PropShape); shape != "" {
		if _, rail := railDirs[shape]; rail {
			state, _ = registry.WithProp(state, registry.PropShape, mapRailShape(shape, func(d string) string { return mirrorDir(d, m) }))
		} else if f := registry.StateValue(state, registry.PropFacing); f != "" && mirrorDir(f, m) != f {
			// vanilla: StairBlock.mirror swaps the corner only when the mirror turns the stair around.
			state, _ = registry.WithProp(state, registry.PropShape, swapLeftRight(shape))
		}
	}
	state = rotateSides(state, RotateNone, m)
	if o := registry.StateValue(state, registry.PropOrientation); o != "" {
		state, _ = registry.WithProp(state, registry.PropOrientation, mapOrientation(o, func(d string) string { return mirrorDir(d, m) }))
	}
	return state
}

// mapOrientation maps both halves of a "front_top" orientation through f.
// vanilla: OctahedralGroup.rotate(FrontAndTop)
func mapOrientation(o string, f func(string) string) string {
	front, top, ok := strings.Cut(o, "_")
	if !ok {
		return o
	}
	return f(front) + "_" + f(top)
}

// swapLeftRight swaps left and right in a property value.
func swapLeftRight(v string) string {
	switch v {
	case "left":
		return "right"
	case "right":
		return "left"
	case "inner_left":
		return "inner_right"
	case "inner_right":
		return "inner_left"
	case "outer_left":
		return "outer_right"
	case "outer_right":
		return "outer_left"
	}
	return v
}
