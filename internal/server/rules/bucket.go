package rules

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// What a bucket's scoop and pour do to a block state. The fluids a bucket
// carries, with their items and sounds, are server's table; these read and
// return the fluid as its block, registry.BlockWater or registry.BlockLava.

// BucketFluidSource reports which fluid a state is a source of (level 0):
// water or lava, the two a bucket carries. Flowing fluid is no source.
func BucketFluidSource(state uint16) (registry.Block, bool) {
	f := blocklogic.FluidOf(state)
	if !f.Pure || !f.IsSource() {
		return 0, false
	}
	return registry.BlockOf(state), true
}

// BucketScoopable reports whether an empty bucket's ray may stop on a
// state's fluid: a source, a waterlogged block (even through its outline's
// gaps), or a bubble column.
// vanilla: BucketItem.use, ClipContext.Fluid.SOURCE_ONLY
func BucketScoopable(state uint16) bool {
	if _, ok := BucketFluidSource(state); ok {
		return true
	}
	if _, ok := BubbleColumnDrag(state); ok {
		return true
	}
	return registry.StateBool(state, registry.PropWaterlogged)
}

// WithWaterlogged returns a block's own state with its waterlogged property
// set, when the block carries one.
// vanilla: SimpleWaterloggedBlock.placeLiquid, SimpleWaterloggedBlock.pickupBlock
func WithWaterlogged(state uint16, on bool) (uint16, bool) {
	next, ok := registry.WithBool(state, registry.PropWaterlogged, on)
	if !ok {
		return 0, false
	}
	return next, true
}

// BucketScoopTarget is the state an empty bucket leaves and the fluid it
// takes: a source drained to air, or a waterlogged block unwaterlogged for
// water. Powder snow drains to air as registry.BlockPowderSnow. false for
// nothing scoopable.
// vanilla: BucketItem.use, SimpleWaterloggedBlock.pickupBlock,
// PowderSnowBlock.pickupBlock
func BucketScoopTarget(state uint16) (after uint16, fluid registry.Block, ok bool) {
	if fluid, ok := BucketFluidSource(state); ok {
		return airState, fluid, true
	}
	// vanilla: BubbleColumnBlock.pickupBlock
	if _, ok := BubbleColumnDrag(state); ok {
		return airState, registry.BlockWater, true
	}
	if IsPowderSnow(state) {
		return airState, registry.BlockPowderSnow, true
	}
	if !registry.StateBool(state, registry.PropWaterlogged) {
		return 0, 0, false
	}
	dry, ok := WithWaterlogged(state, false)
	if !ok {
		return 0, 0, false
	}
	return dry, registry.BlockWater, true
}

// CanPlaceLiquid is the block's state with the fluid in its own cell: water
// only, where LiquidBlockContainer accepts it (waterloggable, no double slab,
// a barrier only for creative), never while sneaking (the fluid goes over the
// face instead). An already waterlogged block returns its own state, the
// hand still emptying. Otherwise false, left to the face's neighbour. A
// dispenser is vanilla's nil user: never sneaking or creative.
// vanilla: BucketItem.emptyContents, SimpleWaterloggedBlock.canPlaceLiquid,
// SlabBlock.canPlaceLiquid, BarrierBlock.canPlaceLiquid
func CanPlaceLiquid(fluid registry.Block, state uint16, sneaking, creative bool) (uint16, bool) {
	if fluid != registry.BlockWater || sneaking {
		return 0, false
	}
	if registry.StateValue(state, registry.PropType) == "double" ||
		registry.BlockOf(state) == registry.BlockBarrier && !creative {
		return 0, false
	}
	next, _, ok := blocklogic.PlaceWater(state)
	return next, ok
}
