package rules

import (
	"strconv"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// ChestPlacementState faces the player and pairs with a compatible single
// chest beside it. Sneaking against a single chest's side takes its facing
// and pairs; sneaking elsewhere places a single. The partner's half is its
// shape update's.
// vanilla: ChestBlock.getStateForPlacement
func ChestPlacementState(w BlockReader, b registry.Block, ctx PlacementContext, x, y, z int32) (uint16, bool) {
	facing := registry.FacingIndex(facingFromPlayer(ctx.Yaw))
	typ := "single"
	var partner registry.Block
	havePartner := false
	clicked := int(ctx.Face)
	if clicked >= dirNorth && clicked <= dirEast && ctx.Sneaking {
		behind := blocklogic.Opposite(clicked)
		if pb, pfacing, ok := chestPartnerFacing(w, b, x, y, z, behind); ok && horizontalAxis(pfacing) != horizontalAxis(clicked) {
			facing = pfacing
			partner, havePartner = pb, true
			typ = "left"
			if blocklogic.CounterClockwise(facing) == behind {
				typ = "right"
			}
		}
	}
	if typ == "single" && !ctx.Sneaking {
		if pb, pfacing, ok := chestPartnerFacing(w, b, x, y, z, blocklogic.Clockwise(facing)); ok && pfacing == facing {
			partner, havePartner = pb, true
			typ = "left"
		} else if pb, pfacing, ok := chestPartnerFacing(w, b, x, y, z, blocklogic.CounterClockwise(facing)); ok && pfacing == facing {
			partner, havePartner = pb, true
			typ = "right"
		}
	}
	if havePartner {
		// A copper chest placed against another stage becomes the least
		// oxidized of the two.
		b = world.LeastOxidizedCopperChest(b, partner)
	}
	return registry.WithProps(b.DefaultState(),
		registry.PropValue{P: registry.PropFacing, V: blocklogic.FaceName(facing)},
		registry.PropValue{P: registry.PropType, V: typ},
		registry.PropValue{P: registry.PropWaterlogged, V: strconv.FormatBool(waterAt(w, x, y, z))})
}

// chestPartnerFacing reads the single chest a chest b pairs with one step
// along d from (x, y, z): its block and facing, false where there is none.
// vanilla: ChestBlock.candidatePartnerFacing
func chestPartnerFacing(w BlockReader, b registry.Block, x, y, z int32, d int) (registry.Block, int, bool) {
	state := w.BlockAt(x+dirDeltas[d][0], y+dirDeltas[d][1], z+dirDeltas[d][2])
	if !world.ChestsConnect(b, registry.BlockOf(state)) || registry.StateValue(state, registry.PropType) != "single" {
		return 0, 0, false
	}
	return registry.BlockOf(state), registry.StateFacing(state), true
}

// horizontalAxis tells the north-south axis from the east-west one.
func horizontalAxis(d int) bool { return d == dirNorth || d == dirSouth }
