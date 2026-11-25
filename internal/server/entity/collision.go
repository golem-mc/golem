package entity

import (
	"math"
	"math/rand"
	"slices"
	"sort"
	"sync"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// Box collision: the per-axis clip against blocks' real collision shapes,
// and the overlap test for item burial and step-up headroom.

// canStandOn checks the top face for a body that cannot sneak.
// vanilla: BlockStateBase.entityCanStandOn
func canStandOn(state uint16, cellY int32, feetY float64) bool {
	return faceFullFor(state, cellY, feetY, faceUp)
}

// faceFullFor is faceFull for the block at row cellY as a body with its feet
// at feetY collides with it: scaffolding's shape depends on the feet.
// vanilla: BlockBehaviour.isFaceFull, ScaffoldingBlock.getCollisionShape
func faceFullFor(state uint16, cellY int32, feetY float64, face uint8) bool {
	if boxes, ok := world.ScaffoldingBoxes(state, cellY, feetY); ok {
		// Only a plate spanning the whole cell can fill a scaffold's top or bottom face.
		for _, b := range boxes {
			if b.MinX == 0 && b.MaxX == 1 && b.MinZ == 0 && b.MaxZ == 1 &&
				(face == faceUp && b.MaxY == 1 || face == faceDown && b.MinY == 0) {
				return true
			}
		}
		return false
	}
	return faceFull(state, face)
}

// The axes a clip moves along, the order the box's coordinates index by.
const (
	axisX = 0
	axisY = 1
	axisZ = 2
)

// collideCtx is what powder snow's per-entity collision reads of the moving
// body: its banked fall, whether it is a falling block, and whether it can
// stand on powder snow. The zero value falls through. Sneaking is not
// modelled; no mob sneaks.
// vanilla: PowderSnowBlock.getCollisionShape
type collideCtx struct {
	fallDistance float64
	fallingBlock bool
	walker       bool
	// lavaWalker is a body that stands on a lava surface.
	lavaWalker bool
	// noBorder is a move without a body, which the world border does not stop.
	noBorder bool
	// borders is set once the world border's walls for this move are in
	// walls; clipMove computes them itself otherwise.
	borders   bool
	walls     []AABB
	moveCells *moveCells
}

// fallCtx is a body that powder snow does not hold up but whose banked fall
// can sink through it.
func fallCtx(fallDistance float64) collideCtx {
	return collideCtx{fallDistance: fallDistance}
}

// bankFall banks the drop from oldY to newY into *fall, or clears it when
// the move ended on the ground. It returns the fall a landing ended (the
// banked fall and this move's drop), 0 when the body did not land.
// vanilla: Entity.checkFallDamage
func bankFall(fall *float64, oldY, newY float64, onGround bool) (landed float64) {
	if onGround {
		landed = *fall + max(0, oldY-newY)
		*fall = 0
	} else if dy := newY - oldY; dy < -physicsEpsilon {
		*fall += -dy
	}
	return landed
}

// CapFallAccumulation caps *fall at 1 for a body that is not falling fast.
// vanilla: Entity.checkFallDistanceAccumulation
func CapFallAccumulation(vy float64, fall *float64) {
	if vy > -0.5 && *fall > 1 {
		*fall = 1
	}
}

// noCtx is the context of a body powder snow never holds up.
var noCtx collideCtx

var (
	powderSnowFull = []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}
	// powderSnowFalling is FALLING_COLLISION_SHAPE, 0.9f high.
	powderSnowFalling = []world.CollisionBox{{MaxX: 1, MaxY: float64(float32(0.9)), MaxZ: 1}}
)

// powderSnowBoxes is powder snow's collision for body c with its box floor
// at bottom, in cell row cellY.
// vanilla: PowderSnowBlock.getCollisionShape, EntityCollisionContext.isAbove
func powderSnowBoxes(c collideCtx, cellY int32, bottom float64) []world.CollisionBox {
	if c.fallDistance > 2.5 {
		return powderSnowFalling
	}
	if c.fallingBlock || c.walker && world.BodyAbove(bottom, float64(cellY)+1) {
		return powderSnowFull
	}
	return nil
}

// cellScratch pools the sweeps' cell buffers so a warm sweep allocates
// nothing.
var cellScratch = sync.Pool{New: func() any { return new([]uint16) }}

