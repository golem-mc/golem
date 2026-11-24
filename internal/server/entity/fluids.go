package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Entity fluids, as vanilla runs them: heights, the eye test and currents are
// EntityFluidInteraction's; water and lava movement are ItemEntity's and
// ExperienceOrb's setFluidMovement; burns are LavaFluid.entityInside and
// BaseFireBlock.entityInside. A fluid state's level property is the same
// number the world's fluid engine writes, so heights and flow come from the
// cells the engine fills. Vanilla keeps these numbers as Java floats widened
// at each use, so the constants are written as float32 round-trips.
const (
	// floatHeight is ItemEntity.FLOAT_HEIGHT's 0.1f: the fluid depth over the
	// feet at which ItemEntity.tick swaps gravity for drift.
	floatHeight = float64(float32(0.1))

	// fluidDrag and lavaDrag are setUnderwaterMovement's and
	// setUnderLavaMovement's horizontal multipliers.
	fluidDrag = float64(float32(0.99))
	lavaDrag  = float64(float32(0.95))

	// fluidRise is setFluidMovement's upward nudge per submerged tick, and
	// fluidRiseCap the 0.06f it stops at, lifting a sunken item back up. The
	// orb shares both, the item applying them under the cap test and the orb
	// under a plain minimum.
	fluidRise    = float64(float32(5e-4))
	fluidRiseCap = float64(float32(0.06))

	// airDragFloat is Entity.getAirDrag's 0.98f, applied by items and orbs after
	// each move.
	airDragFloat = float64(float32(0.98))

	// restThreshold is the squared horizontal speed under which a grounded item
	// moves only every fourth tick (ItemEntity.tick), so a settled stack does
	// not grind against its block.
	restThreshold = float64(float32(1e-5))

	// itemEyeHeight and orbEyeHeight are the eye-in-fluid test heights:
	// minecraft:item's 0.2125f, and the orb's EntityDimensions.defaultEyeHeight
	// of 0.85 of its half-block box.
	itemEyeHeight = float64(float32(0.2125))
	orbEyeHeight  = float64(float32(0.5) * float32(0.85))

	// boxShave is EntityFluidInteraction's box deflation before reading fluids,
	// insideShave the inside-block check's 1.0E-5f: a box that only touches a
	// fluid cell is not in it.
	boxShave    = 0.001
	insideShave = float64(float32(1e-5))

	// waterCurrent is applyCurrentTo's water strength, whatever the flow's
	// speed. Lava's is weaker (0.007 with the nether's fast_lava); both are
	// topped up to 0.0045 for a body barely moving sideways, so a current can
	// always start it drifting.
	waterCurrent         = 0.014
	lavaCurrent          = 0.0023333333333333335
	fastLavaCurrent      = 0.007
	currentStartSpeed    = 0.003
	currentStartPush     = 0.0045000000000000005
	currentMinimumLength = float64(float32(1e-5))

	// sourceHeight is a full cell's fluid height (sources and falling
	// columns), 8/9 in FlowingFluid.getOwnHeight's float division.
	sourceHeight = float64(float32(8) / float32(9))

	// fallFlow is getFlow's -6.0 downward share for a falling column, added
	// before normalising.
	fallFlow = 6.0

	// fireTicksLava and fireTicksFire are the burns the two sources light:
	// fifteen seconds of it for the lava, eight for a block of fire.
	fireTicksLava = 300
	fireTicksFire = 160

	// lavaDamage, onFireDamage, fireBlockDamage and soulFireDamage are the burn
	// costs: Entity.lavaHurt's 4, the burning's every-20th-tick bite, and
	// BaseFireBlock's 1 for fire and 2 for soul fire.
	lavaDamage      = float32(4.0)
	onFireDamage    = float32(1.0)
	fireBlockDamage = float32(1.0)
	soulFireDamage  = float32(2.0)
	// cactusDamage is CactusBlock.entityInside's hurt.
	cactusDamage = float32(1.0)

	// pickupDelayInfinite is the pickup delay a never-to-be-taken item is
	// given, which vanilla's tick counts down to never.
	pickupDelayInfinite = 32767
)

