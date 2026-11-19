package rules

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The cushion: where one may be put down and whether one still rests on
// something. Who sees it and what it drops is the server's.

// cushionSlice is the depth of the anchor slice under the cushion and of the
// resting slice its bottom must not be buried in; cushionAnchorReach is how
// far below the anchor slice a shape's cell may start.
// vanilla: Cushion.hasAnchorBelow, Cushion.isAnchorBuried
const (
	cushionSlice       = 0.015625
	cushionAnchorReach = 0.125
)

// cushionCollisionShaped marks minecraft:cushion_uses_collision_shape,
// cushionFire minecraft:fire.
var (
	cushionCollisionShaped = registry.BlocksOf(func(b registry.Block) bool {
		return b.InTag(registry.BlockTagOf("minecraft:cushion_uses_collision_shape"))
	})
	cushionFire = registry.BlocksOf(func(b registry.Block) bool {
		return b.InTag(registry.BlockTagOf("minecraft:fire"))
	})
)

// cushionCollisionRayEpsilon carries the click's ray just past the outline
// it hit. vanilla: CushionItem.COLLISION_SHAPE_RAYCAST_EPSILON
const cushionCollisionRayEpsilon = 0.001

// CushionSurvives reports whether a cushion with the box rests on an
// outline within 1/64 below it and is not inside suffocating blocks
// everywhere it reaches.
// vanilla: Cushion.wouldSurviveAt
func CushionSurvives(w BlockReader, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return cushionHasAnchorBelow(w, minX, minY, minZ, maxX, maxZ) &&
		!cushionCoveredBySuffocatingBlocks(w, minX, minY, minZ, maxX, maxY, maxZ)
}

// CushionCanBePlacedAt is CushionSurvives with a bottom not buried in the
// collision of the blocks it rests among.
// vanilla: Cushion.canBePlacedAt
func CushionCanBePlacedAt(w BlockReader, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return CushionSurvives(w, minX, minY, minZ, maxX, maxY, maxZ) &&
		!cushionAnchorBuried(w, minX, minY, minZ, maxX, maxZ)
}

// CushionYaw is the yaw a placed cushion faces: the placer's, snapped to the
// nearest horizontal direction. vanilla: Direction.fromYRot, toYRot
func CushionYaw(yaw float32) float32 {
	return float32(int32(math.Floor(float64(yaw)/90+0.5))&3) * 90
}

// CushionClickOnCollision re-aims a click on a block of
// minecraft:cushion_uses_collision_shape (a cauldron's rim, a hopper's
// bowl) at the block's collision rather than its outline: the face and
// the click's height where the ray from the eye meets the collision,
// ok false to keep the click as it was.
// vanilla: CushionItem.recalculateContextForSpecialCollisionShapes
func CushionClickOnCollision(state uint16, x, y, z int32, eyeX, eyeY, eyeZ, clickX, clickY, clickZ float64) (face int32, hitY float64, ok bool) {
	if !cushionCollisionShaped[registry.BlockOf(state)] {
		return 0, 0, false
	}
	dx, dy, dz := clickX-eyeX, clickY-eyeY, clickZ-eyeZ
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length < 1e-4 {
		return 0, 0, false
	}
	scale := 1 + cushionCollisionRayEpsilon/length
	dx, dy, dz = dx*scale, dy*scale, dz*scale
	best := math.Inf(1)
	for _, b := range world.CollisionBoxes(state) {
		t, f, hit := world.SegmentAABB(eyeX, eyeY, eyeZ, dx, dy, dz,
			float64(x)+b.MinX, float64(y)+b.MinY, float64(z)+b.MinZ,
			float64(x)+b.MaxX, float64(y)+b.MaxY, float64(z)+b.MaxZ)
		if hit && t < best {
			best, face, hitY, ok = t, f, eyeY+t*dy, true
		}
	}
	return face, hitY, ok
}

func cushionHasAnchorBelow(w BlockReader, minX, minY, minZ, maxX, maxZ float64) bool {
	aMaxX, aMaxZ := math.Nextafter(maxX, math.Inf(-1)), math.Nextafter(maxZ, math.Inf(-1))
	aMinY := minY - cushionSlice
	for cx := floorCell(minX); cx <= floorCell(aMaxX); cx++ {
		for cy := floorCell(aMinY - cushionAnchorReach); cy <= floorCell(minY); cy++ {
			for cz := floorCell(minZ); cz <= floorCell(aMaxZ); cz++ {
				x0, y0, z0, x1, y1, z1, ok := world.OutlineBounds(w.BlockAt(cx, cy, cz))
				if !ok {
					continue
				}
				if float64(cx)+x0 < aMaxX && float64(cx)+x1 > minX &&
					float64(cy)+y0 < minY && float64(cy)+y1 > aMinY &&
					float64(cz)+z0 < aMaxZ && float64(cz)+z1 > minZ {
					return true
				}
			}
		}
	}
	return false
}

