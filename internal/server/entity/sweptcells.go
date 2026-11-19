package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
)

// cellVisitor sees one cell of a swept walk and the step it falls in, and
// returns false to stop the walk.
// vanilla: BlockGetter.BlockStepVisitor
type cellVisitor func(x, y, z int32, step int) bool

// forEachCellBetween yields the cells a box sweeps over moving from f to t,
// box being the box at t: the cells at the start (step 0), those the leading
// corner meets on the way (steps 1, 2, ...), then the rest of the box at the
// target, each cell once. A move shorter than 1e-5 yields only the cells of
// box, all at step 0. It returns false when visit stopped the walk.
// vanilla: BlockGetter.forEachBlockIntersectedBetween
func forEachCellBetween(fx, fy, fz, tx, ty, tz float64, box AABB, visit cellVisitor) bool {
	dx, dy, dz := tx-fx, ty-fy, tz-fz
	if dx*dx+dy*dy+dz*dz < float64(float32(1e-5)*float32(1e-5)) {
		x0, y0, z0 := floorI(box.minX), floorI(box.minY), floorI(box.minZ)
		x1, y1, z1 := floorI(box.maxX), floorI(box.maxY), floorI(box.maxZ)
		// betweenClosed: x fastest, z outermost.
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					if !visit(x, y, z, 0) {
						return false
					}
				}
			}
		}
		return true
	}
	var buf [32][3]int32
	seen := buf[:0]
	add := func(x, y, z int32) bool {
		if slices.Contains(seen, [3]int32{x, y, z}) {
			return false
		}
		seen = append(seen, [3]int32{x, y, z})
		return true
	}
	start := box.moved(-dx, -dy, -dz)
	ok := cellsInDirection(floorI(start.minX), floorI(start.minY), floorI(start.minZ),
		floorI(start.maxX), floorI(start.maxY), floorI(start.maxZ), dx, dy, dz,
		func(x, y, z int32) bool {
			add(x, y, z)
			return visit(x, y, z, 0)
		})
	if !ok {
		return false
	}
	steps, ok := cellsAlongTravel(add, dx, dy, dz, box, visit)
	if !ok {
		return false
	}
	return cellsInDirection(floorI(box.minX), floorI(box.minY), floorI(box.minZ),
		floorI(box.maxX), floorI(box.maxY), floorI(box.maxZ), dx, dy, dz,
		func(x, y, z int32) bool {
			return !add(x, y, z) || visit(x, y, z, steps+1)
		})
}

// cellsAlongTravel walks the cells the box's leading corner crosses on the
// way to box, each crossing a step, and yields the not yet seen cells of the
// box's cross-section there. It returns the number of steps, and false when
// visit stopped the walk.
// vanilla: BlockGetter.addCollisionsAlongTravel
func cellsAlongTravel(add func(x, y, z int32) bool, dx, dy, dz float64, box AABB, visit cellVisitor) (int, bool) {
	sizeX, sizeY, sizeZ := box.maxX-box.minX, box.maxY-box.minY, box.maxZ-box.minZ
	cx, cy, cz := furthestCorner(dx, dy, dz)
	toX := (box.minX+box.maxX)/2 + sizeX*0.5*float64(cx)
	toY := (box.minY+box.maxY)/2 + sizeY*0.5*float64(cy)
	toZ := (box.minZ+box.maxZ)/2 + sizeZ*0.5*float64(cz)
	fromX, fromY, fromZ := toX-dx, toY-dy, toZ-dz
	bx, by, bz := floorI(fromX), floorI(fromY), floorI(fromZ)
	sx, sy, sz := signInt(dx), signInt(dy), signInt(dz)
	tDelta := func(s int32, d float64) float64 {
		if s == 0 {
			return math.MaxFloat64
		}
		return float64(s) / d
	}
	tFirst := func(s int32, delta, from float64) float64 {
		frac := from - math.Floor(from)
		if s > 0 {
			return delta * (1 - frac)
		}
		return delta * frac
	}
	tdx, tdy, tdz := tDelta(sx, dx), tDelta(sy, dy), tDelta(sz, dz)
	tX, tY, tZ := tFirst(sx, tdx, fromX), tFirst(sy, tdy, fromY), tFirst(sz, tdz, fromZ)
	const edge = float64(float32(1e-5))
	steps := 0
	for tX <= 1 || tY <= 1 || tZ <= 1 {
		switch {
		case tX < tY && tX < tZ:
			bx += sx
			tX += tdx
		case tX < tY || tY >= tZ:
			bz += sz
			tZ += tdz
		default:
			by += sy
			tY += tdy
		}
		cell := AABB{float64(bx), float64(by), float64(bz), float64(bx + 1), float64(by + 1), float64(bz + 1)}
		s, hit := cell.clip(fromX, fromY, fromZ, dx, dy, dz)
		if !hit {
			continue
		}
		hx := min(max(fromX+s*dx, float64(bx)+edge), float64(bx)+1-edge)
		hy := min(max(fromY+s*dy, float64(by)+edge), float64(by)+1-edge)
		hz := min(max(fromZ+s*dz, float64(bz)+edge), float64(bz)+1-edge)
		ox := floorI(hx - sizeX*float64(cx))
		oy := floorI(hy - sizeY*float64(cy))
		oz := floorI(hz - sizeZ*float64(cz))
		steps++
		step := steps
		ok := cellsInDirection(bx, by, bz, ox, oy, oz, dx, dy, dz, func(x, y, z int32) bool {
			return !add(x, y, z) || visit(x, y, z, step)
		})
		if !ok {
			return steps, false
		}
	}
	return steps, true
}

