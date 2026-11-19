package world

import "github.com/golem-mc/golem/registry"

// ShulkerLidBlockedAt reports whether a block collides with the half block a
// shulker box's lid swings through when it opens: the half of the cell in
// front of its facing, shrunk by a millionth on every side. Entities are not
// tested.
// vanilla: ShulkerBoxBlock.canOpen, Shulker.getProgressDeltaAabb
func (w *World) ShulkerLidBlockedAt(x, y, z int32, facing string) bool {
	const deflate = 1e-6
	cell := [3]float64{float64(x), float64(y), float64(z)}
	lo := [3]float64{cell[0] + deflate, cell[1] + deflate, cell[2] + deflate}
	hi := [3]float64{cell[0] + 1 - deflate, cell[1] + 1 - deflate, cell[2] + 1 - deflate}
	axis, positive := 1, true
	switch facing {
	case "down":
		positive = false
	case "north":
		axis, positive = 2, false
	case "south":
		axis = 2
	case "west":
		axis, positive = 0, false
	case "east":
		axis = 0
	}
	if positive {
		lo[axis], hi[axis] = cell[axis]+1+deflate, cell[axis]+1.5-deflate
	} else {
		lo[axis], hi[axis] = cell[axis]-0.5+deflate, cell[axis]-deflate
	}
	return w.BoxCollides(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2])
}

// IsShulkerBoxShape reports whether the state is a shulker box, whose
// collision is the lid's, not the state's.
func IsShulkerBoxShape(state uint16) bool {
	collisionTablesOnce.Do(initCollisionTables)
	return collisionShulker[state]
}

// ShulkerBoxBoxes is the box's cell-local collision: the cube grown toward
// its facing by half the lid's progress, the cube while the lid is CLOSED.
// vanilla: ShulkerBoxBlockEntity.getBoundingBox
func (w *World) ShulkerBoxBoxes(state uint16, x, y, z int32) []CollisionBox {
	box := CollisionBox{MaxX: 1, MaxY: 1, MaxZ: 1}
	v, open := w.shulkerLidsOpen.Load([3]int32{x, y, z})
	if !open {
		return []CollisionBox{box}
	}
	grow := 0.5 * float64(float32(v.(int))/shulkerLidTicks)
	switch registry.StateValue(state, registry.PropFacing) {
	case "down":
		box.MinY -= grow
	case "north":
		box.MinZ -= grow
	case "south":
		box.MaxZ += grow
	case "west":
		box.MinX -= grow
	case "east":
		box.MaxX += grow
	default:
		box.MaxY += grow
	}
	return []CollisionBox{box}
}