// cushionCoveredBySuffocatingBlocks reports whether every cell the box
// reaches into suffocates.
func cushionCoveredBySuffocatingBlocks(w BlockReader, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	up, down := math.Inf(1), math.Inf(-1)
	for cx := floorCell(math.Nextafter(minX, up)); cx <= floorCell(math.Nextafter(maxX, down)); cx++ {
		for cy := floorCell(math.Nextafter(minY, up)); cy <= floorCell(math.Nextafter(maxY, down)); cy++ {
			for cz := floorCell(math.Nextafter(minZ, up)); cz <= floorCell(math.Nextafter(maxZ, down)); cz++ {
				if !IsSuffocating(w.BlockAt(cx, cy, cz)) {
					return false
				}
			}
		}
	}
	return true
}

// cushionAnchorBuried reports whether the collision of the blocks around
// the cushion's bottom fills the thin slice it rests in.
func cushionAnchorBuried(w BlockReader, minX, minY, minZ, maxX, maxZ float64) bool {
	up, down := math.Inf(1), math.Inf(-1)
	slice := world.CollisionBox{
		MinX: math.Nextafter(minX, up), MinY: math.Nextafter(minY, up), MinZ: math.Nextafter(minZ, up),
		MaxX: math.Nextafter(maxX, down), MaxY: math.Nextafter(minY+cushionSlice, down), MaxZ: math.Nextafter(maxZ, down),
	}
	var colliders []world.CollisionBox
	// The cell below counts too: a fence's or a wall's collision reaches
	// half a block over its own.
	for cx := floorCell(slice.MinX); cx <= floorCell(slice.MaxX); cx++ {
		for cy := floorCell(slice.MinY) - 1; cy <= floorCell(slice.MaxY); cy++ {
			for cz := floorCell(slice.MinZ); cz <= floorCell(slice.MaxZ); cz++ {
				for _, b := range world.CollisionBoxes(w.BlockAt(cx, cy, cz)) {
					c := world.CollisionBox{
						MinX: max(slice.MinX, float64(cx)+b.MinX), MinY: max(slice.MinY, float64(cy)+b.MinY), MinZ: max(slice.MinZ, float64(cz)+b.MinZ),
						MaxX: min(slice.MaxX, float64(cx)+b.MaxX), MaxY: min(slice.MaxY, float64(cy)+b.MaxY), MaxZ: min(slice.MaxZ, float64(cz)+b.MaxZ),
					}
					if c.MinX < c.MaxX && c.MinY < c.MaxY && c.MinZ < c.MaxZ {
						colliders = append(colliders, c)
					}
				}
			}
		}
	}
	return len(colliders) > 0 && boxesCover(slice, colliders)
}

// boxesCover reports whether the boxes, each within the target, fill it
// whole: every cell of the grid their edges cut the target into lies in one
// of them.
// vanilla: Shapes.join(exposed, collider, BooleanOp.ONLY_FIRST).isEmpty()
func boxesCover(target world.CollisionBox, boxes []world.CollisionBox) bool {
	xs := []float64{target.MinX, target.MaxX}
	ys := []float64{target.MinY, target.MaxY}
	zs := []float64{target.MinZ, target.MaxZ}
	for _, b := range boxes {
		xs = append(xs, b.MinX, b.MaxX)
		ys = append(ys, b.MinY, b.MaxY)
		zs = append(zs, b.MinZ, b.MaxZ)
	}
	for _, edges := range []*[]float64{&xs, &ys, &zs} {
		slices.Sort(*edges)
		*edges = slices.Compact(*edges)
	}
	for i := range len(xs) - 1 {
		for j := range len(ys) - 1 {
			for k := range len(zs) - 1 {
				mx, my, mz := (xs[i]+xs[i+1])/2, (ys[j]+ys[j+1])/2, (zs[k]+zs[k+1])/2
				if !slices.ContainsFunc(boxes, func(b world.CollisionBox) bool {
					return mx > b.MinX && mx < b.MaxX && my > b.MinY && my < b.MaxY && mz > b.MinZ && mz < b.MaxZ
				}) {
					return false
				}
			}
		}
	}
	return true
}

func floorCell(v float64) int32 { return int32(math.Floor(v)) }

// CushionInFire reports whether a block of minecraft:fire lies in a cell the
// box reaches into. vanilla: Cushion.destroyIfInFire
func CushionInFire(w BlockReader, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	up, down := math.Inf(1), math.Inf(-1)
	for cx := floorCell(math.Nextafter(minX, up)); cx <= floorCell(math.Nextafter(maxX, down)); cx++ {
		for cy := floorCell(math.Nextafter(minY, up)); cy <= floorCell(math.Nextafter(maxY, down)); cy++ {
			for cz := floorCell(math.Nextafter(minZ, up)); cz <= floorCell(math.Nextafter(maxZ, down)); cz++ {
				if cushionFire[registry.BlockOf(w.BlockAt(cx, cy, cz))] {
					return true
				}
			}
		}
	}
	return false
}
