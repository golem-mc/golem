// Package blocklogic shares block-state calculations without world state,
// locks, scheduling or packet routing.
package blocklogic

import (
	"strconv"

	"github.com/golem-mc/golem/registry"
)

// BlockReader supplies states to rules that inspect neighboring cells.
type BlockReader interface {
	BlockAt(x, y, z int32) uint16
}

// WireReader also accounts for conductors whose state depends on block entities.
type WireReader interface {
	BlockReader
	ConductorAt(x, y, z int32) bool
}

// Horizontal face IDs in wire property order: north, south, east, west.
var wireFaces = [4]int{2, 3, 5, 4}
var wireOffsets = [6][2]int32{2: {0, -1}, 3: {0, 1}, 4: {-1, 0}, 5: {1, 0}}

// wireConnectingSide calculates one arm, including a step up or down.
// vanilla: RedStoneWireBlock.getConnectingSide
func wireConnectingSide(w WireReader, x, y, z int32, face int, canConnectUp bool) int {
	off := wireOffsets[face]
	nx, nz := x+off[0], z+off[1]
	side := w.BlockAt(nx, y, nz)
	if canConnectUp && (registry.BlockOf(side).InGroup(registry.GroupTrapdoor) || WireCanSupport(side)) &&
		WireConnectsTo(w.BlockAt(nx, y+1, nz), 0, false) {
		if registry.FaceSturdy(side, Opposite(face), registry.SupportFull) {
			return 2
		}
		return 1
	}
	if WireConnectsTo(side, face, true) ||
		!w.ConductorAt(nx, y, nz) && WireConnectsTo(w.BlockAt(nx, y-1, nz), 0, false) {
		return 1
	}
	return 0
}

// WireSides reads the wire's arms in north, south, east, west order.
func WireSides(state uint16) [4]int {
	var sides [4]int
	for i := range sides {
		sides[i] = int(registry.StateWireSide(state, i))
	}
	return sides
}

// WireShape resolves every arm, keeping an isolated dot if requested.
// vanilla: RedStoneWireBlock.getConnectionState
func WireShape(w WireReader, x, y, z int32, wasDot bool) [4]int {
	canConnectUp := !w.ConductorAt(x, y+1, z)
	var sides [4]int
	for i, face := range wireFaces {
		sides[i] = wireConnectingSide(w, x, y, z, face, canConnectUp)
	}
	if wasDot && sides == [4]int{} {
		return sides
	}
	return WireIsolatedPair(sides)
}

// WireShapeUpdate applies an upward or horizontal neighbor shape update.
// Downward updates check support separately.
// vanilla: RedStoneWireBlock.updateShape
func WireShapeUpdate(w WireReader, x, y, z int32, sides [4]int, face int) [4]int {
	if face != 1 {
		index := WireSideIndex(face)
		side := wireConnectingSide(w, x, y, z, face, !w.ConductorAt(x, y+1, z))
		cross := sides[0] != 0 && sides[1] != 0 && sides[2] != 0 && sides[3] != 0
		if (side != 0) == (sides[index] != 0) && !cross {
			sides[index] = side
			return sides
		}
	}
	return WireShape(w, x, y, z, sides == [4]int{})
}

// WireCanSupport reports a sturdy top face or hopper.
func WireCanSupport(state uint16) bool {
	return registry.FaceSturdy(state, 1, registry.SupportFull) || registry.BlockOf(state) == registry.BlockHopper
}

// WireConnectsTo tests the signal source on a known or unspecified face.
// vanilla: RedStoneWireBlock.shouldConnectTo
func WireConnectsTo(state uint16, face int, withFace bool) bool {
	switch registry.BlockOf(state) {
	case registry.BlockRedstoneWire:
		return true
	case registry.BlockRepeater:
		facing := registry.StateFacing(state)
		return withFace && (face == facing || face == Opposite(facing))
	case registry.BlockObserver:
		return withFace && face == registry.StateFacing(state)
	}
	return withFace && registry.IsRedstoneSignalSourceState(state)
}

// WireIsolatedPair fills missing arms in north, south, east, west order.
// Both empty-axis flags are read before either axis is modified.
func WireIsolatedPair(conns [4]int) [4]int {
	n, s, e, ww := conns[0], conns[1], conns[2], conns[3]
	nsEmpty := n == 0 && s == 0
	ewEmpty := e == 0 && ww == 0
	if ww == 0 && nsEmpty {
		ww = 1
	}
	if e == 0 && nsEmpty {
		e = 1
	}
	if n == 0 && ewEmpty {
		n = 1
	}
	if s == 0 && ewEmpty {
		s = 1
	}
	return [4]int{n, s, e, ww}
}

var wireSideNames = [3]string{"none", "side", "up"}

// wireStates[power][east][north][south][west] is the wire state with that power
// and those connections, precomputed so a cascade down a line does not rebuild
// property maps.
var wireStates = func() [16][3][3][3][3]uint16 {
	var t [16][3][3][3][3]uint16
	for p := range 16 {
		for e := range 3 {
			for n := range 3 {
				for s := range 3 {
					for w := range 3 {
						id, ok := registry.FindState("minecraft:redstone_wire", map[string]string{
							"power": strconv.Itoa(p),
							"east":  wireSideNames[e],
							"north": wireSideNames[n],
							"south": wireSideNames[s],
							"west":  wireSideNames[w],
						})
						if ok {
							t[p][e][n][s][w] = id
						}
					}
				}
			}
		}
	}
	return t
}()

// WireState returns a cached wire state; power must be 0..15 and sides 0..2.
func WireState(power, east, north, south, west int) uint16 {
	return wireStates[power][east][north][south][west]
}

// TripwireConnects reports whether a tripwire reaching toward direction d
// from the asking cell connects to the state there: a tripwire always, a hook
// when it faces back at the asking cell.
// vanilla: TripWireBlock.shouldConnectTo
func TripwireConnects(state uint16, d int) bool {
	switch registry.BlockOf(state) {
	case registry.BlockTripwire:
		return true
	case registry.BlockTripwireHook:
		return registry.StateFacing(state) == Opposite(d)
	}
	return false
}