// furthestCorner is the corner of the box, as signs per axis, whose path
// the travel walk follows. Kept as vanilla computes it, axis swaps and all.
// vanilla: BlockGetter.getFurthestCorner
func furthestCorner(dx, dy, dz float64) (int32, int32, int32) {
	sign := func(d float64) int32 {
		if d >= 0 {
			return 1
		}
		return -1
	}
	xs, ys, zs := sign(dx), sign(dy), sign(dz)
	ax, ay, az := math.Abs(dx), math.Abs(dy), math.Abs(dz)
	if ax <= ay && ax <= az {
		return -xs, -zs, ys
	}
	if ay <= az {
		return zs, -ys, -xs
	}
	return -ys, xs, -zs
}

// cellsInDirection yields the cells between two corners, starting from the
// corner the direction leaves and walking y first, then the larger of the
// horizontal axes, innermost the other. It returns false when yield did.
// vanilla: BlockPos.betweenCornersInDirection, Direction.axisStepOrder
func cellsInDirection(x0, y0, z0, x1, y1, z1 int32, dx, dy, dz float64, yield func(x, y, z int32) bool) bool {
	start := func(a, b int32, d float64) (int32, int32, int32) {
		lo, hi := min(a, b), max(a, b)
		if d >= 0 {
			return lo, 1, hi - lo
		}
		return hi, -1, hi - lo
	}
	sx, stepX, nx := start(x0, x1, dx)
	sy, stepY, ny := start(y0, y1, dy)
	sz, stepZ, nz := start(z0, z1, dz)
	for i := int32(0); i <= ny; i++ {
		y := sy + stepY*i
		if math.Abs(dx) < math.Abs(dz) {
			// YZX: z second, x innermost.
			for j := int32(0); j <= nz; j++ {
				for k := int32(0); k <= nx; k++ {
					if !yield(sx+stepX*k, y, sz+stepZ*j) {
						return false
					}
				}
			}
			continue
		}
		for j := int32(0); j <= nx; j++ {
			for k := int32(0); k <= nz; k++ {
				if !yield(sx+stepX*j, y, sz+stepZ*k) {
					return false
				}
			}
		}
	}
	return true
}

