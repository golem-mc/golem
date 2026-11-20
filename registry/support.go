package registry

// What a block state offers the blocks that hang on it, by state ID, from the
// vanilla dump in blocksupport_gen.go. Directions are Direction ordinals:
// down, up, north, south, west, east.

// SupportType is vanilla's SupportType: how much of a face isFaceSturdy asks
// to be filled. Its value is the shift of its mask in the support bits.
type SupportType uint8

const (
	SupportFull   SupportType = 0
	SupportCenter SupportType = 6
	SupportRigid  SupportType = 12
)

const (
	supportAttachShift = 18
	supportSolid       = 1 << 24
	supportTopFull     = 1 << 25
	supportSolidRender = 1 << 26
	supportBottomFull  = 1 << 27
	supportShapeFull   = 1 << 28
	supportOutlineFull = 1 << 29
)

var blockSupport [65536]uint32

var blockWallCover [65536]uint8

var blockCollisionFaces [65536]uint8

func init() {
	for i, run := range blockSupportGen {
		end := blockSupportStates
		if i+1 < len(blockSupportGen) {
			end = int(blockSupportGen[i+1].from)
		}
		for id := int(run.from); id < end; id++ {
			blockSupport[id] = run.bits
		}
	}
	for i, run := range blockWallCoverGen {
		end := blockSupportStates
		if i+1 < len(blockWallCoverGen) {
			end = int(blockWallCoverGen[i+1].from)
		}
		for id := int(run.from); id < end; id++ {
			blockWallCover[id] = run.bits
		}
	}
	for i, run := range blockCollisionFacesGen {
		end := blockSupportStates
		if i+1 < len(blockCollisionFacesGen) {
			end = int(blockCollisionFacesGen[i+1].from)
		}
		for id := int(run.from); id < end; id++ {
			blockCollisionFaces[id] = run.bits
		}
	}
}

// WallCover reports which of a wall's test shapes the DOWN face of the
// state's collision shape covers: the post, then the north, south, west and
// east arms.
// vanilla: WallBlock.isCovered
func WallCover(state uint16) (post, north, south, west, east bool) {
	c := blockWallCover[state]
	return c&1 != 0, c&2 != 0, c&4 != 0, c&8 != 0, c&16 != 0
}

// FaceSturdy is BlockState.isFaceSturdy: whether the state's face toward dir
// holds a block of the given support type.
func FaceSturdy(state uint16, dir int, t SupportType) bool {
	return blockSupport[state]>>(uint(t)+uint(dir))&1 != 0
}

// SturdyMask is the six-bit isFaceSturdy mask of one support type.
func SturdyMask(state uint16, t SupportType) uint8 {
	return uint8(blockSupport[state] >> t & 0x3f)
}

// FaceAttachable is MultifaceBlock.canAttachTo against the state's face
// toward dir: that face of its support or its collision shape is full.
func FaceAttachable(state uint16, dir int) bool {
	return blockSupport[state]>>(supportAttachShift+uint(dir))&1 != 0
}

// IsSolid is BlockState.isSolid.
func IsSolid(state uint16) bool { return blockSupport[state]&supportSolid != 0 }

// CollisionTopFull reports whether the state's collision shape fills its top
// face, the Block.isFaceFull test SnowLayerBlock.canSurvive runs.
func CollisionTopFull(state uint16) bool { return blockSupport[state]&supportTopFull != 0 }

// CollisionReachesTop reports whether the state's collision shape reaches the
// top of its cell, so its UP face shape is not empty.
// vanilla: VoxelShape.getFaceShape
func CollisionReachesTop(state uint16) bool {
	return blockCollision[state]&collisionTopMask >= 1<<12
}

// CollisionBottomFull reports whether the state's collision shape fills its
// bottom face (CeilingHangingSignBlock.getStateForPlacement).
func CollisionBottomFull(state uint16) bool { return blockSupport[state]&supportBottomFull != 0 }

// CollisionShapeFull reports whether the state's collision shape is the whole
// unit cube, vanilla's isCollisionShapeFullBlock.
func CollisionShapeFull(state uint16) bool { return blockSupport[state]&supportShapeFull != 0 }

// OutlineShapeFull reports whether the state's outline shape is the whole unit
// cube (soul sand's collision is shorter, its outline is full).
// vanilla: Block.isShapeFullBlock(getShape)
func OutlineShapeFull(state uint16) bool { return blockSupport[state]&supportOutlineFull != 0 }

// IsSolidRender is BlockState.isSolidRender.
func IsSolidRender(state uint16) bool { return blockSupport[state]&supportSolidRender != 0 }

// CollisionFaceFull reports whether the collision shape fills its face toward dir.
// vanilla: Block.isFaceFull
func CollisionFaceFull(state uint16, dir int) bool {
	return blockCollisionFaces[state]>>uint(dir)&1 != 0
}
