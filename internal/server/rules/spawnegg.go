package rules

import "github.com/golem-mc/golem/internal/protocol"

// SpawnEggWorld is what a spawn egg used on a block reads of the world.
type SpawnEggWorld interface {
	CollisionEmpty(x, y, z int32) bool
	SpawnYOffset(x, y, z int32, movedUp bool, halfWidth float64) float64
}

// SpawnEggPosition is where SpawnEggItem.useOn stands a body halfWidth wide
// used on the given face of the block at x, y, z: inside the clicked block
// when it has no collision shape, else in the cell on the face, then
// dropped onto what that cell holds (EntityType.create with tryMoveDown).
// sx, sy, sz is that cell before the drop, where the placement event goes.
func SpawnEggPosition(w SpawnEggWorld, x, y, z, face int32, halfWidth float64) (px, py, pz float64, sx, sy, sz int32) {
	sx, sy, sz = x, y, z
	if !w.CollisionEmpty(x, y, z) {
		off := protocol.FaceOffset[face]
		sx, sy, sz = x+off[0], y+off[1], z+off[2]
	}
	movedUp := sy != y && face == protocol.FaceTop
	return float64(sx) + 0.5, float64(sy) + w.SpawnYOffset(sx, sy, sz, movedUp, halfWidth), float64(sz) + 0.5, sx, sy, sz
}

// DispenserEggPosition is where a dispenser facing face stands the body of an
// egg it fires: the cell in front, dropped onto what that cell holds unless
// the dispenser faces up.
// vanilla: SpawnEggItemBehavior.execute, EntityType.create
func DispenserEggPosition(w SpawnEggWorld, x, y, z, face int32, halfWidth float64) (px, py, pz float64) {
	off := protocol.FaceOffset[face]
	sx, sy, sz := x+off[0], y+off[1], z+off[2]
	dy := 0.0
	if face != protocol.FaceTop {
		dy = w.SpawnYOffset(sx, sy, sz, false, halfWidth)
	}
	return float64(sx) + 0.5, float64(sy) + dy, float64(sz) + 0.5
}
