package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// LodestoneWorld is what a lodestone compass's tick reads of its dimension:
// whether the target's chunk is held, and the block standing there.
type LodestoneWorld interface {
	BlockAt(x, y, z int32) uint16
	ChunkLoadedAt(x, z int32) bool
}

// worldBorderLimit is the horizontal extent of the world, in blocks each way
// of the origin.
// vanilla: Level.MAX_LEVEL_SIZE
const worldBorderLimit = 30000000

// ClearLostLodestone returns the stack with its tracker's target taken off,
// and true, when the stack is a tracked lodestone compass that LodestoneLost
// says has lost its lodestone.
func ClearLostLodestone(w LodestoneWorld, stack protocol.ItemStack, dim string, bottom, top int32) (protocol.ItemStack, bool) {
	tracker, ok := stack.LodestoneTrackerOf()
	if !ok || !LodestoneLost(w, tracker, dim, bottom, top) {
		return stack, false
	}
	tracker.Target = nil
	stack.SetLodestoneTracker(tracker)
	return stack, true
}

// LodestoneLost reports whether a compass in dimension dim (world height
// bottom inclusive to top exclusive) lost its lodestone: a same-dimension
// target that is not a lodestone or is out of bounds. Another dimension, or
// an in-bounds target in a chunk w cannot read, is not lost.
// vanilla: CompassItem.inventoryTick, LodestoneTracker.tick
func LodestoneLost(w LodestoneWorld, tracker protocol.LodestoneTracker, dim string, bottom, top int32) bool {
	if !tracker.Tracked || tracker.Target == nil || tracker.Target.Dimension != dim {
		return false
	}
	target := tracker.Target
	if target.X < -worldBorderLimit || target.X >= worldBorderLimit ||
		target.Z < -worldBorderLimit || target.Z >= worldBorderLimit {
		return true
	}
	if target.Y >= bottom && target.Y < top {
		if !w.ChunkLoadedAt(target.X, target.Z) {
			return false
		}
		if registry.BlockOf(w.BlockAt(target.X, target.Y, target.Z)) == registry.BlockLodestone {
			return false
		}
	}
	return true
}

// LodestoneLocksInPlace reports whether a compass used on a lodestone
// converts in place instead of splitting one off: only a lone stack without
// infinite materials.
// vanilla: CompassItem.useOn
func LodestoneLocksInPlace(creative bool, count int32) bool {
	return !creative && count == 1
}
