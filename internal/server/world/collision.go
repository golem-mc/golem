package world

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/registry"
)

// Box-vs-block collision for the pose system: whether any block's collision
// shape crosses a box. It is the blocks-only half of
// CollisionGetter.noCollision (bodies never push each other here), reading the
// entity sweep's per-state table except powder snow's sweep-only cell and
// scaffolding, made body-relative by a simpler rule than the sweep's. The test
// is strict: touching is not crossing.
// vanilla: BlockCollisions

// CollisionFull reports whether a state's collision is the full cube
// (isCollisionShapeFullBlock), as a door's hinge asks of its neighbours. The
// generated block-support table carries vanilla's answer exactly.
func CollisionFull(state uint16) bool {
	return registry.CollisionShapeFull(state)
}

// CollisionBox is one axis-aligned piece of a state's collision, in block units
// (the cell spans 0 to 1).
type CollisionBox struct {
	MinX, MinY, MinZ float64
	MaxX, MaxY, MaxZ float64
}

// collisionBoxes is every state's collision, resolved once so the sweep's
// per-cell read needs no map. The tops are kept beside it so the mover and the
// height read cannot disagree.
var collisionBoxes [65536][]CollisionBox
var collisionTops [65536]float64
var collisionTablesOnce sync.Once

// collisionScaffolding marks scaffolding states, whose shape is empty for a
// body not above the cell.
// vanilla: ScaffoldingBlock.getCollisionShape
var collisionScaffolding [65536]bool

// collisionScaffoldingUnstable marks scaffolding with an unsupported bottom
// piece (bottom=true, distance!=0).
var collisionScaffoldingUnstable [65536]bool

// collisionApprox marks states the table rounds up to a whole cell, covered by
// neither a Go model nor generated boxes.
var collisionApprox [65536]bool

// collisionMoving marks moving piston states, whose shape is the ride's, not
// the state's.
var collisionMoving [65536]bool

// collisionShulker marks shulker box states, whose shape is the lid's.
var collisionShulker [65536]bool

// collisionPlain marks table boxes inside the unit cell, with no position
// offset or body-dependent collision.
var collisionPlain [65536]bool

var collisionPossible [65536]bool

// MayCollide reports whether any body context can give the state a collision.
func MayCollide(state uint16) bool {
	collisionTablesOnce.Do(initCollisionTables)
	return collisionPossible[state]
}

// PlainCollision is CollisionBoxes with whether the boxes are the whole story:
// ok means they sit inside the cell and do not depend on the cell's
// position or the body, so a sweep can rule the cell out by its coordinates.
func PlainCollision(state uint16) (boxes []CollisionBox, ok bool) {
	collisionTablesOnce.Do(initCollisionTables)
	return collisionBoxes[state], collisionPlain[state]
}

// CollisionBoxes lists a state's collision for the entity sweep
// (Entity.collide's shape) as cell-local bounds.
// vanilla: BlockState.getCollisionShape
func CollisionBoxes(state uint16) []CollisionBox {
	if state == airState {
		return nil
	}
	collisionTablesOnce.Do(initCollisionTables)
	return collisionBoxes[state]
}

// scaffoldingStable is ScaffoldingBlock.SHAPE_STABLE (top plate and corner
// posts); scaffoldingUnstableBottom is SHAPE_UNSTABLE_BOTTOM.
var (
	scaffoldingStable = []CollisionBox{
		{MinY: 14.0 / 16, MaxX: 1, MaxY: 1, MaxZ: 1},
		{MaxX: 2.0 / 16, MaxY: 1, MaxZ: 2.0 / 16},
		{MinX: 14.0 / 16, MaxX: 1, MaxY: 1, MaxZ: 2.0 / 16},
		{MaxX: 2.0 / 16, MaxY: 1, MinZ: 14.0 / 16, MaxZ: 1},
		{MinX: 14.0 / 16, MaxX: 1, MaxY: 1, MinZ: 14.0 / 16, MaxZ: 1},
	}
	scaffoldingUnstableBottom = []CollisionBox{{MaxX: 1, MaxY: 2.0 / 16, MaxZ: 1}}
)

