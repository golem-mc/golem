package levelgen

import "math"

// beardKernelRadius is how far, in blocks, a piece or junction bends the
// terrain. vanilla: Beardifier.BEARD_KERNEL_RADIUS
const beardKernelRadius = 12

// BeardRigid is a piece the terrain is fitted to: its box, its structure's
// adaptation and the height of its ground above the box's bottom.
// vanilla: Beardifier.Rigid
type BeardRigid struct {
	Box              BoundingBox
	Adjustment       TerrainAdjustment
	GroundLevelDelta int
}

// BeardJunction is the joint of two jigsaw pieces the terrain is smoothed
// towards. vanilla: JigsawJunction
type BeardJunction struct{ X, GroundY, Z int }

// BeardPiece is a structure piece as the beardifier reads it. A pool element
// that is not Rigid (terrain_matching) bends the terrain only at its
// junctions; a piece that is not a pool element is rigid with no delta and no
// junctions.
type BeardPiece struct {
	Box              BoundingBox
	Adjustment       TerrainAdjustment
	Rigid            bool
	GroundLevelDelta int
	Junctions        []BeardJunction
}

// BeardNear is the box, unbounded in Y, within which a piece shapes chunk
// (cx, cz).
func BeardNear(cx, cz int32) BoundingBox {
	minX, minZ := int(cx)*16, int(cz)*16
	return BoundingBox{minX - beardKernelRadius, math.MinInt32, minZ - beardKernelRadius,
		minX + 15 + beardKernelRadius, math.MaxInt32, minZ + 15 + beardKernelRadius}
}

// Intersects reports whether the boxes share a block.
func (b BoundingBox) Intersects(o BoundingBox) bool { return b.intersects(o) }

// SelectBeards returns the rigid pieces of pieces, which the caller has
// already limited to those adapting the terrain and within near, and the
// junctions strictly inside near. vanilla: Beardifier.forStructuresInChunk
func SelectBeards(near BoundingBox, pieces []BeardPiece) ([]BeardRigid, []BeardJunction) {
	var rigids []BeardRigid
	var junctions []BeardJunction
	for _, p := range pieces {
		if p.Rigid {
			rigids = append(rigids, BeardRigid{p.Box, p.Adjustment, p.GroundLevelDelta})
		}
		for _, j := range p.Junctions {
			if j.X > near.MinX && j.Z > near.MinZ && j.X < near.MaxX && j.Z < near.MaxZ {
				junctions = append(junctions, j)
			}
		}
	}
	return rigids, junctions
}