var moveCellScratch = sync.Pool{New: func() any { return new([]world.CollisionCell) }}

// collisionCellWorld supplies sparse reads when the world has section storage.
type collisionCellWorld interface {
	CollisionCellsIn(x0, y0, z0, x1, y1, z1 int32, buf []world.CollisionCell) []world.CollisionCell
}

// collisionCellsIn reads possible collisions in x/y/z order.
func collisionCellsIn(w World, x0, y0, z0, x1, y1, z1 int32, buf []world.CollisionCell) []world.CollisionCell {
	if cw, ok := w.(collisionCellWorld); ok {
		return cw.CollisionCellsIn(x0, y0, z0, x1, y1, z1, buf)
	}
	scratch := cellScratch.Get().(*[]uint16)
	defer cellScratch.Put(scratch)
	states := w.BlocksIn(x0, y0, z0, x1, y1, z1, *scratch)
	*scratch = states
	buf = buf[:0]
	i := 0
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z, i = z+1, i+1 {
				if world.MayCollide(states[i]) {
					_, plain := world.PlainCollision(states[i])
					buf = append(buf, world.CollisionCell{X: x, Y: y, Z: z, State: states[i], Plain: plain})
				}
			}
		}
	}
	return buf
}

// moveCells holds block states for one move; shapes still read the current
// body context and moving block entities on each clip.
type moveCells struct {
	x0, y0, z0, x1, y1, z1 int32
	cells                  []world.CollisionCell
	scratch                *[]world.CollisionCell
}

// gatherMoveCells includes the padded swept box and the mob's step-up retry.
// vanilla: Entity.collideBoundingBox, Entity.collide
func gatherMoveCells(w World, b AABB, dx, dy, dz, step float64) moveCells {
	// A vertical-only move has one leading-face query already; gathering the
	// full body's height would read extra cells without sharing them.
	if dx == 0 && dz == 0 {
		return moveCells{}
	}
	c := moveCells{
		x0:      int32(math.Floor(b.minX+min(0, dx))) - 1,
		y0:      int32(math.Floor(b.minY+min(0, dy))) - 1,
		z0:      int32(math.Floor(b.minZ+min(0, dz))) - 1,
		x1:      int32(math.Floor(b.maxX+max(0, dx))) + 1,
		y1:      int32(math.Floor(b.maxY+max(0, dy)+step)) + 1,
		z1:      int32(math.Floor(b.maxZ+max(0, dz))) + 1,
		scratch: moveCellScratch.Get().(*[]world.CollisionCell),
	}
	c.cells = collisionCellsIn(w, c.x0, c.y0, c.z0, c.x1, c.y1, c.z1, *c.scratch)
	*c.scratch = c.cells
	return c
}

func (c *moveCells) release() {
	if c.scratch != nil {
		moveCellScratch.Put(c.scratch)
	}
}

func (c *moveCells) contains(x0, y0, z0, x1, y1, z1 int32) bool {
	return c != nil && c.scratch != nil && x0 >= c.x0 && y0 >= c.y0 && z0 >= c.z0 &&
		x1 <= c.x1 && y1 <= c.y1 && z1 <= c.z1
}

// blockBoxes is a state's contextless collision, where powder snow's is
// empty.
func blockBoxes(state uint16) []world.CollisionBox {
	return world.CollisionBoxes(state)
}

// cellBoxes is the cell's collision for body c with box floor at bottom:
// powder snow's per-body shape, a moving piston's ride, scaffolding's
// body-relative shape, else the state's.
func cellBoxes(w World, c collideCtx, state uint16, x, y, z int32, bottom float64) []world.CollisionBox {
	if state == powderSnowState {
		return powderSnowBoxes(c, y, bottom)
	}
	if c.lavaWalker && rules.IsLava(state) {
		return lavaStandBoxes(w, state, x, y, z, bottom)
	}
	if boxes, ok := world.ScaffoldingBoxes(state, y, bottom); ok {
		return boxes
	}
	if world.IsMovingPiston(state) {
		return w.MovingPistonBoxes(x, y, z)
	}
	if world.IsShulkerBoxShape(state) {
		return w.ShulkerBoxBoxes(state, x, y, z)
	}
	return blockBoxes(state)
}

// coreMargin widens the core cell range past the stop test's physicsEpsilon
// and the rounding of the coordinates it compares.
const coreMargin = 1e-6

