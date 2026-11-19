package rules

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// maxPlantHeadAge is the exclusive bound of a placed head's random age.
const maxPlantHeadAge = 25

// GrowingPlantPlacementState is the state a kelp or vine item lays at (x, y,
// z): the body when the cell in the growth direction already holds the plant,
// else the head at the age drawn by randomAge. Any other block, and the
// body blocks, are returned unchanged.
// vanilla: GrowingPlantBlock.getStateForPlacement, GrowingPlantHeadBlock.getStateForPlacement
func GrowingPlantPlacementState(w BlockReader, state uint16, x, y, z int32, randomAge func(int) int) uint16 {
	head := registry.BlockOf(state)
	body, dy, ok := world.GrowingPlantHead(head)
	if !ok {
		return state
	}
	if isPlantOf(w.BlockAt(x, y+dy, z), head, body) {
		return body.DefaultState()
	}
	if s, ok := registry.WithInt(head.DefaultState(), registry.PropAge, randomAge(maxPlantHeadAge)); ok {
		return s
	}
	return state
}

// plantHeadBlocks marks the heads of the growing plants.
var plantHeadBlocks = registry.BlocksOf(func(b registry.Block) bool {
	_, _, ok := world.GrowingPlantHead(b)
	return ok
})

// isPlantOf reports whether the state is the head or the body block given.
func isPlantOf(state uint16, head, body registry.Block) bool {
	b := registry.BlockOf(state)
	return b == head || b == body
}

// reconnectPlantHeads turns the head below or above the block at (x, y, z)
// into its body when this block is the same plant's head or body in its growth
// direction. A cave vines head keeps its berries.
// vanilla: GrowingPlantHeadBlock.updateShape, CaveVinesBlock.updateBodyAfterConvertedFromHead
func reconnectPlantHeads(w ShapeWorld, out []world.BlockChange, x, y, z int32) []world.BlockChange {
	if !world.IsGrowingPlantState(w.BlockAt(x, y, z)) {
		return out
	}
	for _, dy := range [2]int32{-1, 1} {
		state := w.BlockAt(x, y+dy, z)
		head := registry.BlockOf(state)
		if !plantHeadBlocks[head] {
			continue
		}
		bodyBlock, growth, _ := world.GrowingPlantHead(head)
		if growth != -dy || !isPlantOf(w.BlockAt(x, y, z), head, bodyBlock) {
			continue
		}
		body := world.GrowingPlantHeadShape(state, w.BlockAt(x, y, z))
		if !w.SetBlock(x, y+dy, z, body) {
			continue
		}
		out = append(out, world.BlockChange{X: x, Y: y + dy, Z: z, State: body})
	}
	return out
}

// MangrovePropaguleItemState is the standing propagule an item lays: fully
// grown, age 4. Other blocks are returned unchanged.
// vanilla: MangrovePropaguleBlock.getStateForPlacement
func MangrovePropaguleItemState(state uint16) uint16 {
	if registry.BlockOf(state) != registry.BlockMangrovePropagule {
		return state
	}
	if s, ok := registry.WithInt(state, registry.PropAge, 4); ok {
		return s
	}
	return state
}
