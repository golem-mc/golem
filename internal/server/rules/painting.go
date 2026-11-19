package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
)

// The painting: the geometry of a HangingEntity-sized body on the wall it
// was placed against, and the cells that carry it. Who hears about it is the
// server's.

// PaintingCandidate is one painting variant a placement could hang: its ID in
// the minecraft:painting_variant registry and the size it covers.
type PaintingCandidate struct {
	Variant int32
	Width   int32
	Height  int32
}

// ChoosePainting is vanilla Painting.create's decision: of the candidates
// that survive where they would hang, only the largest by area are left, and
// pick chooses one of those — vanilla rolls the entity's own random.
func ChoosePainting(candidates []PaintingCandidate, survives func(PaintingCandidate) bool, pick func(n int) int) (PaintingCandidate, bool) {
	fitting := make([]PaintingCandidate, 0, len(candidates))
	for _, c := range candidates {
		if survives(c) {
			fitting = append(fitting, c)
		}
	}
	if len(fitting) == 0 {
		return PaintingCandidate{}, false
	}
	largest := 0
	for _, c := range fitting {
		largest = max(largest, int(c.Width)*int(c.Height))
	}
	largestFitting := fitting[:0]
	for _, c := range fitting {
		if int(c.Width)*int(c.Height) == largest {
			largestFitting = append(largestFitting, c)
		}
	}
	chosen := pick(len(largestFitting))
	if chosen < 0 || chosen >= len(largestFitting) {
		return PaintingCandidate{}, false
	}
	return largestFitting[chosen], true
}

// PaintingBox is the 1/16-thick box a painting occupies in its hanging cell
// against the wall. (x, y, z) is the cell the clicked face points into, dir
// the facing in packet numbering. Odd sizes centre on the cell; even ones
// shift half a block to the painting's left and up.
// vanilla: Painting.calculateBoundingBox
func PaintingBox(x, y, z, dir, width, height int32) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	// The point the box centres on: the cell's own centre, held a
	// thirty-second of a block off the wall's surface (vanilla's
	// relative(direction, -0.46875)).
	step := dirDeltas[int(dir)]
	cx := float64(x) + 0.5 - float64(step[0])*0.46875
	cy := float64(y) + 0.5 - float64(step[1])*0.46875
	cz := float64(z) + 0.5 - float64(step[2])*0.46875
	// An even size shifts the centre half a block toward the painting's
	// left across the wall, and an even height half a block up. Vanilla's
	// own offsets, 0.5 for even and nothing for odd.
	left := dirDeltas[blocklogic.CounterClockwise(int(dir))]
	if width%2 == 0 {
		cx += float64(left[0]) * 0.5
		cz += float64(left[2]) * 0.5
	}
	if height%2 == 0 {
		cy += 0.5
	}
	// The sliver is one deep along the facing axis and the variant's size
	// everywhere else.
	xSize, zSize := float64(width), float64(width)
	if step[0] != 0 {
		xSize = 0.0625
	}
	if step[2] != 0 {
		zSize = 0.0625
	}
	return cx - xSize/2, cy - float64(height)/2, cz - zSize/2,
		cx + xSize/2, cy + float64(height)/2, cz + zSize/2
}

// cellShave is the margin cells are enumerated off a box's bounds by, the
// vanilla deflate(1.0E-7) of the support box: a box laying its face exactly
// on a cell boundary does not cover the cell beyond it.
const cellShave = 1e-7

// paintingCells enumerates the block cells a box's bounds cover, each
// boundary shaved.
func paintingCells(minX, minY, minZ, maxX, maxY, maxZ float64) [][3]int32 {
	var cells [][3]int32
	for y := int32(math.Floor(minY + cellShave)); y <= int32(math.Floor(maxY-cellShave)); y++ {
		for z := int32(math.Floor(minZ + cellShave)); z <= int32(math.Floor(maxZ-cellShave)); z++ {
			for x := int32(math.Floor(minX + cellShave)); x <= int32(math.Floor(maxX-cellShave)); x++ {
				cells = append(cells, [3]int32{x, y, z})
			}
		}
	}
	return cells
}

// PaintingFrontCells lists the cells a painting runs across in front of its
// wall: the hanging layer it occupies, a block growing a collision into
// which pushes it off.
func PaintingFrontCells(x, y, z, dir, width, height int32) [][3]int32 {
	return paintingCells(PaintingBox(x, y, z, dir, width, height))
}

// PaintingSupportCells lists the wall cells that carry the painting: its box
// moved half a block into the wall, exactly the cells behind the ones it
// covers. vanilla: HangingEntity.calculateSupportBox
func PaintingSupportCells(x, y, z, dir, width, height int32) [][3]int32 {
	minX, minY, minZ, maxX, maxY, maxZ := PaintingBox(x, y, z, dir, width, height)
	// Half a block along the facing, backwards: into the wall.
	step := dirDeltas[int(dir)]
	minX -= float64(step[0]) * 0.5
	maxX -= float64(step[0]) * 0.5
	minY -= float64(step[1]) * 0.5
	maxY -= float64(step[1]) * 0.5
	minZ -= float64(step[2]) * 0.5
	maxZ -= float64(step[2]) * 0.5
	return paintingCells(minX, minY, minZ, maxX, maxY, maxZ)
}

// BoxesIntersect reports whether the two boxes overlap one another,
// strictly: two that only touch at a face do not. Out of vanilla's
// AABB.intersects, what the hanging entities' coexistence check asks.
func BoxesIntersect(minA, maxA, minB, maxB [3]float64) bool {
	return minA[0] < maxB[0] && maxA[0] > minB[0] &&
		minA[1] < maxB[1] && maxA[1] > minB[1] &&
		minA[2] < maxB[2] && maxA[2] > minB[2]
}