// BodyAbove reports whether a body with its floor at bodyBottom stands on a
// shape topping at top, within the float epsilon.
// vanilla: EntityCollisionContext.isAbove
func BodyAbove(bodyBottom, top float64) bool {
	const above = float64(float32(1e-5))
	return bodyBottom > top-above
}

// ScaffoldingBoxes is a scaffolding state's collision at row cellY for a body
// with its floor at bodyBottom: the stable shape when above the cell, the
// unstable bottom plate when above that plate, else nothing. The descending
// flag is not modelled (no mob or item sneaks). ok is false for other states.
// vanilla: ScaffoldingBlock.getCollisionShape
func ScaffoldingBoxes(state uint16, cellY int32, bodyBottom float64) (boxes []CollisionBox, ok bool) {
	collisionTablesOnce.Do(initCollisionTables)
	if !collisionScaffolding[state] {
		return nil, false
	}
	if BodyAbove(bodyBottom, float64(cellY)+1) {
		return scaffoldingStable, true
	}
	if collisionScaffoldingUnstable[state] && BodyAbove(bodyBottom, float64(cellY)) {
		return scaffoldingUnstableBottom, true
	}
	return nil, true
}

// CollisionTop is the highest top among a state's collision boxes, above its
// cell floor.
// vanilla: BlockState.getCollisionShape
func CollisionTop(state uint16) float64 {
	if state == airState {
		return 0
	}
	collisionTablesOnce.Do(initCollisionTables)
	return collisionTops[state]
}

// initCollisionTables resolves every state's boxes and tops once.
func initCollisionTables() {
	for id := range collisionBoxes {
		boxes, approx := resolveCollisionBoxes(uint16(id))
		collisionApprox[id] = approx
		collisionBoxes[id] = boxes
		collisionPlain[id] = offsetKind[id] == 0 && inUnitCell(boxes)
		var top float64
		for _, b := range boxes {
			top = max(top, b.MaxY)
		}
		collisionTops[id] = top
		if id >= registry.StateCount {
			continue
		}
		b := registry.BlockOf(uint16(id))
		collisionScaffolding[id] = b == registry.BlockScaffolding
		if collisionScaffolding[id] {
			p := registry.StateProps(uint16(id))
			collisionScaffoldingUnstable[id] = p["distance"] != "0" && p["bottom"] == "true"
		}
		collisionMoving[id] = b == registry.BlockMovingPiston
		collisionShulker[id] = b.IsShulkerBox()
		// Powder snow and lava can hold a body despite their empty default shape.
		collisionPossible[id] = len(boxes) != 0 || collisionScaffolding[id] || collisionMoving[id] || collisionShulker[id] || b == registry.BlockPowderSnow || b == registry.BlockLava
		collisionPlain[id] = collisionPlain[id] && !collisionScaffolding[id] && !collisionMoving[id] && !collisionShulker[id] && b != registry.BlockPowderSnow && b != registry.BlockLava
	}
}

func inUnitCell(boxes []CollisionBox) bool {
	for _, b := range boxes {
		if b.MinX < 0 || b.MinY < 0 || b.MinZ < 0 || b.MaxX > 1 || b.MaxY > 1 || b.MaxZ > 1 {
			return false
		}
	}
	return true
}

// resolveCollisionBoxes resolves one state: empty where the generated table has
// no shape, else the modelled family's boxes, else the reference server's
// generated boxes, else that height as one full-floor box.
func resolveCollisionBoxes(state uint16) ([]CollisionBox, bool) {
	_, props, ok := registry.BlockStateName(state)
	if !ok {
		return nil, false
	}
	// The table decides whether a state collides at all: a family may model a
	// fluid-only shape the sweep must not read (frogspawn's 1.5/16 plate has no
	// collision).
	h := registry.CollisionHeight(state)
	if h == 0 {
		return nil, false
	}
	if geo, modelled := modelledGeometry(state, registry.BlockOf(state), registry.ParseStateProps(props)); modelled && len(geo) > 0 {
		out := make([]CollisionBox, len(geo))
		for i, b := range geo {
			out[i] = CollisionBox{MinX: b.x0, MinY: b.y0, MinZ: b.z0, MaxX: b.x1, MaxY: b.y1, MaxZ: b.z1}
		}
		return out, false
	}
	// Unmodelled families (beds, hoppers, cauldrons) carry the reference
	// server's boxes; a family that gains a Go model reads that instead.
	if boxes, ok := registry.BlockShapeBoxes(state); ok {
		out := make([]CollisionBox, len(boxes))
		for i, b := range boxes {
			out[i] = CollisionBox{
				MinX: float64(b[0]) / 32, MinY: float64(b[1]) / 32, MinZ: float64(b[2]) / 32,
				MaxX: float64(b[3]) / 32, MaxY: float64(b[4]) / 32, MaxZ: float64(b[5]) / 32,
			}
		}
		return out, false
	}
	return []CollisionBox{{MaxX: 1, MaxY: h, MaxZ: 1}}, !registry.CollisionFlat(state)
}