// fluidKind is which of the world's two fluids a cell holds, or neither.
type fluidKind = blocklogic.FluidKind

const (
	fluidNone  = blocklogic.FluidNone
	fluidWater = blocklogic.FluidWater
	fluidLava  = blocklogic.FluidLava
)

// fluidCell is a cell as FluidState sees it: which fluid, and the level
// property (0 source, 1-7 flowing depths, 8+ falling).
type fluidCell struct {
	kind  fluidKind
	level int
}

// fluidCellOf reads a state's fluid. Waterlogged blocks, kelp and seagrass
// hold a water source (their getFluidState); everything else holds nothing.
func fluidCellOf(state uint16) fluidCell {
	f := blocklogic.FluidOf(state)
	return fluidCell{kind: f.Kind, level: int(f.Level)}
}

// fluidOwnHeight is FlowingFluid.getOwnHeight: the fluid's amount over nine,
// in the float arithmetic the bytecode divides it with.
func fluidOwnHeight(cell fluidCell) float64 {
	return float64(float32(blocklogic.FluidAmount(cell.level)) / 9)
}

// fluidHeightAt is FluidState.getHeight at a cell: one with the same fluid
// standing above it is brim-full, and one without is only its own depth.
func fluidHeightAt(w World, x, y, z int32, cell fluidCell) float64 {
	above := fluidCellOf(w.BlockAt(x, y+1, z))
	if above.kind == cell.kind {
		return 1
	}
	return fluidOwnHeight(cell)
}

// blocksMotion is BlockState.blocksMotion via this server's collision
// shapes: any shape at all blocks; fluids and vegetation do not.
func blocksMotion(state uint16) bool {
	return blockExtent(state) > 0
}

// isSolidFace is FlowingFluid.isSolidFace for a horizontal step dir (a
// Direction ordinal): never for the same fluid flowing past, never for ice
// or frosted ice (vanilla's IceBlock), else the state's face toward dir is
// sturdy.
func isSolidFace(state uint16, kind fluidKind, dir int) bool {
	if neighbour := fluidCellOf(state); neighbour.kind == kind {
		return false
	}
	if b := registry.BlockOf(state); b == registry.BlockIce || b == registry.BlockFrostedIce {
		return false
	}
	return registry.FaceSturdy(state, dir, registry.SupportFull)
}

// fluidFlowAt is FlowingFluid.getFlow: the flow direction from surrounding
// heights. A lower or drier neighbour pulls in proportion to the difference;
// a dry neighbour over the same fluid pulls harder (the drop is worth more).
// A falling column beside a wall adds a strong downward pull.
// The sum is normalised into the current's direction.
func fluidFlowAt(w World, x, y, z int32, cell fluidCell) (fx, fy, fz float64) {
	own := float32(fluidOwnHeight(cell))
	for _, d := range [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		nx, nz := x+d[0], z+d[1]
		neighbour := fluidCellOf(w.BlockAt(nx, y, nz))
		if neighbour.kind != fluidNone && neighbour.kind != cell.kind {
			continue // a different fluid is none of this flow's business
		}
		neighbourHeight := float32(0)
		if neighbour.kind != fluidNone {
			neighbourHeight = float32(fluidOwnHeight(neighbour))
		}
		distance := float32(0)
		if neighbourHeight == 0 {
			// The pour: a dry neighbour is worth a look downward, where
			// this cell's fluid may be running off the edge.
			if !registry.BlockOf(w.BlockAt(nx, y, nz)).InTag(registry.TagIDBlocksFluidFlow) {
				below := fluidCellOf(w.BlockAt(nx, y-1, nz))
				if below.kind == cell.kind {
					if belowHeight := float32(fluidOwnHeight(below)); belowHeight > 0 {
						distance = own - (belowHeight - float32(sourceHeight))
					}
				}
			}
		} else {
			distance = own - neighbourHeight
		}
		if distance == 0 {
			continue
		}
		fx += float64(float32(d[0]) * distance)
		fz += float64(float32(d[1]) * distance)
	}
	if cell.level >= 8 {
		// The falling column: a solid face beside it, at its level or the
		// one above, gives the flow its downward share.
		// The steps east, west, south, north, with their Direction ordinals.
		for _, d := range [4][3]int32{{1, 0, 5}, {-1, 0, 4}, {0, 1, 3}, {0, -1, 2}} {
			if isSolidFace(w.BlockAt(x+d[0], y, z+d[1]), cell.kind, int(d[2])) ||
				isSolidFace(w.BlockAt(x+d[0], y+1, z+d[1]), cell.kind, int(d[2])) {
				length := math.Sqrt(fx*fx + fz*fz)
				if length > 0 {
					fx, fz = fx/length, fz/length
				}
				fy = -fallFlow
				break
			}
		}
	}
	length := math.Sqrt(fx*fx + fy*fy + fz*fz)
	if length > 0 {
		return fx / length, fy / length, fz / length
	}
	return 0, 0, 0
}

