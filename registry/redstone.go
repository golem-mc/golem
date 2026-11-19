package registry

var (
	redstoneConductorBlock = BlocksWhere(func(name string) bool { return RedstoneConductor[name] })
	// conductorCandidate is every block with a state that can conduct: the
	// conductors and the slabs, whose double state is a full block.
	conductorCandidate = BlocksWhere(func(name string) bool {
		return RedstoneConductor[name] || BlockInTag(name, "minecraft:slabs")
	})
	// conductorAlways are the blocks whose conductor predicate is an explicit
	// Blocks::always, true despite a collision shape below the full cube.
	// vanilla: Blocks.SOUL_SAND, Blocks.MUD
	conductorAlways   = BlocksOf(func(b Block) bool { return b == BlockSoulSand || b == BlockMud })
	signalSourceBlock = BlocksWhere(func(name string) bool { return RedstoneSignalSource[name] })
)

// IsRedstoneConductor reports whether a block conducts strong power -
// vanilla's isRedstoneConductor, a full, motion-blocking block.
func (b Block) IsRedstoneConductor() bool {
	return b < BlockCount && redstoneConductorBlock[b]
}

// IsRedstoneConductorState is isRedstoneConductor for a state ID: a block that
// can conduct whose state's collision shape is the whole cube, or one that is
// always a conductor (soul sand, mud). An ID that is no state is not a conductor.
// vanilla: BlockBehaviour.isRedstoneConductor
func IsRedstoneConductorState(state uint16) bool {
	if int(state) >= StateCount {
		return false
	}
	b := blockOfState[state]
	return conductorAlways[b] || conductorCandidate[b] && CollisionShapeFull(state)
}

// IsShulkerBoxState is IsShulkerBox for a state ID.
func IsShulkerBoxState(state uint16) bool {
	return int(state) < StateCount && blockOfState[state].IsShulkerBox()
}

// IsRedstoneSignalSource reports whether a block is a redstone signal source
// at all, vanilla's isSignalSource.
func (b Block) IsRedstoneSignalSource() bool {
	return b < BlockCount && signalSourceBlock[b]
}

// IsRedstoneSignalSourceState is Block.IsRedstoneSignalSource for a state ID.
func IsRedstoneSignalSourceState(state uint16) bool {
	return int(state) < StateCount && signalSourceBlock[blockOfState[state]]
}
