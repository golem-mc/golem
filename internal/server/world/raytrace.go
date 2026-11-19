package world

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The look ray: BlockGetter.traverseBlocks' cell walk against
// ClipContext.Block.OUTLINE shapes, where a client's crosshair stops. These
// differ from the fluid engine's collision set: plants without collision are
// still targetable.

// SegmentAABB is AABB.clip: the fraction along the segment where it first
// enters the box, the face crossed, and whether it enters at all. A start
// inside reads the face the ray runs into
// (getApproximateNearest(diff).getOpposite()). A start exactly on a face reads
// as inside, so a body resting against a shape it just left is not hit by the
// leaving ray.
func SegmentAABB(x, y, z, dx, dy, dz, minX, minY, minZ, maxX, maxY, maxZ float64) (float64, int32, bool) {
	const epsilon = 1e-9
	tEnter := 0.0
	tLeave := 1.0
	crossed := -1 // the axis the entry plane stood on, none for a start inside
	axis := func(i int, p, d, lo, hi float64) bool {
		if p >= lo-epsilon && p <= hi+epsilon {
			// Within the slab already; the ray leaves it at the exit.
			if d > 0 {
				if exit := (hi - p) / d; exit < tLeave {
					tLeave = exit
				}
			} else if d < 0 {
				if exit := (lo - p) / d; exit < tLeave {
					tLeave = exit
				}
			}
			return tEnter <= tLeave
		}
		if (p < lo && d <= 0) || (p > hi && d >= 0) {
			return false // moving away from the slab, or parallel outside it
		}
		cross := lo
		if p > hi {
			cross = hi
		}
		if enter := (cross - p) / d; enter > tEnter {
			tEnter = enter
			crossed = i
		}
		// The box is left where the first of its slabs is left.
		if d > 0 {
			if exit := (hi - p) / d; exit < tLeave {
				tLeave = exit
			}
		} else {
			if exit := (lo - p) / d; exit < tLeave {
				tLeave = exit
			}
		}
		return tEnter <= tLeave
	}
	if !axis(0, x, dx, minX, maxX) || !axis(1, y, dy, minY, maxY) || !axis(2, z, dz, minZ, maxZ) {
		return 0, 0, false
	}
	switch crossed {
	case -1:
		return tEnter, backFace(dx, dy, dz), true
	case 0:
		return tEnter, entryFaceX(stepOf(dx)), true
	case 1:
		return tEnter, entryFaceY(stepOf(dy)), true
	default:
		return tEnter, entryFaceZ(stepOf(dz)), true
	}
}

