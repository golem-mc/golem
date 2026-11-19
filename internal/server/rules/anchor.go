package rules

import (
	"github.com/golem-mc/golem/registry"
)

// The respawn anchor: charges, the water that braces its blast, and the
// stand-up search on respawn. The charging click is internal/server/anchor.go's.

// anchorStandUpOffsets is the stand-up search order: the eight cells
// around, the same one down, one up, then the anchor's own cell.
// vanilla: RespawnAnchorBlock.RESPAWN_OFFSETS
var anchorStandUpOffsets = [][3]int32{
	{0, 0, -1}, {-1, 0, 0}, {0, 0, 1}, {1, 0, 0},
	{-1, 0, -1}, {1, 0, -1}, {-1, 0, 1}, {1, 0, 1},
	{0, -1, -1}, {-1, -1, 0}, {0, -1, 1}, {1, -1, 0},
	{-1, -1, -1}, {1, -1, -1}, {-1, -1, 1}, {1, -1, 1},
	{0, 1, -1}, {-1, 1, 0}, {0, 1, 1}, {1, 1, 0},
	{-1, 1, -1}, {1, 1, -1}, {-1, 1, 1}, {1, 1, 1},
	{0, 1, 0},
}

// AnchorStandUpSpot is the first offset cell a standing body fits on for
// an anchor at (x, y, z), first refusing dangerous cells, then accepting
// them; false when boxed in.
// vanilla: RespawnAnchorBlock.findStandUpPosition
func AnchorStandUpSpot(w BedWorld, x, y, z int32) (px, py, pz float64, ok bool) {
	for _, checkDangerous := range [2]bool{true, false} {
		for _, o := range anchorStandUpOffsets {
			if px, py, pz, ok := safeStandUp(w, x+o[0], y+o[1], z+o[2], checkDangerous); ok {
				return px, py, pz, true
			}
		}
	}
	return 0, 0, 0, false
}

// AnchorMaxCharges is the most an anchor holds.
// vanilla: RespawnAnchorBlock.MAX_CHARGES
const AnchorMaxCharges = 4

// AnchorState reads a block state as the anchor it may be: its charges,
// false for any other block.
func AnchorState(state uint16) (charges int, ok bool) {
	if registry.BlockOf(state) != registry.BlockRespawnAnchor {
		return 0, false
	}
	charges, _ = registry.StateInt(state, registry.PropCharges)
	return charges, true
}

// AnchorWithCharges is the anchor state with charges; false when no such
// state exists.
// vanilla: RespawnAnchorBlock.charge
func AnchorWithCharges(state uint16, charges int) (uint16, bool) {
	return registry.WithInt(state, registry.PropCharges, charges)
}

// AnchorRefusesRespawn reports an empty anchor refusing a respawn before
// the search, unless the point is forced.
// vanilla: ServerPlayer.findRespawnAndUseSpawnBlock
func AnchorRefusesRespawn(charges int, forced bool) bool {
	return charges == 0 && !forced
}

// AnchorFlooded reports whether water braces the blast: any water directly
// above, or a horizontal neighbour whose water would flow. Every ray starts
// in the anchor's cell, where water's resistance kills it.
// vanilla: RespawnAnchorBlock.explode, isWaterThatWouldFlow
func AnchorFlooded(w BlockReader, x, y, z int32) bool {
	if IsWater(w.BlockAt(x, y+1, z)) {
		return true
	}
	for _, d := range [][3]int32{{0, 0, -1}, {-1, 0, 0}, {0, 0, 1}, {1, 0, 0}} {
		if IsWaterThatWouldFlow(w, x+d[0], y+d[1], z+d[2]) {
			return true
		}
	}
	return false
}

// AnchorUseOutcome is the anchor's answer to a click once the glowstone
// charge has declined it.
type AnchorUseOutcome uint8

const (
	// AnchorUsePass leaves the click to other handlers.
	AnchorUsePass AnchorUseOutcome = iota
	// AnchorUseBlast takes the anchor apart in an explosion.
	AnchorUseBlast
	// AnchorUseKeepPoint consumes the click and changes nothing.
	AnchorUseKeepPoint
	// AnchorUseSetPoint makes the anchor the spawn point.
	AnchorUseSetPoint
)

// AnchorCanCharge reports whether a click holding glowstone adds a charge.
// vanilla: RespawnAnchorBlock.useItemOn
func AnchorCanCharge(charges int, holdsGlowstone bool) bool {
	return holdsGlowstone && charges < AnchorMaxCharges
}

// AnchorUse decides a click on a charged anchor. A main-hand click with
// glowstone in the off hand passes while not full, leaving the charge to
// the off hand's click. Outside its dimensions it blasts; already the
// player's point, it answers without change.
// vanilla: RespawnAnchorBlock.useItemOn, useWithoutItem
func AnchorUse(charges int, mainHand, offHandGlowstone, works, sameSpawn bool) AnchorUseOutcome {
	if mainHand && charges < AnchorMaxCharges && offHandGlowstone {
		return AnchorUsePass
	}
	switch {
	case charges == 0:
		return AnchorUsePass
	case !works:
		return AnchorUseBlast
	case sameSpawn:
		return AnchorUseKeepPoint
	}
	return AnchorUseSetPoint
}