// clipMove is Shapes.collide on one axis: how far box b's leading face may
// travel by d before entering a shape. It returns the box moved (flush
// against the stopper, or the full distance), obstacleTop (the highest top
// it stopped on, for the step-up) and blocked. A shape that only touches
// the box shares nothing with it, so a resting body slides freely. The
// world's collidable bodies stop the move too.
// vanilla: Shapes.collide / VoxelShape.collide, Entity.collide
func clipMove(w World, c collideCtx, b AABB, axis int, d float64) (AABB, float64, bool) {
	if d == 0 {
		return b, math.Inf(-1), false
	}
	lo := [3]float64{b.minX, b.minY, b.minZ}
	hi := [3]float64{b.maxX, b.maxY, b.maxZ}
	sweptLo, sweptHi := lo, hi
	if d > 0 {
		sweptHi[axis] += d
	} else {
		sweptLo[axis] += d
	}
	// A shape must cross the box on both other axes to be in the way.
	p, q := (axis+1)%3, (axis+2)%3
	allowed := d
	obstacleTop := math.Inf(-1)
	// stop clamps the leading face to an obstacle crossing both other axes;
	// the tightest clamp's top is the step-up obstacle.
	stop := func(oLo, oHi [3]float64) {
		if hi[p] <= oLo[p] || lo[p] >= oHi[p] || hi[q] <= oLo[q] || lo[q] >= oHi[q] {
			return
		}
		if d > 0 && hi[axis] <= oLo[axis] {
			gap := oLo[axis] - hi[axis]
			if gap < allowed {
				allowed = gap
			}
			if gap <= allowed+physicsEpsilon {
				obstacleTop = math.Max(obstacleTop, oHi[1])
			}
		} else if d < 0 && lo[axis] >= oHi[axis] {
			gap := oHi[axis] - lo[axis]
			if gap > allowed {
				allowed = gap
			}
			if gap >= allowed-physicsEpsilon {
				obstacleTop = math.Max(obstacleTop, oHi[1])
			}
		}
	}
	// One cell past the swept span on every axis, since a shape can reach
	// out of its cell (a fence's 24/16).
	x0, x1 := int32(math.Floor(sweptLo[0]))-1, int32(math.Floor(sweptHi[0]))+1
	y0, y1 := int32(math.Floor(sweptLo[1]))-1, int32(math.Floor(sweptHi[1]))+1
	z0, z1 := int32(math.Floor(sweptLo[2]))-1, int32(math.Floor(sweptHi[2]))+1
	cells := c.moveCells
	if !cells.contains(x0, y0, z0, x1, y1, z1) {
		scratch := moveCellScratch.Get().(*[]world.CollisionCell)
		defer moveCellScratch.Put(scratch)
		states := collisionCellsIn(w, x0, y0, z0, x1, y1, z1, *scratch)
		*scratch = states
		cells = &moveCells{x0: x0, y0: y0, z0: z0, x1: x1, y1: y1, z1: z1, cells: states}
	}
	// A shape inside its own cell can only matter in the cells the box or
	// its sweep touches; the ring beyond is read for the shapes that reach
	// out of theirs.
	var coreLo, coreHi [3]int32
	for k := range 3 {
		switch {
		case k == axis && d > 0:
			coreLo[k], coreHi[k] = int32(math.Floor(hi[k]-1-coreMargin)), int32(math.Floor(sweptHi[k]+coreMargin))
		case k == axis:
			coreLo[k], coreHi[k] = int32(math.Floor(sweptLo[k]-1-coreMargin)), int32(math.Floor(lo[k]+coreMargin))
		default:
			coreLo[k], coreHi[k] = int32(math.Floor(lo[k])), int32(math.Floor(hi[k]))
		}
	}
	// A plain shape is checked against the core only, so the core is cut to
	// the cells read.
	coreLo[0], coreHi[0] = max(coreLo[0], x0), min(coreHi[0], x1)
	coreLo[1], coreHi[1] = max(coreLo[1], y0), min(coreHi[1], y1)
	coreLo[2], coreHi[2] = max(coreLo[2], z0), min(coreHi[2], z1)
	for _, cell := range cells.cells {
		x, y, z, state := cell.X, cell.Y, cell.Z, cell.State
		if cell.Plain {
			if y < coreLo[1] || y > coreHi[1] || x < coreLo[0] || x > coreHi[0] || z < coreLo[2] || z > coreHi[2] {
				continue
			}
			fx, fy, fz := float64(x), float64(y), float64(z)
			for _, s := range blockBoxes(state) {
				stop([3]float64{fx + s.MinX, fy + s.MinY, fz + s.MinZ}, [3]float64{fx + s.MaxX, fy + s.MaxY, fz + s.MaxZ})
			}
			continue
		}
		if x < x0 || x > x1 || y < y0 || y > y1 || z < z0 || z > z1 {
			continue
		}
		boxes := cellBoxes(w, c, state, x, y, z, b.minY)
		if len(boxes) == 0 {
			continue
		}
		ox, oz := world.CollisionOffset(state, x, z)
		for _, s := range boxes {
			oLo := [3]float64{float64(x) + ox + s.MinX, float64(y) + s.MinY, float64(z) + oz + s.MinZ}
			oHi := [3]float64{float64(x) + ox + s.MaxX, float64(y) + s.MaxY, float64(z) + oz + s.MaxZ}
			stop(oLo, oHi)
		}
	}
	if !c.noBorder {
		walls := c.walls
		if !c.borders {
			walls = borderWalls(w, b, 0, 0)
		}
		for _, wall := range walls {
			stop([3]float64{wall.minX, wall.minY, wall.minZ}, [3]float64{wall.maxX, wall.maxY, wall.maxZ})
		}
	}
	if bodies, ok := w.(BodyWorld); ok {
		swept := AABB{sweptLo[0], sweptLo[1], sweptLo[2], sweptHi[0], sweptHi[1], sweptHi[2]}
		for _, hull := range bodies.BodyBoxes(swept) {
			stop([3]float64{hull.minX, hull.minY, hull.minZ}, [3]float64{hull.maxX, hull.maxY, hull.maxZ})
		}
	}
	lo[axis] += allowed
	hi[axis] += allowed
	return AABB{lo[0], lo[1], lo[2], hi[0], hi[1], hi[2]}, obstacleTop, allowed != d
}