// stepOf is the ray's direction along an axis, zero for none.
func stepOf(d float64) int32 {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// spanOf returns, for one axis, where along the unit segment the ray first
// reaches a cell wall and the span between walls; a still axis never crosses.
func spanOf(from float64, d float64, cell int32) (tMax, tDelta float64) {
	if d == 0 {
		return math.Inf(1), math.Inf(1)
	}
	boundary := float64(cell)
	if d > 0 {
		boundary = float64(cell + 1)
	}
	return (boundary - from) / d, math.Abs(1 / d)
}

// entryFace is the face a step along one axis enters through, in the protocol's
// face numbers a placement reads.
func entryFaceX(step int32) int32 {
	if step > 0 {
		return protocol.FaceWest
	}
	return protocol.FaceEast
}

func entryFaceY(step int32) int32 {
	if step > 0 {
		return protocol.FaceBottom
	}
	return protocol.FaceTop
}

func entryFaceZ(step int32) int32 {
	if step > 0 {
		return protocol.FaceNorth
	}
	return protocol.FaceSouth
}

// backFace is the face the ray runs into: the dominant axis from the far side,
// ties y before z before x, vanilla's getApproximateNearest(diff).getOpposite()
// for a hit at the segment's start.
func backFace(dx, dy, dz float64) int32 {
	ay, az, ax := math.Abs(dy), math.Abs(dz), math.Abs(dx)
	switch {
	case ay >= ax && ay >= az:
		return entryFaceY(stepOf(dy))
	case az >= ax:
		return entryFaceZ(stepOf(dz))
	default:
		return entryFaceX(stepOf(dx))
	}
}

// TraverseBlocks walks the cells the segment from (fx, fy, fz) along (dx, dy,
// dz) crosses, nearest first, until visit returns stop: the start cell, then
// one per boundary, none past the end (a zero segment visits only the start).
// face is the entry face. It reports whether a visit stopped it.
// vanilla: BlockGetter.traverseBlocks
func TraverseBlocks(fx, fy, fz, dx, dy, dz float64, visit func(x, y, z, face int32) bool) bool {
	cx, cy, cz := int32(math.Floor(fx)), int32(math.Floor(fy)), int32(math.Floor(fz))
	tx, ty, tz := int32(math.Floor(fx+dx)), int32(math.Floor(fy+dy)), int32(math.Floor(fz+dz))
	if visit(cx, cy, cz, backFace(dx, dy, dz)) {
		return true
	}
	if dx == 0 && dy == 0 && dz == 0 {
		return false
	}
	const epsilon = 1e-7
	stepX, stepY, stepZ := stepOf(dx), stepOf(dy), stepOf(dz)
	tMaxX, tDeltaX := spanOf(fx, dx, cx)
	tMaxY, tDeltaY := spanOf(fy, dy, cy)
	tMaxZ, tDeltaZ := spanOf(fz, dz, cz)
	for cx != tx || cy != ty || cz != tz {
		var face int32
		// Ties as vanilla's nested comparisons break them: z before y before x.
		switch {
		case tMaxX < tMaxY && tMaxX < tMaxZ:
			if tMaxX > 1+epsilon {
				return false
			}
			cx += stepX
			tMaxX += tDeltaX
			face = entryFaceX(stepX)
		case tMaxY < tMaxZ:
			if tMaxY > 1+epsilon {
				return false
			}
			cy += stepY
			tMaxY += tDeltaY
			face = entryFaceY(stepY)
		default:
			if tMaxZ > 1+epsilon {
				return false
			}
			cz += stepZ
			tMaxZ += tDeltaZ
			face = entryFaceZ(stepZ)
		}
		if visit(cx, cy, cz, face) {
			return true
		}
	}
	return false
}

// RayHit is where a clip stopped: the cell, the shape's entry face (a start
// inside reads the face run into), the fraction along the segment, the state,
// and whether it was a fluid shape rather than an outline. Cell and face are
// where a placement goes.
// vanilla: BlockHitResult
type RayHit struct {
	X, Y, Z int32
	Face    int32
	T       float64
	State   uint16
	Fluid   bool
}

// ClipBlocks returns the look ray's first stop: a block outline the ray enters,
// or the fluid's depth where the fluid rule names the state, whichever is
// nearer in the cell, the block winning ties. ok is false if nothing stopped
// it. A nil fluid leaves fluids to their (absent) outlines.
// vanilla: Level.clip, ClipContext.Block.OUTLINE
func (w *World) ClipBlocks(fx, fy, fz, dx, dy, dz float64, fluid func(state uint16) bool) (RayHit, bool) {
	var hit RayHit
	found := false
	TraverseBlocks(fx, fy, fz, dx, dy, dz, func(x, y, z, _ int32) bool {
		state := w.BlockAt(x, y, z)
		for _, b := range outlineBoxes(state) {
			if t, face, ok := SegmentAABB(fx, fy, fz, dx, dy, dz,
				float64(x)+b.x0, float64(y)+b.y0, float64(z)+b.z0,
				float64(x)+b.x1, float64(y)+b.y1, float64(z)+b.z1); ok && (!found || t < hit.T) {
				hit, found = RayHit{X: x, Y: y, Z: z, Face: face, T: t, State: state}, true
			}
		}
		if fluid != nil && fluid(state) {
			if b, ok := fluidBoxOf(w, state, x, y, z); ok {
				if t, face, ok := SegmentAABB(fx, fy, fz, dx, dy, dz,
					float64(x)+b.x0, float64(y)+b.y0, float64(z)+b.z0,
					float64(x)+b.x1, float64(y)+b.y1, float64(z)+b.z1); ok && (!found || t < hit.T) {
					hit, found = RayHit{X: x, Y: y, Z: z, Face: face, T: t, State: state, Fluid: true}, true
				}
			}
		}
		return found
	})
	return hit, found
}

// fluidBoxOf is a cell's fluid shape (FluidState's): its amount's depth, or the
// whole block under the same fluid.
func fluidBoxOf(w *World, state uint16, x, y, z int32) (box, bool) {
	fs := fluidHolding(state)
	if fs.kind == fluidNone {
		return box{}, false
	}
	return box{0, 0, 0, 1, fs.fluidHeight(w, x, y, z), 1}, true
}

// OutlineBounds is the cell-local bounds of a state's outline, false for one
// with none. vanilla: VoxelShape.bounds of BlockState.getShape
func OutlineBounds(state uint16) (minX, minY, minZ, maxX, maxY, maxZ float64, ok bool) {
	boxes := outlineBoxes(state)
	if len(boxes) == 0 {
		return 0, 0, 0, 0, 0, 0, false
	}
	b := boxes[0]
	minX, minY, minZ, maxX, maxY, maxZ = b.x0, b.y0, b.z0, b.x1, b.y1, b.z1
	for _, b := range boxes[1:] {
		minX, minY, minZ = min(minX, b.x0), min(minY, b.y0), min(minZ, b.z0)
		maxX, maxY, maxZ = max(maxX, b.x1), max(maxY, b.y1), max(maxZ, b.z1)
	}
	return minX, minY, minZ, maxX, maxY, maxZ, true
}

// outlines caches each state's outline; geometry never changes, so it fills
// once.
var outlines sync.Map

// outlineBoxes resolves a state's outline: the collision geometry where
// vanilla's outline is the collision, the loose blocks' own outlines where the
// collision is empty. States outside the registry stop no ray.
func outlineBoxes(state uint16) []box {
	if v, ok := outlines.Load(state); ok {
		return v.([]box)
	}
	var boxes []box
	if _, props, ok := registry.BlockStateName(state); ok {
		boxes = outlineOf(state, registry.BlockOf(state), props)
	}
	outlines.Store(state, boxes)
	return boxes
}

// outlineOf is a block's outline, coarser than vanilla's in places: the
// collision geometry where the outline is the collision, and the families with
// empty collision (fluids, fires, loose things) here. A ray a fuller shape
// stops that vanilla would let past is accepted generosity.
func outlineOf(state uint16, block registry.Block, props string) []box {
	p := registry.ParseStateProps(props)
	switch {
	case block == registry.BlockWater || block == registry.BlockLava || block == registry.BlockBubbleColumn ||
		block == registry.BlockAir || block == registry.BlockCaveAir || block == registry.BlockVoidAir ||
		block == registry.BlockNetherPortal || block == registry.BlockEndPortal || block == registry.BlockEndGateway ||
		block == registry.BlockFire || block == registry.BlockSoulFire:
		// Fluids and fires stop no ray; fluids are the fluid rule's.
		return nil
	case block == registry.BlockCobweb:
		// vanilla: WebBlock — a full-cube outline with no collision.
		return []box{unitBox()}
	case block == registry.BlockSnow:
		// Layers stack from the floor, 2/16 each.
		// vanilla: SnowLayerBlock
		n, _ := strconv.Atoi(p["layers"])
		if n < 1 || n > 8 {
			n = 1
		}
		return []box{slabBox(0, float64(n)*2)}
	case block == registry.BlockRedstoneWire || block == registry.BlockTripwire ||
		block.InGroup(registry.GroupPressurePlate):
		return []box{slabBox(0, 1)}
	case block.InTag(registry.TagIDRails):
		// vanilla: BaseRailBlock.getShape — SHAPE_SLOPE when ascending
		if strings.HasPrefix(registry.StateValue(state, registry.PropShape), "ascending_") {
			return []box{slabBox(0, 8)}
		}
		return []box{slabBox(0, 2)}
	case block.InGroup(registry.GroupCarpet) ||
		block == registry.BlockPinkPetals || block == registry.BlockLeafLitter:
		return []box{slabBox(0, 1)}
	case block == registry.BlockTorch || block == registry.BlockSoulTorch || block == registry.BlockRedstoneTorch ||
		block == registry.BlockCopperTorch:
		// vanilla: TorchBlock — the stand and the tip.
		return []box{boxF(6, 0, 6, 10, 10, 10), boxF(7, 10, 7, 9, 16, 9)}
	case block.InGroup(registry.GroupWallTorch):
		// Coarse: the stand against the wall its facing leaves behind.
		return []box{rotateNorth(boxF(6, 3, 14, 10, 15, 16), dirIndex(p["facing"]))}
	case block.InGroup(registry.GroupButton) || block == registry.BlockLever || block == registry.BlockTripwireHook:
		// Coarse: the plate the click lands on, on the face it hangs from.
		return []box{hangPlate(p["face"], p["facing"])}
	case block == registry.BlockGlowLichen || block == registry.BlockSculkVein || block == registry.BlockResinClump:
		return lichenBoxes(faceMask(p))
	case block == registry.BlockSeaPickle:
		return []box{boxF(5, 0, 5, 11, 6, 11)}
	case block.InGroup(registry.GroupCoralFan):
		return []box{slabBox(0, 1)}
	case block.InGroup(registry.GroupCoralWallFan):
		return []box{rotateNorth(boxF(0, 0, 15, 16, 16, 16), dirIndex(p["facing"]))}
	case block == registry.BlockFlowerPot || block.InGroup(registry.GroupFlowerPot):
		// Coarse: the pot's own body.
		return []box{boxF(4, 0, 4, 12, 6, 12)}
	case block.InGroup(registry.GroupSign) && !block.InGroup(registry.GroupWallSign):
		// vanilla: SignBlock — the panel and post; its sixteen rotation steps
		// are unmodelled.
		return []box{boxF(4, 0, 4, 12, 12, 12)}
	case block.InGroup(registry.GroupWallSign):
		// vanilla: WallSignBlock — the panel against the wall behind its
		// facing.
		return []box{rotateNorth(boxF(0, 4, 14, 16, 12, 16), dirIndex(p["facing"]))}
	case block.InGroup(registry.GroupHangingSign):
		// Coarse: the plank the chains hold and the chains under it.
		return []box{boxF(0, 14, 0, 16, 16, 16), boxF(7, 0, 7, 9, 14, 9)}
	case block.InGroup(registry.GroupBanner):
		// Coarse: the pole and the flag it carries.
		return []box{boxF(7, 0, 7, 9, 16, 9), boxF(1, 4, 1, 15, 13, 15)}
	case block.InGroup(registry.GroupWallBanner):
		// Coarse: the pole on the wall and the flag it carries.
		d := dirIndex(p["facing"])
		return []box{rotateNorth(boxF(7, 0, 14, 9, 16, 16), d),
			rotateNorth(boxF(0, 0, 12, 16, 13, 16), d)}
	case block.InGroup(registry.GroupFence):
		// The collision cross at 16/16 tall.
		// vanilla: FenceBlock.getShape
		return crossCollisionBoxes(p, 6, 10, 16)
	case block.InGroup(registry.GroupWall):
		return wallOutlineBoxes(p)
	case block.InGroup(registry.GroupFenceGate):
		return gateOutlineBoxes(p)
	}
	if boxes := collisionGeometry(state, block, props); len(boxes) > 0 {
		return boxes
	}
	// Everything else is a full cube: coarse for cross plants' diagonals and
	// candles' wax.
	return []box{unitBox()}
}

// wallOutlineBoxes is the 16/16 post when up, and each connected side's arm:
// 16/16 tall, 14/16 low.
// vanilla: WallBlock.getShape
func wallOutlineBoxes(p map[string]string) []box {
	var boxes []box
	if p["up"] == "true" {
		boxes = append(boxes, boxF(4, 0, 4, 12, 16, 12))
	}
	side := func(v string, x0, z0, x1, z1 float64) {
		if v == "none" {
			return
		}
		h := 16.0
		if v == "low" {
			h = 14
		}
		boxes = append(boxes, boxF(x0, 0, z0, x1, h, z1))
	}
	side(p["north"], 5, 0, 11, 11)
	side(p["south"], 5, 5, 11, 16)
	side(p["west"], 0, 5, 11, 11)
	side(p["east"], 5, 5, 16, 11)
	return boxes
}

// gateOutlineBoxes is the gate's plate, 16/16 tall or 13/16 in_wall, open or
// shut.
// vanilla: FenceGateBlock.getShape
func gateOutlineBoxes(p map[string]string) []box {
	h := 16.0
	if p["in_wall"] == "true" {
		h = 13
	}
	if p["facing"] == "east" || p["facing"] == "west" {
		return []box{boxF(6, 0, 0, 10, h, 16)}
	}
	return []box{boxF(0, 0, 6, 16, h, 10)}
}

// hangPlate is a button's, lever's or tripwire hook's coarse outline: a small
// plate on the face it hangs from, facing pointing away from that face.
func hangPlate(face, facing string) box {
	switch face {
	case "floor":
		return boxF(5, 0, 5, 11, 2, 11)
	case "ceiling":
		return boxF(5, 14, 5, 11, 16, 11)
	default: // wall
		return rotateNorth(boxF(5, 5, 14, 11, 11, 16), dirIndex(facing))
	}
}

// lichenBoxes is a multiface block's outline (lichen, sculk vein, resin clump):
// a 1/16 plate per face in the mask, in the face properties' direction order.
func lichenBoxes(mask uint8) []box {
	plates := [6]box{
		boxF(0, 0, 0, 16, 1, 16),
		boxF(0, 15, 0, 16, 16, 16),
		boxF(0, 0, 0, 16, 16, 1),
		boxF(0, 0, 15, 16, 16, 16),
		boxF(0, 0, 0, 1, 16, 16),
		boxF(15, 0, 0, 16, 16, 16),
	}
	var boxes []box
	for d := range 6 {
		if mask&(1<<d) != 0 {
			boxes = append(boxes, plates[d])
		}
	}
	return boxes
}

// CollisionBlocked reports whether the segment crosses a block's collision
// shape; fluids and shapes without collision (grass, torches) do not stop it.
// vanilla: Level.clip, ClipContext.Block.COLLIDER
func (w *World) CollisionBlocked(fx, fy, fz, dx, dy, dz float64) bool {
	_, _, _, ok := ClipCollider(w, fx, fy, fz, dx, dy, dz)
	return ok
}

// ClipCollider returns the cell of the first block collision box the segment
// meets; fluids and shapes without collision do not stop it.
// vanilla: Level.clip, ClipContext.Block.COLLIDER, Fluid.NONE
func ClipCollider(w BlockReader, fx, fy, fz, dx, dy, dz float64) (x, y, z int32, ok bool) {
	TraverseBlocks(fx, fy, fz, dx, dy, dz, func(cx, cy, cz, _ int32) bool {
		for _, b := range CollisionBoxes(w.BlockAt(cx, cy, cz)) {
			if _, _, hit := SegmentAABB(fx, fy, fz, dx, dy, dz,
				float64(cx)+b.MinX, float64(cy)+b.MinY, float64(cz)+b.MinZ,
				float64(cx)+b.MaxX, float64(cy)+b.MaxY, float64(cz)+b.MaxZ); hit {
				x, y, z, ok = cx, cy, cz, true
				return true
			}
		}
		return false
	})
	return
}
