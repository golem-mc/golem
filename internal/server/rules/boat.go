package rules

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// The boat a boat item lays: the spot vanilla's BoatItem.use stands it at,
// and the seat and the dismount the riding runs on.

// BoatHalfWidth and BoatHeight are the boat box's halves, the
// EntityTypes.BOAT builder's sized(1.375F, 0.5625F): 1.375 of a block about
// the position, 0.5625 of a block tall above the feet.
const (
	BoatHalfWidth = 0.6875
	BoatHeight    = 0.5625
)

// BoatSeatY is the boat's passenger attachment height (a third of its height).
// vanilla: AbstractBoat.getPassengerAttachmentPoint
const BoatSeatY = 0.1875

// RaftSeatY is a raft's seat height, 0.8888889 of its height, higher than a
// boat's.
// vanilla: Raft.rideHeight, ChestRaft.rideHeight
const RaftSeatY = 0.5

// BoatSeatOffset is a passenger attachment from a hull at its feet facing yaw:
// up by the seat height, along the hull (bow at yaw 0) by the seat's place,
// rotated by yaw. A lone passenger sits centred (0.15 forward in a chest
// boat); with two, +0.2 and -0.6, an Animal's 0.2 further.
// vanilla: AbstractBoat.getPassengerAttachmentPoint, getSinglePassengerXOffset,
// Raft.rideHeight
func BoatSeatOffset(raft, chest bool, count, index int, animal bool, yaw float32) (dx, dy, dz float64) {
	along := 0.0
	if chest {
		along = 0.15
	}
	if count > 1 {
		if index == 0 {
			along = 0.2
		} else {
			along = -0.6
		}
		if animal {
			along += 0.2
		}
	}
	seatY := BoatSeatY
	if raft {
		seatY = RaftSeatY
	}
	rad := float64(yaw) * math.Pi / 180
	return -along * math.Sin(rad), seatY, along * math.Cos(rad)
}

// BoatWorld is what a boat item's use reads: the look's first block or fluid
// in reach, the blocks a fluid surface is read from, and the collision check.
type BoatWorld interface {
	ClipAnyFluid(ox, oy, oz, dx, dy, dz, reach float64) (x, y, z int32, liquid, hit bool)
	BlockAt(x, y, z int32) uint16
	BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool
}

// BoatSpot is where a boat item places its boat: the look ray's first stop
// in reach (a block as a whole cube, or any fluid level, Fluid.ANY), at the
// fluid's surface or the crossed face. Collision is checked separately after
// the stack's entity data has loaded, since that can replace the position.
// vanilla: BoatItem.use
func BoatSpot(w BoatWorld, ex, ey, ez, dx, dy, dz, reach float64) (x, y, z float64, ok bool) {
	cx, cy, cz, liquid, hit := w.ClipAnyFluid(ex, ey, ez, dx, dy, dz, reach)
	if !hit {
		return 0, 0, 0, false
	}
	// The top of what was hit: the block's whole cube, or the fluid's own
	// depth of the cell — the box the clip's own shape check measured the
	// hit by.
	top := float64(cy) + 1
	if liquid {
		top = float64(cy) + FluidSurfaceHeight(w.BlockAt(cx, cy, cz), w.BlockAt(cx, cy+1, cz))
	}
	x, y, z, crossed := rayIntoBox(ex, ey, ez, dx, dy, dz, float64(cx), float64(cy), float64(cz), top)
	return x, y, z, crossed
}

// BoatFits reports whether a boat's box at the position is clear of blocks.
// vanilla: BoatItem.use's noCollision
func BoatFits(w BoatWorld, x, y, z float64) bool {
	return !w.BoxCollides(x-BoatHalfWidth, y, z-BoatHalfWidth,
		x+BoatHalfWidth, y+BoatHeight, z+BoatHalfWidth)
}

// FluidSurfaceHeight is the fluid's height in its cell under above: 8/9 for
// a source or falling column, 8/9 less the level for a flow, and a whole
// cell under the same fluid (waterlogged included). Non-fluids read 8/9;
// callers ask only of fluid cells.
// vanilla: FlowingFluid.getOwnHeight, FluidState.getHeight
func FluidSurfaceHeight(state, above uint16) float64 {
	block := registry.BlockOf(state)
	if registry.BlockOf(above) == block ||
		block == registry.BlockWater && registry.StateBool(above, registry.PropWaterlogged) {
		return 1
	}
	// A state without a level reads as -1, a source's eight ninths.
	level, _ := registry.StateInt(state, registry.PropLevel)
	if level <= 0 || level >= 8 {
		return 8.0 / 9
	}
	return (8.0 - float64(level)) / 9
}