// borderWalls is the outside of the world border as four slabs, when the box
// moving by (dx, dz) is close to the border: the distance from its centre to
// the nearest edge is under twice the larger horizontal size of the box
// expanded by the move (at least 1), and the centre is within the border
// grown by that size. Whether it is close is read from the raw edges; the
// slabs are cut at the edges rounded outwards to whole blocks.
// vanilla: WorldBorder.isInsideCloseToBorder, StaticBorderExtent.updateBox
func borderWalls(w World, b AABB, dx, dz float64) []AABB {
	minX, minZ, maxX, maxZ, ok := w.BorderEdges()
	if !ok {
		return nil
	}
	size := math.Max(math.Max(b.maxX-b.minX+math.Abs(dx), b.maxZ-b.minZ+math.Abs(dz)), 1)
	x, z := (b.minX+b.maxX)/2, (b.minZ+b.maxZ)/2
	dist := math.Min(math.Min(x-minX, maxX-x), math.Min(z-minZ, maxZ-z))
	if dist >= size*2 || x < minX-size || x >= maxX+size || z < minZ-size || z >= maxZ+size {
		return nil
	}
	minX, minZ, maxX, maxZ = math.Floor(minX), math.Floor(minZ), math.Ceil(maxX), math.Ceil(maxZ)
	const inf = 1e9
	return []AABB{
		{-inf, -inf, -inf, minX, inf, inf},
		{maxX, -inf, -inf, inf, inf, inf},
		{minX, -inf, -inf, maxX, inf, minZ},
		{minX, -inf, maxZ, maxX, inf, inf},
	}
}

// collideHorizontal moves the box horizontally, larger axis first as
// vanilla's collide does. It reports which axes were stopped and the higher
// obstacle top, for the step-up.
func collideHorizontal(w World, c collideCtx, b AABB, dx, dz float64) (AABB, float64, bool, bool) {
	if !c.noBorder && !c.borders {
		c.walls, c.borders = borderWalls(w, b, dx, dz), true
	}
	if math.Abs(dx) >= math.Abs(dz) {
		b, ox, bx := clipMove(w, c, b, axisX, dx)
		b, oz, bz := clipMove(w, c, b, axisZ, dz)
		return b, math.Max(ox, oz), bx, bz
	}
	b, oz, bz := clipMove(w, c, b, axisZ, dz)
	b, ox, bx := clipMove(w, c, b, axisX, dx)
	return b, math.Max(ox, oz), bx, bz
}