// CollisionOffset is OffsetType.XZ's shift of a state's shape within its cell
// at (x, z).
func CollisionOffset(state uint16, x, z int32) (dx, dz float64) {
	return collisionOffset(state, x, z)
}

// BoxCollides reports whether any block's collision crosses the box, strictly:
// the blocks-only half of noCollision. A box touching a face exactly does not
// collide, which lets the pose test fit vanilla's box (shaved by 1e-6) under a
// ceiling the body stops just below.
//
// The walk covers vanilla's cells: from one below the shrunken floor to one
// past the swollen ceiling, so a shape poking out of its cell is still seen.
func (w *World) BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	const eps = 1e-7
	minCX := int32(math.Floor(minX-eps)) - 1
	maxCX := int32(math.Floor(maxX+eps)) + 1
	minCY := int32(math.Floor(minY-eps)) - 1
	maxCY := int32(math.Floor(maxY+eps)) + 1
	minCZ := int32(math.Floor(minZ-eps)) - 1
	maxCZ := int32(math.Floor(maxZ+eps)) + 1
	var scratch [walkCells]uint16
	cells := w.BlocksIn(minCX, minCY, minCZ, maxCX, maxCY, maxCZ, scratch[:0])
	ny, nz := int(maxCY-minCY+1), int(maxCZ-minCZ+1)
	for y := minCY; y <= maxCY; y++ {
		for z := minCZ; z <= maxCZ; z++ {
			for x := minCX; x <= maxCX; x++ {
				state := cells[int(x-minCX)*ny*nz+int(y-minCY)*nz+int(z-minCZ)]
				if IsMovingPiston(state) {
					// The ride's shape is the cell's, not the state's.
					for _, b := range w.MovingPistonBoxes(x, y, z) {
						if b.MinX+float64(x) < maxX && b.MaxX+float64(x) > minX &&
							b.MinY+float64(y) < maxY && b.MaxY+float64(y) > minY &&
							b.MinZ+float64(z) < maxZ && b.MaxZ+float64(z) > minZ {
							return true
						}
					}
					continue
				}
				if IsShulkerBoxShape(state) {
					for _, b := range w.ShulkerBoxBoxes(state, x, y, z) {
						if b.MinX+float64(x) < maxX && b.MaxX+float64(x) > minX &&
							b.MinY+float64(y) < maxY && b.MaxY+float64(y) > minY &&
							b.MinZ+float64(z) < maxZ && b.MaxZ+float64(z) > minZ {
							return true
						}
					}
					continue
				}
				if StateCollides(state, x, y, z, minX, minY, minZ, maxX, maxY, maxZ) {
					return true
				}
			}
		}
	}
	return false
}

// StateCollides reports whether state's collision at (x, y, z) strictly crosses
// the box.
func StateCollides(state uint16, x, y, z int32, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	collisionTablesOnce.Do(initCollisionTables)
	// Scaffolding is empty unless the body is above the cell; the unstable
	// bottom and descending flag are not modelled.
	// vanilla: ScaffoldingBlock.getCollisionShape
	if collisionScaffolding[state] && minY <= float64(y)+1-1e-5 {
		return false
	}
	ox, oz := collisionOffset(state, x, z)
	fx, fy, fz := float64(x)+ox, float64(y), float64(z)+oz
	for _, b := range CollisionBoxes(state) {
		if b.MinX+fx < maxX && b.MaxX+fx > minX &&
			b.MinY+fy < maxY && b.MaxY+fy > minY &&
			b.MinZ+fz < maxZ && b.MaxZ+fz > minZ {
			return true
		}
	}
	return false
}

