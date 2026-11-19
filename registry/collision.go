package registry

// Collision shapes, by state ID: what BlockState.getCollisionShape answers
// with the empty collision context, from the vanilla dump in
// blocksupport_gen.go.

const (
	// collisionTopMask holds the shape's max Y in 4096ths of a block, wide
	// enough for the shapes that leave their cell — a piston head's rod
	// a quarter past it, a fence's own collision 24 sixteenths.
	collisionTopMask = 1<<14 - 1
	// collisionFlat marks the shape that is one box covering the whole
	// cell floor.
	collisionFlat = 1 << 14
)

var blockCollision [65536]uint16

func init() {
	for i, run := range blockCollisionGen {
		end := blockCollisionStates
		if i+1 < len(blockCollisionGen) {
			end = int(blockCollisionGen[i+1].from)
		}
		for id := int(run.from); id < end; id++ {
			blockCollision[id] = run.bits
		}
	}
}

// CollisionHeight gives the height of a block state's collision above its
// cell's floor: a flat whole-floor shape at its own top, a shape that
// leaves its cell at its full reach, any other shape with a collision the
// whole cell, an empty shape nothing.
// vanilla: BlockState.getCollisionShape
func CollisionHeight(state uint16) float64 {
	shape := blockCollision[state]
	top := float64(shape&collisionTopMask) / 4096
	switch {
	case shape&collisionFlat != 0, top > 1:
		return top
	case top > 0:
		return 1
	}
	return 0
}

// CollisionTop is the max Y of the state's collision shape in cell units,
// 0 for an empty shape and above 1 for one that leaves its cell.
// vanilla: VoxelShape.max
func CollisionTop(state uint16) float64 {
	return float64(blockCollision[state]&collisionTopMask) / 4096
}

// CollisionFlat reports whether the state's collision is exactly one box
// covering the whole cell floor, so CollisionHeight is its exact shape.
func CollisionFlat(state uint16) bool { return blockCollision[state]&collisionFlat != 0 }