// collisionIsFull reports whether a block's collision is a full cube, which
// the escape walk checks each side against.
func collisionIsFull(state uint16) bool {
	return world.CollisionFull(state)
}

// moveTowardsClosestSpace is Entity.moveTowardsClosestSpace for a trapped
// item or orb: of north, south, west, east and up (never down), the nearest
// side whose neighbour is not a full cube is chosen; the other two axes keep
// three quarters of the velocity and the chosen one gets a random 0.1-0.3
// push. If every side is full it pushes up, so a buried body rises out of
// the ground.
func moveTowardsClosestSpace(w World, x, y, z, vx, vy, vz float64, rng *rand.Rand) (float64, float64, float64) {
	bx, by, bz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	fx, fy, fz := x-float64(bx), y-float64(by), z-float64(bz)

	// Default to up.
	axis, sign := 1, 1.0
	best := math.MaxFloat64
	ask := func(a int, s, frac float64, cx, cy, cz int32) {
		if collisionIsFull(w.BlockAt(cx, cy, cz)) {
			return // a full block's shape; no air beyond this side
		}
		dist := frac
		if s > 0 {
			dist = 1 - frac
		}
		if dist < best {
			best, axis, sign = dist, a, s
		}
	}
	ask(2, -1, fz, bx, by, bz-1) // north
	ask(2, 1, fz, bx, by, bz+1)  // south
	ask(0, -1, fx, bx-1, by, bz) // west
	ask(0, 1, fx, bx+1, by, bz)  // east
	ask(1, 1, fy, bx, by+1, bz)  // up

	speed := float64(rng.Float32()*0.2 + 0.1)
	vx, vy, vz = vx*0.75, vy*0.75, vz*0.75
	switch axis {
	case 0:
		vx = sign * speed
	case 1:
		vy = sign * speed
	case 2:
		vz = sign * speed
	}
	return vx, vy, vz
}

// overlapsSolid is CollisionGetter.noCollision (inverted): whether any block
// shape or collidable body strictly crosses the box. A face exactly touching
// shares nothing. Cells are read from one below the box, since a shape can
// rise past its cell.
// vanilla: CollisionGetter.noCollision
func overlapsSolid(w World, c collideCtx, b AABB) bool {
	x0, x1 := int32(math.Floor(b.minX)), int32(math.Floor(b.maxX))
	y0, y1 := int32(math.Floor(b.minY))-1, int32(math.Floor(b.maxY))
	z0, z1 := int32(math.Floor(b.minZ)), int32(math.Floor(b.maxZ))
	scratch := cellScratch.Get().(*[]uint16)
	defer cellScratch.Put(scratch)
	cells := w.BlocksIn(x0, y0, z0, x1, y1, z1, *scratch)
	*scratch = cells
	i := 0
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z, i = z+1, i+1 {
				state := cells[i]
				boxes := cellBoxes(w, c, state, x, y, z, b.minY)
				if len(boxes) == 0 {
					continue
				}
				ox, oz := world.CollisionOffset(state, x, z)
				for _, s := range boxes {
					if b.minX < float64(x)+ox+s.MaxX && b.maxX > float64(x)+ox+s.MinX &&
						b.minY < float64(y)+s.MaxY && b.maxY > float64(y)+s.MinY &&
						b.minZ < float64(z)+oz+s.MaxZ && b.maxZ > float64(z)+oz+s.MinZ {
						return true
					}
				}
			}
		}
	}
	if bodies, ok := w.(BodyWorld); ok && len(bodies.BodyBoxes(b)) > 0 {
		return true
	}
	return false
}

// BoxBlocked reports whether the body's current box crosses a block shape,
// for a body whose size just changed. The box is shaved by the 1e-7 the
// engine treats as flush, since the fudge's feet = centre - height/2 can
// leave a body a hair below the surface it stands on.
func (m *Mob) BoxBlocked(w World) bool {
	const eps = 1e-7
	return overlapsSolid(w, m.collideCtx(), m.mobBox().inflate(-eps, -eps, -eps))
}

