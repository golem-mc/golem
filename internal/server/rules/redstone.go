package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// wireReader adapts a plain placement reader to the wire rule.
type wireReader struct{ BlockReader }

func (w wireReader) ConductorAt(x, y, z int32) bool {
	return registry.IsRedstoneConductorState(w.BlockAt(x, y, z))
}

func wireReadView(w BlockReader) blocklogic.WireReader {
	if live, ok := w.(blocklogic.WireReader); ok {
		return live
	}
	return wireReader{w}
}

// WirePlacementState returns the supported wire's initial shape at zero power.
// vanilla: RedStoneWireBlock.getStateForPlacement
func WirePlacementState(w BlockReader, x, y, z int32) (uint16, bool) {
	if !blocklogic.WireCanSupport(w.BlockAt(x, y-1, z)) {
		return 0, false
	}
	sides := blocklogic.WireShape(wireReadView(w), x, y, z, false)
	return blocklogic.WireState(0, sides[2], sides[0], sides[1], sides[3]), true
}

// WireStateFromState reapplies neighbor shapes in vanilla's update order.
// Placement checks downward survival separately.
func WireStateFromState(w BlockReader, base uint16, x, y, z int32) (uint16, bool) {
	if registry.BlockOf(base) != registry.BlockRedstoneWire {
		return 0, false
	}
	view := wireReadView(w)
	sides := blocklogic.WireSides(base)
	for _, d := range [5]int{dirWest, dirEast, dirNorth, dirSouth, dirUp} {
		sides = blocklogic.WireShapeUpdate(view, x, y, z, sides, d)
	}
	return blocklogic.WireState(registry.StatePower(base), sides[2], sides[0], sides[1], sides[3]), true
}

// The six directions, indexed as the vanilla Direction values.
const (
	dirDown  = 0
	dirUp    = 1
	dirNorth = 2
	dirSouth = 3
	dirWest  = 4
	dirEast  = 5
)

var dirDeltas = [6][3]int32{
	{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0},
}

// TripwirePlacementState is TripWireBlock.getStateForPlacement: the four
// side connections read from the neighbours, everything else false.
// vanilla: TripWireBlock.getStateForPlacement
func TripwirePlacementState(w BlockReader, x, y, z int32) (uint16, bool) {
	return tripwireConnected(w, x, y, z, registry.BlockTripwire.DefaultState())
}

// TripwireStateFromState is the state a tripwire takes after the neighbour
// shape pass a /setblock runs, the twin of the placement rule above with
// the state's own powered, attached and disarmed kept.
// vanilla: Block.updateFromNeighbourShapes, TripWireBlock.updateShape
func TripwireStateFromState(w BlockReader, base uint16, x, y, z int32) (uint16, bool) {
	if registry.BlockOf(base) != registry.BlockTripwire {
		return 0, false
	}
	return tripwireConnected(w, x, y, z, base)
}

// tripwireConnected sets the four side properties of base from the neighbours.
func tripwireConnected(w BlockReader, x, y, z int32, base uint16) (uint16, bool) {
	sides := [...]struct {
		dir  int
		prop registry.Prop
	}{{dirNorth, registry.PropNorth}, {dirSouth, registry.PropSouth}, {dirEast, registry.PropEast}, {dirWest, registry.PropWest}}
	ok := true
	for _, s := range sides {
		var set bool
		base, set = registry.WithBool(base, s.prop, blocklogic.TripwireConnects(w.BlockAt(x+dirDeltas[s.dir][0], y, z+dirDeltas[s.dir][1]), s.dir))
		ok = ok && set
	}
	return base, ok
}

// The target block's hit power, from vanilla's
// TargetBlock.getRedstoneStrength: the closeness scaled to fifteen and
// ceiled, never below one however far off the centre the hit landed.
const (
	targetHitPowerMax = 15.0
	targetHitPowerMin = 1
)

// TargetHitPower is a target block's power from a hit: how far the crossing
// (x, y, z) is from the centre of the entered face, on that face's two axes
// only. face is the protocol.Face* entered.
// vanilla: TargetBlock.getRedstoneStrength
func TargetHitPower(face int32, x, y, z float64) int {
	fracX := math.Abs(x - math.Floor(x) - 0.5)
	fracY := math.Abs(y - math.Floor(y) - 0.5)
	fracZ := math.Abs(z - math.Floor(z) - 0.5)
	var off float64
	switch face {
	case protocol.FaceBottom, protocol.FaceTop:
		off = math.Max(fracX, fracZ)
	case protocol.FaceNorth, protocol.FaceSouth:
		off = math.Max(fracX, fracY)
	default:
		off = math.Max(fracY, fracZ)
	}
	closeness := (0.5 - off) / 0.5
	power := targetHitPowerMax * math.Max(0, math.Min(1, closeness))
	return max(targetHitPowerMin, int(math.Ceil(power)))
}

// The target's holds, vanilla's TargetBlock.ACTIVATION_TICKS_ARROWS and
// ACTIVATION_TICKS_OTHER: the ticks before the scheduled tick resets the
// power, arrows held a full second, anything else under half.
const (
	TargetHoldTicksArrow = 20
	TargetHoldTicksOther = 8
)

// TargetHoldTicks is the hold a hit on the target block books: an arrow's,
// or any other body's.
// vanilla: TargetBlock.onProjectileHit
func TargetHoldTicks(arrow bool) int {
	if arrow {
		return TargetHoldTicksArrow
	}
	return TargetHoldTicksOther
}

// IsTargetBlock reports whether a state is the target block's.
func IsTargetBlock(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockTarget
}
