package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The scaffolding column a click on a piece extends; the landing piece's
// distance is internal/server/world/scaffolding.go's (ScaffoldingBlock.getDistance).
// vanilla: ScaffoldingBlockItem.updatePlacementContext

// scaffoldingWalkSteps is how far the extension walk reaches along a
// horizontal direction, vanilla's own loop bound (unnamed there).
const scaffoldingWalkSteps = 7

// ScaffoldingExtendTo retargets a scaffolding placement clicked on a piece:
// the walk goes along the clicked face when sneaking (out its far side when
// clicked from inside), along the horizontal look for a top-face click, else
// up, over scaffolding to the first replaceable cell, which it returns. false
// (vanilla's null) for an irreplaceable cell, a horizontal walk's last step,
// or the world's top (tooHigh, answered with the build-limit message).
// vanilla: ScaffoldingBlockItem.updatePlacementContext
func ScaffoldingExtendTo(w BlockReader, x, y, z int32, ctx PlacementContext, minY, maxY int32) (tx, ty, tz int32, ok, tooHigh bool) {
	var direction int
	if ctx.Sneaking {
		direction = int(ctx.Face)
		if ctx.Inside {
			direction = blocklogic.Opposite(direction)
		}
	} else if ctx.Face == protocol.FaceTop {
		direction = registry.FacingIndex(PlayerDirection(ctx.Yaw))
	} else {
		direction = dirUp
	}
	delta := dirDeltas[direction]
	horizontal := direction >= dirNorth
	tx, ty, tz = x+delta[0], y+delta[1], z+delta[2]
	// A vertical walk runs to the world's ends alone; the step count and its
	// bound belong to the horizontal walks.
	for steps := 0; !horizontal || steps < scaffoldingWalkSteps; {
		if ty < minY || ty > maxY {
			tooHigh = ty > maxY
			return
		}
		state := w.BlockAt(tx, ty, tz)
		if registry.BlockOf(state) != registry.BlockScaffolding {
			ok = CanBeReplaced(state, registry.BlockScaffolding, ctx)
			return
		}
		tx, ty, tz = tx+delta[0], ty+delta[1], tz+delta[2]
		if horizontal {
			steps++
		}
	}
	return
}
