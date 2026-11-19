package world

import "github.com/golem-mc/golem/registry"

// The hopper's own state between moves: vanilla's HopperBlockEntity
// cooldown and the box its mouth draws items from.

// HopperMoveInterval is vanilla's MOVE_ITEM_SPEED: the cooldown a hopper
// sets itself after a move.
const HopperMoveInterval = 8

// HopperReady is the head of pushItemsTick: count the cooldown down and record
// the tick's game time for the chained tie-break. It reports whether the hopper
// may move this tick.
func (b *BlockEntity) HopperReady(gameTime int64) bool {
	b.hopperSlept = false
	b.Cooldown--
	b.TickedGameTime = gameTime
	if b.Cooldown > 0 {
		return false
	}
	b.Cooldown = 0
	return true
}

// HopperReceived is the chained-hopper cooldown for a hopper that was empty
// and just received from src (nil for an item entity): 8, or 7 when src is
// a hopper and this one's tick ran at or after src's.
// vanilla: HopperBlockEntity.tryMoveInItem, setCooldown(8 - skip)
func (b *BlockEntity) HopperReceived(src *BlockEntity) {
	if b.Kind != KindHopper || b.Cooldown > HopperMoveInterval {
		return
	}
	b.Wake()
	if b.hopperSlept && src != nil && src.Kind == KindHopper {
		// pushItemsTick: a sleeping receiver before the source already had its
		// no-op turn this tick; one after it last ran on the preceding tick.
		b.hopperSlept = false
		b.TickedGameTime = src.TickedGameTime - 1
		if b.tickOrder < src.tickOrder {
			b.TickedGameTime = src.TickedGameTime
		}
	}
	k := int32(0)
	if src != nil && src.Kind == KindHopper && b.TickedGameTime >= src.TickedGameTime {
		k = 1
	}
	b.Cooldown = HopperMoveInterval - k
}

// HopperSuckBox is the volume vanilla's SUCK_AABB covers: from the top of
// the funnel (11/16) to two blocks above the hopper's corner.
func (b *BlockEntity) HopperSuckBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return float64(b.X), float64(b.Y) + 11.0/16.0, float64(b.Z),
		float64(b.X) + 1, float64(b.Y) + 2, float64(b.Z) + 1
}

// doesNotBlockHoppers is vanilla's block tag of the same name.
var doesNotBlockHoppers = blocksNamed("minecraft:bee_nest", "minecraft:beehive")

// HopperBlockedBy tells whether the block above a hopper keeps it from the
// items lying there: one that fills its cell, unless the tag excuses it.
func HopperBlockedBy(state uint16, fullCell bool) bool {
	return fullCell && !doesNotBlockHoppers[registry.BlockOf(state)]
}

// HopperEnabled reads the enabled property, false under a redstone signal.
func HopperEnabled(w *World, x, y, z int32) bool {
	return registry.StateEnabled(w.BlockAt(x, y, z))
}

var containerMouthBlocks = registry.BlocksOf(func(b registry.Block) bool {
	switch b.Type() {
	case registry.BlockTypeDecoratedPot, registry.BlockTypeShelf,
		registry.BlockTypeCopperChest, registry.BlockTypeWeatheringCopperChest:
		return true
	}
	return false
})

// ContainerMouthBlock reports a vanilla container block without a block entity
// here (decorated pot, shelves, copper chests laid without one) that
// suckInItems reaches before any block entity, since its collision does not
// fill the cell: it blocks the suck and items in its cell stay. The composter
// is not one: containerAt answers it at every level.
// vanilla: HopperBlockEntity.suckInItems
func ContainerMouthBlock(state uint16) bool {
	return containerMouthBlocks[registry.BlockOf(state)]
}