// BoatDismountSpot is where a body steps off a boat at (x, y, z): beside the
// hull along yaw by half the escape gap of the two widths, on the floor
// there, else back on top. poses are tried in getDismountPoses order; the
// first free box wins.
// vanilla: AbstractBoat.getDismountLocationForPassenger, Entity.getCollisionHorizontalEscapeVector
func BoatDismountSpot(w BoatWorld, x, y, z float64, yaw float32, poses []DismountPose) (px, py, pz float64) {
	maxY := y + BoatHeight
	// The vehicle's width widened by the diagonal, so the step clears the
	// hull's corners.
	half := (BoatHalfWidth*2*math.Sqrt2 + poses[0].Dims.Width + 1e-5) / 2
	rad := float64(yaw) * math.Pi / 180
	dx, dz := -math.Sin(rad), math.Cos(rad)
	if scale := math.Max(math.Abs(dx), math.Abs(dz)); scale != 0 {
		dx, dz = dx/scale, dz/scale
	}
	tx, tz := x+dx*half, z+dz*half
	ax, az := int32(math.Floor(tx)), int32(math.Floor(tz))
	ay := int32(math.Floor(maxY))
	// No floor beside the hull: the body comes back on top of it.
	if IsWater(w.BlockAt(ax, ay-1, az)) {
		return x, maxY, z
	}
	var cands [2][3]float64
	n := 0
	if h := DismountHeightAt(w, ax, ay, az); CanDismountInBlock(h) {
		cands[n] = [3]float64{tx, float64(ay) + h, tz}
		n++
	}
	if h := DismountHeightAt(w, ax, ay-1, az); CanDismountInBlock(h) {
		cands[n] = [3]float64{tx, float64(ay-1) + h, tz}
		n++
	}
	for _, p := range poses {
		halfW := p.Dims.Width / 2
		for j := 0; j < n; j++ {
			c := cands[j]
			if !w.BoxCollides(c[0]-halfW, c[1], c[2]-halfW, c[0]+halfW, c[1]+p.Dims.Height, c[2]+halfW) {
				return c[0], c[1], c[2]
			}
		}
	}
	return x, maxY, z
}

// DismountHeightAt is a cell's floor height: a collision's top; for an empty
// cell, the block below's top minus one (a full floor gives the cell's
// bottom); no floor when below has no shape.
// vanilla: BlockGetter.getBlockFloorHeight
func DismountHeightAt(w BoatWorld, x, y, z int32) float64 {
	if h := registry.CollisionHeight(w.BlockAt(x, y, z)); h > 0 {
		return h
	}
	if bh := registry.CollisionHeight(w.BlockAt(x, y-1, z)); bh >= 1 {
		return bh - 1
	}
	return math.Inf(-1)
}

// CanDismountInBlock is DismountHelper.isBlockFloorValid: a cell with no floor
// and a cell whose floor reaches its own ceiling both refuse the step.
// vanilla: DismountHelper.isBlockFloorValid
func CanDismountInBlock(height float64) bool {
	return !math.IsInf(height, 0) && height < 1
}

// rayIntoBox is where the ray first enters the block-wide box from loY to
// hiY (the face the DDA's first step crossed) and whether it does; a
// crossing outside the other ranges slides past. An origin inside crosses
// at itself.
func rayIntoBox(ox, oy, oz, dx, dy, dz, loX, loY, loZ, hiY float64) (px, py, pz float64, ok bool) {
	var t float64
	cross := func(o, d, lo, hi float64) {
		if d == 0 {
			return // the ray never crosses this face; it enters by the others
		}
		b := lo
		if d < 0 {
			b = hi
		}
		if ti := (b - o) / d; ti > t {
			t = ti
		}
	}
	cross(ox, dx, loX, loX+1)
	cross(oy, dy, loY, hiY)
	cross(oz, dz, loZ, loZ+1)
	if t < 0 {
		t = 0 // the ray started inside the box
	}
	px, py, pz = ox+dx*t, oy+dy*t, oz+dz*t
	// The face whose crossing came last is the one entered by; the point
	// has to sit within every other range the box bounds, or the ray only
	// slides past.
	const eps = 1e-9
	ok = px >= loX-eps && px <= loX+1+eps &&
		py >= loY-eps && py <= hiY+eps &&
		pz >= loZ-eps && pz <= loZ+1+eps
	return px, py, pz, ok
}