// fluidTracker is EntityFluidInteraction.Tracker for one fluid: depth above
// the feet, whether the eyes are in it (and whether the eye cell is a bubble
// column, which breathing ignores), and the accumulated current.
type fluidTracker struct {
	height                       float64
	eyesIn                       bool
	bubbleAtEyes                 bool
	currentX, currentY, currentZ float64
	currentCount                 int
}

// reset is Tracker.reset, the wipe each fluid interaction pass opens with.
func (t *fluidTracker) reset() {
	t.height = 0
	t.eyesIn = false
	t.bubbleAtEyes = false
	t.currentX, t.currentY, t.currentZ = 0, 0, 0
	t.currentCount = 0
}

// accumulateCurrent is Tracker.accumulateCurrent.
func (t *fluidTracker) accumulateCurrent(x, y, z float64) {
	t.currentX += x
	t.currentY += y
	t.currentZ += z
	t.currentCount++
}

// currentImpulse is Tracker.applyCurrentTo's push: the accumulated flow
// normalised and scaled to the fluid's strength, topped up to the starting
// push for a body barely drifting sideways. false when there is no push.
func (t *fluidTracker) currentImpulse(scale, velocityX, velocityZ float64) (dx, dy, dz float64, any bool) {
	if t.currentCount == 0 {
		return 0, 0, 0, false
	}
	lengthSq := t.currentX*t.currentX + t.currentY*t.currentY + t.currentZ*t.currentZ
	if lengthSq < currentMinimumLength {
		return 0, 0, 0, false
	}
	length := math.Sqrt(lengthSq)
	nx, ny, nz := t.currentX/length, t.currentY/length, t.currentZ/length
	dx, dy, dz = nx*scale, ny*scale, nz*scale
	if math.Abs(velocityX) < currentStartSpeed && math.Abs(velocityZ) < currentStartSpeed {
		if l := math.Sqrt(dx*dx + dy*dy + dz*dz); l < currentStartPush {
			dx, dy, dz = nx*currentStartPush, ny*currentStartPush, nz*currentStartPush
		}
	}
	return dx, dy, dz, true
}