// inflate grows the box by the amount on every side, AABB.inflate.
func (b AABB) inflate(dx, dy, dz float64) AABB {
	return AABB{b.minX - dx, b.minY - dy, b.minZ - dz, b.maxX + dx, b.maxY + dy, b.maxZ + dz}
}

// boxOfSize is AABB.ofSize: the box of the size given centred on a point.
func boxOfSize(center [3]float64, sizeX, sizeY, sizeZ float64) AABB {
	hx, hy, hz := sizeX/2, sizeY/2, sizeZ/2
	return AABB{center[0] - hx, center[1] - hy, center[2] - hz, center[0] + hx, center[1] + hy, center[2] + hz}
}

// blockCollisions is the block shapes whose insides meet the box, in world
// coordinates; cells are read one past the box on every axis since a shape
// can rise past its cell. Collidable bodies are not included
// (getBlockCollisions is blocks only).
// vanilla: BlockCollisions, CollisionGetter.getBlockCollisions
func blockCollisions(w World, c collideCtx, box AABB) []AABB {
	x0 := int32(math.Floor(box.minX-1e-7)) - 1
	x1 := int32(math.Floor(box.maxX+1e-7)) + 1
	y0 := int32(math.Floor(box.minY-1e-7)) - 1
	y1 := int32(math.Floor(box.maxY+1e-7)) + 1
	z0 := int32(math.Floor(box.minZ-1e-7)) - 1
	z1 := int32(math.Floor(box.maxZ+1e-7)) + 1
	scratch := cellScratch.Get().(*[]uint16)
	defer cellScratch.Put(scratch)
	cells := w.BlocksIn(x0, y0, z0, x1, y1, z1, *scratch)
	*scratch = cells
	var out []AABB
	i := 0
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z, i = z+1, i+1 {
				state := cells[i]
				boxes := cellBoxes(w, c, state, x, y, z, box.minY)
				if len(boxes) == 0 {
					continue
				}
				ox, oz := world.CollisionOffset(state, x, z)
				cross := false
				for _, s := range boxes {
					if box.minX < float64(x)+ox+s.MaxX && box.maxX > float64(x)+ox+s.MinX &&
						box.minY < float64(y)+s.MaxY && box.maxY > float64(y)+s.MinY &&
						box.minZ < float64(z)+oz+s.MaxZ && box.maxZ > float64(z)+oz+s.MinZ {
						cross = true
						break
					}
				}
				if !cross {
					continue
				}
				for _, s := range boxes {
					out = append(out, AABB{
						minX: float64(x) + ox + s.MinX, minY: float64(y) + s.MinY, minZ: float64(z) + oz + s.MinZ,
						maxX: float64(x) + ox + s.MaxX, maxY: float64(y) + s.MaxY, maxZ: float64(z) + oz + s.MaxZ,
					})
				}
			}
		}
	}
	return out
}

// findFreePosition is CollisionGetter.findFreePosition: the point in allowed
// nearest preferred whose box of size clears every block. Shapes are
// inflated by half the box, so a centre clears when its box does; the
// region is cut into cells by them and the nearest uncovered cell answers
// (closestPointTo). free is false when nothing clears.
// vanilla: CollisionGetter.findFreePosition, VoxelShape.closestPointTo
func findFreePosition(w World, allowed AABB, preferred [3]float64, sizeX, sizeY, sizeZ float64) (center [3]float64, free bool) {
	blocks := blockCollisions(w, noCtx, allowed.inflate(sizeX, sizeY, sizeZ))
	xs := []float64{allowed.minX, allowed.maxX}
	ys := []float64{allowed.minY, allowed.maxY}
	zs := []float64{allowed.minZ, allowed.maxZ}
	inflated := make([]AABB, len(blocks))
	for i, b := range blocks {
		inflated[i] = b.inflate(sizeX/2, sizeY/2, sizeZ/2)
		// Clamp to the region so the cell grid stays inside it.
		xs = append(xs, clamp(inflated[i].minX, allowed.minX, allowed.maxX), clamp(inflated[i].maxX, allowed.minX, allowed.maxX))
		ys = append(ys, clamp(inflated[i].minY, allowed.minY, allowed.maxY), clamp(inflated[i].maxY, allowed.minY, allowed.maxY))
		zs = append(zs, clamp(inflated[i].minZ, allowed.minZ, allowed.maxZ), clamp(inflated[i].maxZ, allowed.minZ, allowed.maxZ))
	}
	sort.Float64s(xs)
	xs = dedupeCoords(xs)
	sort.Float64s(ys)
	ys = dedupeCoords(ys)
	sort.Float64s(zs)
	zs = dedupeCoords(zs)
	best := math.Inf(1)
	for i := 0; i+1 < len(xs); i++ {
		for j := 0; j+1 < len(ys); j++ {
			for k := 0; k+1 < len(zs); k++ {
				cell := AABB{xs[i], ys[j], zs[k], xs[i+1], ys[j+1], zs[k+1]}
				blocked := slices.ContainsFunc(inflated, cell.Crosses)
				if blocked {
					continue
				}
				cx := clamp(preferred[0], cell.minX, cell.maxX)
				cy := clamp(preferred[1], cell.minY, cell.maxY)
				cz := clamp(preferred[2], cell.minZ, cell.maxZ)
				dx, dy, dz := cx-preferred[0], cy-preferred[1], cz-preferred[2]
				if d := dx*dx + dy*dy + dz*dz; d < best {
					best, center, free = d, [3]float64{cx, cy, cz}, true
				}
			}
		}
	}
	return center, free
}