// CollidesAnew reports whether moving from box from to box to crosses a block
// shape not already crossed (both shrunk by vanilla's 1e-5f). Approximated
// states are skipped, as is scaffolding, whose shape depends on descending (not
// modelled) and which a sneak down through is a legal step. Collidable entities
// are not modelled.
// vanilla: ServerGamePacketListenerImpl.isEntityCollidingWithAnythingNew
func (w *World) CollidesAnew(from, to [6]float64) bool {
	collisionTablesOnce.Do(initCollisionTables)
	const shrink = float64(float32(1e-5)) // vanilla: AABB.deflate(1.0E-5f)
	f, n := from, to
	for i := range 3 {
		f[i] += shrink
		f[i+3] -= shrink
		n[i] += shrink
		n[i+3] -= shrink
	}
	const eps = 1e-7
	minCX := int32(math.Floor(n[0]-eps)) - 1
	maxCX := int32(math.Floor(n[3]+eps)) + 1
	minCY := int32(math.Floor(n[1]-eps)) - 1
	maxCY := int32(math.Floor(n[4]+eps)) + 1
	minCZ := int32(math.Floor(n[2]-eps)) - 1
	maxCZ := int32(math.Floor(n[5]+eps)) + 1
	var scratch [walkCells]uint16
	cells := w.BlocksIn(minCX, minCY, minCZ, maxCX, maxCY, maxCZ, scratch[:0])
	ny, nz := int(maxCY-minCY+1), int(maxCZ-minCZ+1)
	for y := minCY; y <= maxCY; y++ {
		for z := minCZ; z <= maxCZ; z++ {
			for x := minCX; x <= maxCX; x++ {
				state := cells[int(x-minCX)*ny*nz+int(y-minCY)*nz+int(z-minCZ)]
				// A shape rounded up to a whole cell would refuse steps vanilla
				// allows (cactus, honey, composter).
				if state == airState || collisionScaffolding[state] || collisionApprox[state] {
					continue
				}
				if StateCollides(state, x, y, z, n[0], n[1], n[2], n[3], n[4], n[5]) &&
					!StateCollides(state, x, y, z, f[0], f[1], f[2], f[3], f[4], f[5]) {
					return true
				}
			}
		}
	}
	return false
}

// walkCells sizes the box walks' stack buffer: a player's box plus its shell is
// 5x5x6 = 150 cells; larger boxes use the heap.
const walkCells = 192

// CollisionEmpty reports whether the cell's block has no collision shape.
func (w *World) CollisionEmpty(x, y, z int32) bool {
	return len(CollisionBoxes(w.BlockAt(x, y, z))) == 0
}

// SpawnYOffset is EntityType.getYOffset: a body halfWidth wide, centred with
// its feet one block up, falls one block (two when movedUp) onto the shapes
// there; the result is its feet's height above the cell's floor.
func (w *World) SpawnYOffset(x, y, z int32, movedUp bool, halfWidth float64) float64 {
	const eps = 1e-7
	floor, dist := float64(y), -1.0
	if movedUp {
		floor, dist = floor-1, -2
	}
	feet := float64(y) + 1
	cx, cz := float64(x)+0.5, float64(z)+0.5
	minX, maxX := cx-halfWidth+eps, cx+halfWidth-eps
	minZ, maxZ := cz-halfWidth+eps, cz+halfWidth-eps
	// Start one cell lower: a taller-than-cell shape (a fence) reaches up into
	// the space.
	for cy := int32(math.Floor(floor)) - 1; cy <= y; cy++ {
		fy := float64(cy)
		state := w.BlockAt(x, cy, z)
		ox, oz := collisionOffset(state, x, z)
		for _, b := range CollisionBoxes(state) {
			bottom, top := b.MinY+fy, b.MaxY+fy
			if top <= floor || bottom >= feet {
				continue // outside the space getCollisions is asked about
			}
			if b.MinX+float64(x)+ox >= maxX || b.MaxX+float64(x)+ox <= minX ||
				b.MinZ+float64(z)+oz >= maxZ || b.MaxZ+float64(z)+oz <= minZ {
				continue
			}
			if top <= feet+eps {
				dist = max(dist, top-feet)
			}
		}
	}
	return 1 + dist
}