// updateFluid is EntityFluidInteraction.update for one body: walk the cells
// the box (deflated by 0.001) spans; each fluid cell adds its depth above
// the feet (measured from the unshaved box) to its tracker, marks the eyes
// if the eye's column is in it, and adds its flow when currents are wanted.
// A cell whose fluid does not reach the feet is ignored.
func updateFluid(w World, box AABB, eyeX, eyeY, eyeZ float64, withCurrent bool, water, lava *fluidTracker) {
	water.reset()
	lava.reset()
	cellBox := box
	cellBox.deflate(boxShave)
	x0, x1 := int32(math.Floor(cellBox.minX)), int32(math.Ceil(cellBox.maxX))-1
	y0, y1 := int32(math.Floor(cellBox.minY)), int32(math.Ceil(cellBox.maxY))-1
	z0, z1 := int32(math.Floor(cellBox.minZ)), int32(math.Ceil(cellBox.maxZ))-1
	eyeColumnX, eyeColumnZ := int32(math.Floor(eyeX)), int32(math.Floor(eyeZ))
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				state := w.BlockAt(x, y, z)
				cell := fluidCellOf(state)
				if cell.kind == fluidNone {
					continue
				}
				top := float64(y) + fluidHeightAt(w, x, y, z, cell)
				if top < box.minY {
					continue
				}
				var t *fluidTracker
				if cell.kind == fluidLava {
					t = lava
				} else {
					t = water
				}
				if x == eyeColumnX && z == eyeColumnZ && eyeY >= float64(y) && eyeY <= top {
					t.eyesIn = true
					if rules.IsBubbleColumn(state) {
						t.bubbleAtEyes = true
					}
				}
				if h := top - box.minY; h > t.height {
					t.height = h
				}
				if !withCurrent {
					continue
				}
				fx, fy, fz := fluidFlowAt(w, x, y, z, cell)
				if t.height < 0.4 {
					fx, fy, fz = fx*t.height, fy*t.height, fz*t.height
				}
				t.accumulateCurrent(fx, fy, fz)
			}
		}
	}
}

// AABB is the span of a body's box, the two corners the fluid walk and the
// inside-block check are measured between.
type AABB struct {
	minX, minY, minZ float64
	maxX, maxY, maxZ float64
}

// NewAABB is a box from its corners, the shape a server-side hull snapshot
// builds its boxes from.
func NewAABB(minX, minY, minZ, maxX, maxY, maxZ float64) AABB {
	return AABB{minX, minY, minZ, maxX, maxY, maxZ}
}

// Corners are the box's two corners.
func (b AABB) Corners() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ
}

// Crosses reports whether the boxes share an inside (AABB.intersects'
// strict test; touching faces do not count).
func (b AABB) Crosses(o AABB) bool {
	return b.minX < o.maxX && b.maxX > o.minX &&
		b.minY < o.maxY && b.maxY > o.minY &&
		b.minZ < o.maxZ && b.maxZ > o.minZ
}

// Inflate is the box grown the same distance every face out, the hull a
// search around a body asks for. vanilla: AABB.inflate
func (b AABB) Inflate(d float64) AABB {
	return AABB{b.minX - d, b.minY - d, b.minZ - d, b.maxX + d, b.maxY + d, b.maxZ + d}
}

// PlayerTouchBox is the reach for entity touches. A zero vehicle box means the player is on foot.
// vanilla: Player.aiStep
func PlayerTouchBox(x, y, z, width, height float64, vehicle AABB) AABB {
	hw := width / 2
	if vehicle == (AABB{}) {
		return NewAABB(x-hw-1, y-0.5, z-hw-1, x+hw+1, y+height+0.5, z+hw+1)
	}
	return NewAABB(min(x-hw, vehicle.minX)-1, min(y, vehicle.minY), min(z-hw, vehicle.minZ)-1,
		max(x+hw, vehicle.maxX)+1, max(y+height, vehicle.maxY), max(z+hw, vehicle.maxZ)+1)
}

// centerXZ is the centre of the box's horizontal span, where a clipped
// horizontal move leaves a body's bottom-centre position.
func (b AABB) centerXZ() (x, z float64) {
	return (b.minX + b.maxX) / 2, (b.minZ + b.maxZ) / 2
}

// moved is the box translated by the vector given, AABB.move.
func (b AABB) moved(dx, dy, dz float64) AABB {
	return AABB{b.minX + dx, b.minY + dy, b.minZ + dz, b.maxX + dx, b.maxY + dy, b.maxZ + dz}
}

// itemBox is an item's quarter-block box about its centre; vanilla's fluid
// reads measure from its bottom.
func (it *Item) itemBox() AABB {
	return AABB{
		minX: it.X - halfSize, minY: it.Y - halfSize, minZ: it.Z - halfSize,
		maxX: it.X + halfSize, maxY: it.Y + halfSize, maxZ: it.Z + halfSize,
	}
}