// clamp keeps v within [lo, hi].
func clamp(v, lo, hi float64) float64 { return min(max(v, lo), hi) }

// dedupeCoords drops repeated neighbours in a sorted list (shared edges of
// touching boxes).
func dedupeCoords(v []float64) []float64 {
	out := v[:1]
	for _, c := range v[1:] {
		if c != out[len(out)-1] {
			out = append(out, c)
		}
	}
	return out
}

// moveBox moves a box-shaped body along its velocity as far as block shapes
// allow, vertical first, zeroing stopped axes. It reports whether the body
// ended on the ground.
func moveBox(w World, c collideCtx, x, y, z, vx, vy, vz *float64, stuck *stuckFactor, halfWidth, height float64) bool {
	dx, dy, dz, slowed := stuck.scale(*vx, *vy, *vz)
	if slowed {
		*vx, *vy, *vz = 0, 0, 0
	}
	cells := gatherMoveCells(w, AABB{*x - halfWidth, *y, *z - halfWidth, *x + halfWidth, *y + height, *z + halfWidth}, dx, dy, dz, 0)
	defer cells.release()
	c.moveCells = &cells
	onGround := false
	if dy < 0 {
		landY, blocked := clipFall(w, c, *x, *z, halfWidth, *y, dy)
		*y = landY
		if blocked {
			*vy = 0
			onGround = true
		}
	} else if dy > 0 {
		topY, blocked := clipRise(w, c, *x, *z, halfWidth, *y+height, dy)
		*y = topY - height
		if blocked {
			*vy = 0
		}
	}
	if dx != 0 || dz != 0 {
		box, _, blockedX, blockedZ := collideHorizontal(w, c, AABB{
			*x - halfWidth, *y, *z - halfWidth,
			*x + halfWidth, *y + height, *z + halfWidth,
		}, dx, dz)
		*x, *z = box.centerXZ()
		if blockedX {
			*vx = 0
		}
		if blockedZ {
			*vz = 0
		}
	}
	f := blockSpeedFactor(w, *x, *y, *z)
	*vx *= f
	*vz *= f
	*stuck = stuckIn(w, AABB{*x - halfWidth, *y, *z - halfWidth,
		*x + halfWidth, *y + height, *z + halfWidth}, *x, *y, *z, false, false, false, false)
	return onGround
}

// ClipDisplacement resolves an external shove against collision shapes without
// changing the body's velocity. The box is in world coordinates.
// vanilla: Entity.collide
func ClipDisplacement(w World, bounds [6]float64, dx, dy, dz float64) (float64, float64, float64) {
	b := AABB{bounds[0], bounds[1], bounds[2], bounds[3], bounds[4], bounds[5]}
	ctx := collideCtx{noBorder: true}
	b, _, _ = clipMove(w, ctx, b, axisY, dy)
	b, _, _, _ = collideHorizontal(w, ctx, b, dx, dz)
	return b.minX - bounds[0], b.minY - bounds[1], b.minZ - bounds[2]
}
