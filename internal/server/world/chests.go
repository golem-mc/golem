package world

import "github.com/golem-mc/golem/registry"

// ChestConnectedOffset is the horizontal offset from a double chest's half
// to its other half; false for a single chest. vanilla:
// ChestBlock.getConnectedDirection
func ChestConnectedOffset(state uint16) (dx, dz int32, ok bool) {
	// A left half's partner stands clockwise of its facing: east of a north
	// half, south of an east one.
	switch registry.StateValue(state, registry.PropFacing) {
	case "north":
		dx = 1
	case "east":
		dz = 1
	case "south":
		dx = -1
	case "west":
		dz = -1
	default:
		return 0, 0, false
	}
	switch registry.StateValue(state, registry.PropType) {
	case "left":
		return dx, dz, true
	case "right":
		return -dx, -dz, true
	}
	return 0, 0, false
}

// CopperChestStage is a copper chest block's oxidation level, 0 (unaffected)
// to 3 (oxidized); false for any other block.
func CopperChestStage(b registry.Block) (int, bool) {
	if bare, ok := b.Unwaxed(); ok {
		b = bare
	}
	switch b {
	case registry.BlockCopperChest:
		return 0, true
	case registry.BlockExposedCopperChest:
		return 1, true
	case registry.BlockWeatheredCopperChest:
		return 2, true
	case registry.BlockOxidizedCopperChest:
		return 3, true
	}
	return 0, false
}

// LeastOxidizedCopperChest returns the block a copper chest b settles on
// beside partner: the less oxidized of the two (both unwaxed first when exactly
// one is waxed), b on a tie, and b for any pair that is not two copper chests.
// vanilla: CopperChestBlock.getLeastOxidizedChestOfConnectedBlocks
func LeastOxidizedCopperChest(b, partner registry.Block) registry.Block {
	sa, aok := CopperChestStage(b)
	sb, bok := CopperChestStage(partner)
	if !aok || !bok {
		return b
	}
	na, np := b, partner
	bareB, waxedB := b.Unwaxed()
	barePartner, waxedPartner := partner.Unwaxed()
	if waxedB != waxedPartner {
		if waxedB {
			na = bareB
		}
		if waxedPartner {
			np = barePartner
		}
	}
	if sb < sa {
		return np
	}
	return na
}

// copperChestShapeUpdate returns state turned into the block of its partner
// when that is another copper chest, keeping its own properties, so a waxed,
// scraped or weathered half carries the other along. state is the half after
// ChestBlock.updateShape.
// vanilla: CopperChestBlock.updateShape
func (p *Physics) copperChestShapeUpdate(x, y, z int32, state uint16, d [3]int32) uint16 {
	dx, dz, ok := ChestConnectedOffset(state)
	// d points from the changed block to this one, so only a change on the
	// partner's side counts.
	if !ok || dx != -d[0] || dz != -d[2] || d[1] != 0 {
		return state
	}
	partner := registry.BlockOf(p.w.BlockAt(x+dx, y, z+dz))
	if !partner.InTag(registry.TagIDCopperChests) || registry.BlockOf(state) == partner {
		return state
	}
	if next, ok := registry.WithPropertiesOf(partner, state); ok {
		return next
	}
	return state
}