// orbBox is an orb's half-block box about its feet, the bottom-centre
// position every vanilla entity keeps.
func (o *Orb) orbBox() AABB {
	return AABB{
		minX: o.X - orbHalfSize, minY: o.Y, minZ: o.Z - orbHalfSize,
		maxX: o.X + orbHalfSize, maxY: o.Y + orbHeight, maxZ: o.Z + orbHalfSize,
	}
}

// updateFluids is one EntityFluidInteraction.update for the item: trackers
// refilled, plus a flowing fluid's push (water's strength, lava's per
// dimension). It returns isInWater and isInLava with the heights the
// movement branch compares against the float threshold.
func (it *Item) updateFluids(w World, withCurrent bool) (inWater, inLava bool, waterHeight, lavaHeight float64) {
	updateFluid(w, it.itemBox(), it.X, it.Y-halfSize+itemEyeHeight, it.Z, withCurrent, &it.water, &it.lava)
	inWater = it.water.height > 0
	inLava = it.lava.height > 0
	if !withCurrent {
		return inWater, inLava, it.water.height, it.lava.height
	}
	if inWater {
		if dx, dy, dz, any := it.water.currentImpulse(waterCurrent, it.VX, it.VZ); any {
			it.VX += dx
			it.VY += dy
			it.VZ += dz
		}
	}
	if inLava {
		scale := lavaCurrent
		if w.FastLava() {
			scale = fastLavaCurrent
		}
		if dx, dy, dz, any := it.lava.currentImpulse(scale, it.VX, it.VZ); any {
			it.VX += dx
			it.VY += dy
			it.VZ += dz
		}
	}
	return inWater, inLava, it.water.height, it.lava.height
}

// BoxInLava is Entity.isInLava via the fluid box walk: touching a face, or
// feet above the fluid's height, is not in it. width is the horizontal span
// and height the box's height above the feet at (x, y, z).
func BoxInLava(w World, x, y, z, width, height float64) bool {
	hw := width / 2
	var water, lava fluidTracker
	updateFluid(w, AABB{x - hw, y, z - hw, x + hw, y + height, z + hw}, x, y, z, false, &water, &lava)
	return lava.height > 0
}

// BoxInWater is Entity.isInWater and isEyeInFluid via the fluid box walk,
// plus the breath's bubble-column test at eyeY: touching a face, or feet
// above the fluid's height, is not in it. width and height are as for
// BoxInLava.
// vanilla: EntityFluidInteraction.update, Entity.isEyeInFluid,
// LivingEntity.baseTick
func BoxInWater(w World, x, y, z, width, height, eyeY float64) (inWater, eyesIn, bubbleAtEyes bool) {
	hw := width / 2
	var water, lava fluidTracker
	updateFluid(w, AABB{x - hw, y, z - hw, x + hw, y + height, z + hw}, x, eyeY, z, false, &water, &lava)
	return water.height > 0, water.eyesIn, water.bubbleAtEyes
}

// deflate shrinks a box by the shave given, the way AABB.deflate does.
func (b *AABB) deflate(amount float64) {
	b.minX += amount
	b.minY += amount
	b.minZ += amount
	b.maxX -= amount
	b.maxY -= amount
	b.maxZ -= amount
}

// insideCells yields the cells the box overlaps, shaved by insideShave so a
// box resting on a face stays out of the next cell.
func (b AABB) insideCells(yield func(x, y, z int32)) {
	x0, x1 := int32(math.Floor(b.minX+insideShave)), int32(math.Floor(b.maxX-insideShave))
	y0, y1 := int32(math.Floor(b.minY+insideShave)), int32(math.Floor(b.maxY-insideShave))
	z0, z1 := int32(math.Floor(b.minZ+insideShave)), int32(math.Floor(b.maxZ-insideShave))
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				yield(x, y, z)
			}
		}
	}
}
