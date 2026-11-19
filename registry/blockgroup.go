package registry

// BlockGroup is a definition-class family used by placement and coarse shapes.
type BlockGroup uint64

const (
	GroupDoor BlockGroup = 1 << iota
	GroupTrapdoor
	GroupBed
	GroupSlab
	GroupStairs
	GroupWall
	GroupFence
	GroupFenceGate
	GroupLeaves
	GroupWallTorch
	GroupPressurePlate
	GroupRailVariant
	GroupCarpet
	GroupButton
	GroupCoralFan
	GroupCoralWallFan
	GroupFlowerPot
	GroupSign
	GroupWallSign
	GroupHangingSign
	GroupBanner
	GroupWallBanner
	GroupChest
	GroupShelf
	GroupHead
	GroupLantern
	GroupCauldron
	GroupCake
	GroupChain
	GroupCopperGolemStatue
	GroupBars
	GroupStainedGlass
	GroupGrateVariant
	GroupLightningRod
)

// InGroup reports immutable class membership, independent of data-pack tags.
func (b Block) InGroup(group BlockGroup) bool { return b < BlockCount && blockGroups[b]&group != 0 }

// BlockNamedInGroup is InGroup for callers working with registry names.
//
// Deprecated: resolve the name once with BlockByName and use Block.InGroup.
func BlockNamedInGroup(name string, group BlockGroup) bool {
	b, ok := BlockByName(Qualify(name))
	return ok && b.InGroup(group)
}

// BlocksInGroup builds a block-indexed membership table for a class family.
func BlocksInGroup(group BlockGroup) *[BlockCount]bool {
	return BlocksOf(func(b Block) bool { return b.InGroup(group) })
}
