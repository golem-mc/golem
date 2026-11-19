package rules

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const paleMossCarpet = world.PaleMossCarpet

// mossyCarpetHasFaces is MossyCarpetBlock.hasFaces.
func mossyCarpetHasFaces(state uint16) bool {
	if registry.StateBool(state, registry.PropBottom) {
		return true
	}
	for _, prop := range registry.HorizontalSideProps {
		if registry.StateValue(state, prop) != "none" {
			return true
		}
	}
	return false
}

// reconnectMossyCarpet reshapes the pale moss carpet at (x, y, z) and appends
// the write, air when no face is left, then reshapes the carpets above and
// below the written state. A non-carpet is a no-op.
// vanilla: MossyCarpetBlock.updateShape
func reconnectMossyCarpet(w ShapeWorld, out []world.BlockChange, x, y, z int32) []world.BlockChange {
	return reconnectMossyCarpetDepth(w, out, x, y, z, 512)
}

// reconnectMossyCarpetDepth is reconnectMossyCarpet with the remaining
// shape-update depth, vanilla's limit of 512 nested updates.
// vanilla: Level.setBlock
func reconnectMossyCarpetDepth(w ShapeWorld, out []world.BlockChange, x, y, z int32, depth int) []world.BlockChange {
	if depth <= 0 {
		return out
	}
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockPaleMossCarpet {
		return out
	}
	newState := world.MossyCarpetState(w, state, x, y, z, false)
	if !mossyCarpetHasFaces(newState) {
		newState = airState
	}
	if newState == state || !w.SetBlock(x, y, z, newState) {
		return out
	}
	out = append(out, world.BlockChange{X: x, Y: y, Z: z, State: newState})
	// Only a carpet stacked above or below reads this one's state.
	out = reconnectMossyCarpetDepth(w, out, x, y+1, z, depth-1)
	return reconnectMossyCarpetDepth(w, out, x, y-1, z, depth-1)
}
