package world

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

var placementShapeBlocks = registry.BlocksOf(func(b registry.Block) bool {
	return plantBodyBlocks[b] || plantHeadBlocks[b] || isSpeleothem[b] ||
		snowyDirtTable[b] || hugeMushroomBlocks[b] || b.InGroup(registry.GroupDoor) ||
		b == registry.BlockBigDripleaf || b == registry.BlockChorusPlant || b == registry.BlockPaleHangingMoss
})

// PlacementShape applies neighbour shapes to a state before it is written.
// vanilla: Block.updateFromNeighbourShapes
func PlacementShape(r BlockReader, rng *rand.Rand, state uint16, x, y, z int32) uint16 {
	if !placementShapeBlocks[registry.BlockOf(state)] {
		return state
	}
	// vanilla: Block.UPDATE_SHAPE_ORDER
	for _, toward := range [...]int{dirWest, dirEast, dirNorth, dirSouth, dirDown, dirUp} {
		d := dirDelta[toward]
		neighbor := r.BlockAt(x+d[0], y+d[1], z+d[2])
		b := registry.BlockOf(state)
		switch {
		case snowyDirtTable[b]:
			state = snowyDirtShapeUpdate(state, toward, neighbor)
		case hugeMushroomBlocks[b]:
			state = hugeMushroomShapeUpdate(state, toward, neighbor)
		case b.InGroup(registry.GroupDoor):
			state = doorShapeUpdate(state, toward, neighbor)
		case plantBodyBlocks[b]:
			state = plantBodyShapeUpdate(rng, state, toward, neighbor)
		case plantHeadBlocks[b]:
			if toward == directionOf(growingPlantOf(b).dir) {
				state = GrowingPlantHeadShape(state, neighbor)
			}
		case isSpeleothem[b]:
			state = speleothemShapeState(r, state, toward, x, y, z)
		case b == registry.BlockBigDripleaf:
			if toward == dirDown && !canSurvive(r, x, y, z, state) {
				return registry.BlockAir.DefaultState()
			}
			state = bigDripleafShapeUpdate(state, toward, neighbor)
		case b == registry.BlockChorusPlant:
			if chorusPlantCanSurvive(r, x, y, z) {
				state = chorusPlantShapeState(state, toward, neighbor)
			}
		case b == registry.BlockPaleHangingMoss:
			state = hangingMossShapeUpdate(state, r.BlockAt(x, y-1, z))
		}
	}
	return state
}

// vanilla: SpeleothemBlock.updateShape
func speleothemShapeState(r BlockReader, state uint16, toward int, x, y, z int32) uint16 {
	if toward != dirUp && toward != dirDown {
		return state
	}
	tip := dirIndex(registry.StateValue(state, registry.PropVerticalDirection))
	if toward == blocklogic.Opposite(tip) && !canSurvive(r, x, y, z, state) {
		return state
	}
	current := registry.StateValue(state, registry.PropThickness)
	thickness := SpeleothemThickness(r, registry.BlockOf(state), tip, current == "tip_merge", x, y, z)
	if thickness == current {
		return state
	}
	next, _ := registry.WithProp(state, registry.PropThickness, thickness)
	return next
}

// vanilla: BigDripleafBlock.updateShape
func bigDripleafShapeUpdate(state uint16, toward int, neighbor uint16) uint16 {
	if toward != dirUp || registry.BlockOf(neighbor) != registry.BlockBigDripleaf {
		return state
	}
	next, _ := registry.WithProp(registry.BlockBigDripleafStem.DefaultState(), registry.PropFacing, registry.StateValue(state, registry.PropFacing))
	next, _ = registry.WithBool(next, registry.PropWaterlogged, registry.StateBool(state, registry.PropWaterlogged))
	return next
}

// vanilla: HangingMossBlock.updateShape
func hangingMossShapeUpdate(state, below uint16) uint16 {
	next, _ := registry.WithBool(state, registry.PropTip, registry.BlockOf(below) != registry.BlockPaleHangingMoss)
	return next
}
