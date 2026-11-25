package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// LodestoneWorld is what a lodestone compass's tick reads of its dimension:
// whether a lodestone stands at a position, when that is known without a disk
// read (a saved chunk's answer is resolved ahead of the tick's locks).
type LodestoneWorld interface {
	LodestoneCached(x, y, z int32) (present, known bool)
}

// worldBorderLimit is the horizontal extent of the world, in blocks each way
// of the origin.
// vanilla: Level.MAX_LEVEL_SIZE
const worldBorderLimit = 30000000

// LodestoneCompassLost reports whether the stack is a tracked lodestone
// compass that LodestoneLost says has lost its lodestone; ClearedLodestone
// is what it becomes. Other items keep their tracker.
// vanilla: CompassItem.inventoryTick
func LodestoneCompassLost(w LodestoneWorld, stack *protocol.ItemStack, dim string, bottom, top int32) bool {
	if registry.Item(stack.ItemID) != registry.ItemCompass {
		return false
	}
	tracker, ok := stack.LodestoneTrackerOf()
	return ok && LodestoneLost(w, tracker, dim, bottom, top)
}

// ClearedLodestone returns the stack with its tracker's target taken off.
func ClearedLodestone(stack protocol.ItemStack) protocol.ItemStack {
	tracker, _ := stack.LodestoneTrackerOf()
	tracker.Target = nil
	stack.SetLodestoneTracker(tracker)
	return stack
}

// ClearLostLodestone returns the stack cleared, and true, when
// LodestoneCompassLost says so.
func ClearLostLodestone(w LodestoneWorld, stack protocol.ItemStack, dim string, bottom, top int32) (protocol.ItemStack, bool) {
	if !LodestoneCompassLost(w, &stack, dim, bottom, top) {
		return stack, false
	}
	return ClearedLodestone(stack), true
}

// LodestoneLost reports whether a compass in dimension dim (world height
// bottom inclusive to top exclusive) lost its lodestone: a same-dimension
// target that is known not to be a lodestone, or is out of bounds. Another dimension or an
// untracked compass is not lost.
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
		present, known := w.LodestoneCached(target.X, target.Y, target.Z)
		return known && !present
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
