package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// ladderFrontClicked is LadderBlock.getStateForPlacement's refusal of a
// click on a ladder's front: the new ladder would lean on nothing.
func ladderFrontClicked(w BlockReader, ctx PlacementContext, x, y, z int32) bool {
	if ctx.ReplacingClickedOn {
		return false
	}
	off := protocol.FaceOffset[ctx.Face]
	state := w.BlockAt(x-off[0], y-off[1], z-off[2])
	return registry.BlockOf(state) == registry.BlockLadder && registry.StateValue(state, registry.PropFacing) == blocklogic.FaceName(int(ctx.Face))
}

// waterAt is getFluidState(pos).is(Fluids.WATER), what placement's
// waterlogged reads: a level-0 source, a waterlogged state, or an
// always-source block. Levels 1-15 (falling too) are FlowingWater, not
// Fluids.WATER.
func waterAt(w BlockReader, x, y, z int32) bool {
	f := blocklogic.FluidOf(w.BlockAt(x, y, z))
	return f.Kind == blocklogic.FluidWater && f.IsSource()
}

// waterFullAt is level.getFluidState(pos) being water and full, the read the
// coral family and the conduit make: a source and a falling column (level 8
// and up) are full, a flow of level 1-7 is not.
func waterFullAt(w BlockReader, x, y, z int32) bool {
	return blocklogic.FluidOf(w.BlockAt(x, y, z)).IsFullWater()
}
