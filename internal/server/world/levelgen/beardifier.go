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

// SelectBeards returns the rigid pieces within 12 blocks of chunk (cx, cz)
// and, of the pieces that near, the junctions within 12 blocks.
// vanilla: Beardifier.forStructuresInChunk
func SelectBeards(cx, cz int32, pieces []BeardPiece) ([]BeardRigid, []BeardJunction) {
	minX, minZ := int(cx)*16, int(cz)*16
	near := BoundingBox{minX - beardKernelRadius, math.MinInt32, minZ - beardKernelRadius,
		minX + 15 + beardKernelRadius, math.MaxInt32, minZ + 15 + beardKernelRadius}
	var rigids []BeardRigid
	var junctions []BeardJunction
	for _, p := range pieces {
		if p.Adjustment == AdjustNone || !p.Box.intersects(near) {
			continue
		}
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
