package entity

import "github.com/golem-mc/golem/registry"

// FrogspawnInside appends to buf[:0] the frogspawn cells touching the inset body box.
// vanilla: Entity.checkInsideBlocks, FrogspawnBlock.entityInside
func (f *FallingBlock) FrogspawnInside(w World, buf []Waypoint) []Waypoint {
	buf = buf[:0]
	minX, minY, minZ, maxX, maxY, maxZ := f.HurtBox()
	const eps = 1e-5
	for x := floorI(minX + eps); x <= floorI(maxX-eps); x++ {
		for y := floorI(minY + eps); y <= floorI(maxY-eps); y++ {
			for z := floorI(minZ + eps); z <= floorI(maxZ-eps); z++ {
				if registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockFrogspawn {
					buf = append(buf, Waypoint{x, y, z})
				}
			}
		}
	}
	return buf
}
