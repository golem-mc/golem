package blocklogic

import "github.com/golem-mc/golem/registry"

// Opposite returns the opposite of a Direction.values index (0..5).
// vanilla: Direction.getOpposite
func Opposite(d int) int { return d ^ 1 }

// Clockwise turns a horizontal Direction.values index ninety degrees clockwise.
// vanilla: Direction.getClockWise
func Clockwise(d int) int {
	switch d {
	case 2:
		return 5
	case 5:
		return 3
	case 3:
		return 4
	default:
		return 2
	}
}

// CounterClockwise turns a horizontal Direction.values index ninety degrees counterclockwise.
// vanilla: Direction.getCounterClockWise
func CounterClockwise(d int) int { return Opposite(Clockwise(d)) }

// FaceName returns a Direction.values index's property name; unknown reads as east.
// vanilla: Direction.getName
func FaceName(d int) string {
	switch d {
	case 0:
		return "down"
	case 1:
		return "up"
	case 2:
		return "north"
	case 3:
		return "south"
	case 4:
		return "west"
	default:
		return "east"
	}
}

// SideProp returns the connection property for a horizontal Direction.values index.
func SideProp(d int) registry.Prop {
	switch d {
	case 2:
		return registry.PropNorth
	case 3:
		return registry.PropSouth
	case 4:
		return registry.PropWest
	default:
		return registry.PropEast
	}
}

// FaceProp returns the connection property for a Direction.values index (0..5).
func FaceProp(d int) registry.Prop {
	switch d {
	case 0:
		return registry.PropDown
	case 1:
		return registry.PropUp
	case 2:
		return registry.PropNorth
	case 3:
		return registry.PropSouth
	case 4:
		return registry.PropWest
	default:
		return registry.PropEast
	}
}

// WireSideIndex maps a horizontal Direction.values index to north, south, east, west order.
func WireSideIndex(d int) int {
	return [6]int{3, 3, 0, 1, 3, 2}[d]
}
