package registry

// The connection rules for fences, walls and bars; the group predicates they
// use are in blockgroups.go.

// IsExceptionForConnection reports whether a block has a sturdy full-cube
// face yet takes no fence, wall or bars connection: leaves, barrier, carved
// pumpkin, jack o'lantern, melon, pumpkin and the shulker boxes.
// vanilla: Block.isExceptionForConnection
func (b Block) IsExceptionForConnection() bool {
	if b >= BlockCount {
		return false
	}
	return connectionExcluded[b] || b.InTag(idTagLeaves) || b.InTag(idTagShulkerBoxes)
}

// connectionExcluded holds the five blocks named; leaves and shulker boxes are
// tags and are read live.
var connectionExcluded = BlocksWhere(func(name string) bool {
	switch name {
	case "minecraft:barrier", "minecraft:carved_pumpkin", "minecraft:jack_o_lantern",
		"minecraft:melon", "minecraft:pumpkin":
		return true
	}
	return false
})

// barsBlocks is BarsBlocks as a table.
var barsBlocks = BlocksWhere(func(name string) bool { return BarsBlocks[name] })

// ConnectsToFence reports whether a fence connects toward state in dir (from
// the fence to the neighbour): a non-exception with a sturdy (FULL) face, a
// fence of the same wooden/nether-brick group as fence, or a gate facing
// perpendicular to dir. vanilla: FenceBlock.connectsTo
func ConnectsToFence(fence Block, state uint16, dir string) bool {
	if int(state) >= StateCount {
		return false
	}
	b := BlockOf(state)
	if b.IsFence() {
		return fence.IsWoodenFence() == b.IsWoodenFence()
	}
	if b.IsFenceGate() {
		return gateConnects(state, dir)
	}
	if b.IsExceptionForConnection() {
		return false
	}
	return faceSturdyToward(state, dir)
}

// gateConnects reports whether a gate takes a connection from dir: a
// north/south gate has its posts east and west, so only those connect.
// vanilla: FenceGateBlock.connectsToDirection
func gateConnects(state uint16, dir string) bool {
	switch StateValue(state, PropFacing) {
	case "north", "south":
		return dir == "east" || dir == "west"
	case "east", "west":
		return dir == "north" || dir == "south"
	}
	return true // unknown facing: connect as a fallback
}

// ConnectsToBars reports whether a bars block (iron or copper bars, any
// pane) connects toward state: a non-exception with a sturdy face, any bars
// block, or a wall; never a gate, and no same-group check.
// vanilla: IronBarsBlock.attachsTo
func ConnectsToBars(state uint16, dir string) bool {
	if int(state) >= StateCount {
		return false
	}
	b := BlockOf(state)
	if b.IsBars() || b.IsWall() {
		return true
	}
	if b.IsExceptionForConnection() {
		return false
	}
	return faceSturdyToward(state, dir)
}

// ConnectsToWall reports whether a wall connects toward state in dir: a
// wall, a non-exception with a sturdy face, any bars block (fences do not),
// or a gate facing perpendicular to dir. vanilla: WallBlock.connectsTo
func ConnectsToWall(state uint16, dir string) bool {
	if int(state) >= StateCount {
		return false
	}
	b := BlockOf(state)
	if b.IsWall() || b.IsBars() {
		return true
	}
	if b.IsFenceGate() {
		return gateConnects(state, dir)
	}
	if b.IsExceptionForConnection() {
		return false
	}
	return faceSturdyToward(state, dir)
}

// faceSturdyToward is FenceBlock's faceSolid: whether the neighbour's face
// toward the fence, opposite dir, is sturdy. Fences, bars and walls share it.
func faceSturdyToward(state uint16, dir string) bool {
	var face int
	switch dir {
	case "north":
		face = 3
	case "south":
		face = 2
	case "east":
		face = 4
	case "west":
		face = 5
	default:
		return false
	}
	return FaceSturdy(state, face, SupportFull)
}