// signInt is Mth.sign: -1, 0 or 1.
func signInt(d float64) int32 {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// bodyMove is one movement of a body's tick, between its positions. One that
// Entity.move made is axis-split: it walks y, then the horizontal axes in the
// order of the movement it was asked for (ax, az).
// vanilla: Entity.Movement
type bodyMove struct {
	fx, fy, fz, tx, ty, tz float64
	ax, az                 float64
	axisSplit              bool
}

// insideVisitor sees one non-air cell of an inside walk, its state, the step
// it falls in, the body's box at the leg's start and the leg's travel, and
// returns false to stop the walk.
type insideVisitor func(x, y, z int32, state uint16, step int, from AABB, dx, dy, dz float64) bool

// insideStepBudget is checkInsideBlocks' maxMovementIterations: a move's legs
// share this many steps, and a move that spends them ends with the cells at
// its target.
const insideStepBudget = 16

// walkInside visits, once each, the non-air cells the box swept on the moves,
// rel being the box with the body at the origin. Every non-air cell counts as
// met, as a full entity-inside shape does.
// vanilla: Entity.checkInsideBlocks
func walkInside(w World, moves []bodyMove, rel AABB, visit insideVisitor) {
	var buf [32][3]int32
	visited := buf[:0]
	stopped := false
	// leg walks one stretch within the budget left and returns the steps it
	// spent: the last step it reached, plus one.
	leg := func(fx, fy, fz, tx, ty, tz float64, budget int) int {
		if stopped {
			return 0
		}
		box := rel.moved(tx, ty, tz)
		box.deflate(insideShave)
		from := rel.moved(fx, fy, fz)
		dx, dy, dz := tx-fx, ty-fy, tz-fz
		last := 0
		forEachCellBetween(fx, fy, fz, tx, ty, tz, box, func(x, y, z int32, step int) bool {
			if step >= budget {
				return false
			}
			last = step
			state := w.BlockAt(x, y, z)
			if rules.IsAir(state) || slices.Contains(visited, [3]int32{x, y, z}) {
				return true
			}
			visited = append(visited, [3]int32{x, y, z})
			if !visit(x, y, z, state, step, from, dx, dy, dz) {
				stopped = true
				return false
			}
			return true
		})
		return last + 1
	}
	for _, m := range moves {
		budget := insideStepBudget
		dx, dy, dz := m.tx-m.fx, m.ty-m.fy, m.tz-m.fz
		if m.axisSplit && dx*dx+dy*dy+dz*dz > 0 {
			// vanilla: Direction.axisStepOrder, YZX when |x| < |z|, else YXZ
			px, py, pz := m.fx, m.fy, m.fz
			axes := [3]int{1, 0, 2}
			if math.Abs(m.ax) < math.Abs(m.az) {
				axes = [3]int{1, 2, 0}
			}
			for _, axis := range axes {
				nx, ny, nz := px, py, pz
				switch {
				case axis == 0 && dx != 0:
					nx += dx
				case axis == 1 && dy != 0:
					ny += dy
				case axis == 2 && dz != 0:
					nz += dz
				default:
					continue
				}
				budget -= leg(px, py, pz, nx, ny, nz, budget)
				px, py, pz = nx, ny, nz
			}
		} else {
			budget -= leg(m.fx, m.fy, m.fz, m.tx, m.ty, m.tz, budget)
		}
		if budget <= 0 {
			leg(m.tx, m.ty, m.tz, m.tx, m.ty, m.tz, 1)
		}
		if stopped {
			return
		}
	}
}

// sweptFluid is the fluid of the cell whose surface the box met moving by
// (dx, dy, dz), or fluidNone.
// vanilla: Entity.collidedWithFluid, Fluid.getAABB
func sweptFluid(w World, x, y, z int32, state uint16, from AABB, dx, dy, dz float64) fluidKind {
	cell := fluidCellOf(state)
	if cell.kind == fluidNone {
		return fluidNone
	}
	h := float32(fluidHeightAt(w, x, y, z, cell))
	surface := AABB{float64(x), float64(y), float64(z), float64(x + 1), float64(float32(y) + h), float64(z + 1)}
	if !from.collidedAlongVector(dx, dy, dz, surface) {
		return fluidNone
	}
	return cell.kind
}

// collidedAlongVector reports whether the box, moving by (dx, dy, dz), meets
// part.
// vanilla: AABB.collidedAlongVector
func (b AABB) collidedAlongVector(dx, dy, dz float64, part AABB) bool {
	const eps = 1e-7
	cx, cy, cz := (b.minX+b.maxX)/2, (b.minY+b.maxY)/2, (b.minZ+b.maxZ)/2
	grown := part.inflate((b.maxX-b.minX)*0.5-eps, (b.maxY-b.minY)*0.5-eps, (b.maxZ-b.minZ)*0.5-eps)
	if grown.contains(cx+dx, cy+dy, cz+dz) || grown.contains(cx, cy, cz) {
		return true
	}
	_, hit := grown.clip(cx, cy, cz, dx, dy, dz)
	return hit
}
