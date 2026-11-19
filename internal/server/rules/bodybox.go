package rules

// BodyBox is one live body's box as a disk's unobstructed test reads it: the
// feet centre, and the box's width and height.
type BodyBox struct {
	X, Y, Z       float64
	Width, Height float64
}

// BodyBoxesReach reports whether a body's box reaches the whole block at (x,
// y, z), the intersection vanilla's Shapes.joinIsNotEmpty takes strictly.
// vanilla: EntityGetter.isUnobstructed
func BodyBoxesReach(bodies []BodyBox, x, y, z int32) bool {
	for _, b := range bodies {
		if b.X+b.Width/2 > float64(x) && b.X-b.Width/2 < float64(x)+1 &&
			b.Y+b.Height > float64(y) && b.Y < float64(y)+1 &&
			b.Z+b.Width/2 > float64(z) && b.Z-b.Width/2 < float64(z)+1 {
			return true
		}
	}
	return false
}

// BodyBoxOfBox reads a box as a body: the feet centre, the width and the height.
func BodyBoxOfBox(minX, minY, minZ, maxX, maxY, maxZ float64) BodyBox {
	return BodyBox{
		X: (minX + maxX) / 2, Y: minY, Z: (minZ + maxZ) / 2,
		Width: maxX - minX, Height: maxY - minY,
	}
}
