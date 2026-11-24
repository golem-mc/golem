package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// pistonStep is a moving piston's progress per game tick.
// vanilla: PistonMovingBlockEntity.tick
const pistonStep = 0.5

// PistonPush is one tick of a moving piston ride sweeping its cell, carrying
// the bodies its shape runs into. The server applies it to every kind of body.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities
type PistonPush struct {
	X, Y, Z int32
	// Dir is the movement direction: the ride's facing, reversed on a
	// retraction.
	Dir int
	// Progress is the ride's progress before this tick's step.
	Progress float64
	// Extending and Source are the ride's flags; a retracting source piston
	// also pushes bodies out of its base.
	Extending, Source bool
	// Boxes is the collision shape the ride carries, in cell units.
	Boxes []CollisionBox
	// Bounce marks a slime block: bodies hit take its movement axis as
	// velocity.
	Bounce bool
	// Sticky marks a honey block: bodies on it ride a horizontal movement.
	Sticky bool
	// Top is the carried block's collision height, where the sticky region
	// starts.
	Top float64

	// reach bounds every box Shift or Carries can move, once reachSet.
	reach    [6]float64
	reachSet bool
}

// origin is where the shape's cell stands this tick, shifted along the
// ride's facing.
func (pp *PistonPush) origin() (ox, oy, oz float64) {
	off := pp.offset()
	f := dirDelta[pp.facing()]
	return float64(pp.X) + off*float64(f[0]), float64(pp.Y) + off*float64(f[1]), float64(pp.Z) + off*float64(f[2])
}

// stickyRegion is the box above a horizontal honey ride whose bodies it
// carries, and false for any other ride.
func (pp *PistonPush) stickyRegion() ([6]float64, bool) {
	if !pp.Sticky || (pp.Dir != dirNorth && pp.Dir != dirSouth && pp.Dir != dirEast && pp.Dir != dirWest) {
		return [6]float64{}, false
	}
	ox, oy, oz := pp.origin()
	return [6]float64{ox, oy + pp.Top, oz, ox + 1, oy + 1.5000010000000001, oz + 1}, true
}

// Reaches reports whether the box overlaps the union of the ride's swept slabs
// and sticky region: false means neither Shift nor Carries moves it.
func (pp *PistonPush) Reaches(e [6]float64) bool {
	return overlaps(pp.reachBox(), e)
}

// ReachesFrom reports whether a box whose bottom is minY can reach the ride:
// false means Reaches is false for any such box, whatever its size.
func (pp *PistonPush) ReachesFrom(minY float64) bool {
	return minY < pp.reachBox()[4]
}

// ReachBox is the union of the ride's swept slabs and sticky region as
// {minX, minY, minZ, maxX, maxY, maxZ}; empty (min above max) with neither.
func (pp *PistonPush) ReachBox() [6]float64 { return pp.reachBox() }

// reachBox is the union of the ride's swept slabs and sticky region, built on
// first use; with neither it is empty (min above max), so nothing overlaps.
func (pp *PistonPush) reachBox() [6]float64 {
	if !pp.reachSet {
		r := [6]float64{math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1), math.Inf(-1)}
		grow := func(b [6]float64) {
			r = [6]float64{min(r[0], b[0]), min(r[1], b[1]), min(r[2], b[2]), max(r[3], b[3]), max(r[4], b[4]), max(r[5], b[5])}
		}
		ox, oy, oz := pp.origin()
		for _, c := range pp.Boxes {
			grow(pp.swept([6]float64{c.MinX + ox, c.MinY + oy, c.MinZ + oz, c.MaxX + ox, c.MaxY + oy, c.MaxZ + oz}))
		}
		if region, ok := pp.stickyRegion(); ok {
			grow(region)
		}
		pp.reach, pp.reachSet = r, true
	}
	return pp.reach
}

// offset is how far the shape stands from its cell along the ride's facing.
func (pp *PistonPush) offset() float64 {
	if pp.Extending {
		return pp.Progress - 1
	}
	return 1 - pp.Progress
}

// facing is the ride's own direction, which Dir reverses on a retraction.
func (pp *PistonPush) facing() int {
	if pp.Extending {
		return pp.Dir
	}
	return blocklogic.Opposite(pp.Dir)
}

// swept is the slab in front of the box's leading face a step sweeps.
// vanilla: PistonMath.getMovementArea
func (pp *PistonPush) swept(b [6]float64) [6]float64 {
	d := dirDelta[pp.Dir]
	step := pistonStep
	switch {
	case d[0] > 0:
		return [6]float64{b[3], b[1], b[2], b[3] + step, b[4], b[5]}
	case d[0] < 0:
		return [6]float64{b[0] - step, b[1], b[2], b[0], b[4], b[5]}
	case d[1] > 0:
		return [6]float64{b[0], b[4], b[2], b[3], b[4] + step, b[5]}
	case d[1] < 0:
		return [6]float64{b[0], b[1] - step, b[2], b[3], b[1], b[5]}
	case d[2] > 0:
		return [6]float64{b[0], b[1], b[5], b[3], b[4], b[5] + step}
	}
	return [6]float64{b[0], b[1], b[2] - step, b[3], b[4], b[2]}
}

// overlaps reports a strict box overlap.
func overlaps(a, b [6]float64) bool {
	return a[0] < b[3] && a[3] > b[0] && a[1] < b[4] && a[4] > b[1] && a[2] < b[5] && a[5] > b[2]
}

// reach is how far the box must move along dir to clear the slab.
// vanilla: PistonMovingBlockEntity.getMovement
func reach(dir int, slab, e [6]float64) float64 {
	switch dir {
	case dirEast:
		return slab[3] - e[0]
	case dirWest:
		return e[3] - slab[0]
	case dirUp:
		return slab[4] - e[1]
	case dirDown:
		return e[4] - slab[1]
	case dirSouth:
		return slab[5] - e[2]
	}
	return e[5] - slab[2]
}

// Shift is the offset along the movement axis that the box (minX, minY, minZ,
// maxX, maxY, maxZ) is carried this tick, and whether the ride touched it.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities
func (pp *PistonPush) Shift(e [6]float64) (dx, dy, dz float64, hit bool) {
	d := dirDelta[pp.Dir]
	step := pistonStep
	ox, oy, oz := pp.origin()
	delta := 0.0
	for _, c := range pp.Boxes {
		b := [6]float64{c.MinX + ox, c.MinY + oy, c.MinZ + oz, c.MaxX + ox, c.MaxY + oy, c.MaxZ + oz}
		slab := pp.swept(b)
		if !overlaps(slab, e) {
			continue
		}
		delta = math.Max(delta, reach(pp.Dir, slab, e))
		if delta >= step {
			break
		}
	}
	if delta <= 0 {
		return 0, 0, 0, false
	}
	delta = math.Min(delta, step) + 0.01
	net := delta
	if !pp.Extending && pp.Source {
		// A body the withdrawal left inside the base is pushed back out.
		// vanilla: PistonMovingBlockEntity.fixEntityWithinPistonBase
		m := [6]float64{e[0] + delta*float64(d[0]), e[1] + delta*float64(d[1]), e[2] + delta*float64(d[2]),
			e[3] + delta*float64(d[0]), e[4] + delta*float64(d[1]), e[5] + delta*float64(d[2])}
		cell := [6]float64{float64(pp.X), float64(pp.Y), float64(pp.Z), float64(pp.X) + 1, float64(pp.Y) + 1, float64(pp.Z) + 1}
		if overlaps(m, cell) {
			back := blocklogic.Opposite(pp.Dir)
			inter := [6]float64{max(m[0], cell[0]), max(m[1], cell[1]), max(m[2], cell[2]),
				min(m[3], cell[3]), min(m[4], cell[4]), min(m[5], cell[5])}
			out := reach(back, cell, m) + 0.01
			if math.Abs(out-(reach(back, cell, inter)+0.01)) < 0.01 {
				net -= math.Min(out, step) + 0.01
			}
		}
	}
	return net * float64(d[0]), net * float64(d[1]), net * float64(d[2]), true
}

// Carries reports whether a sticky ride carries the box of a body resting on
// the ground, and by how much.
// vanilla: PistonMovingBlockEntity.moveStuckEntities
func (pp *PistonPush) Carries(e [6]float64, centreX, centreZ float64) (dx, dz float64, ok bool) {
	region, ok := pp.stickyRegion()
	if !ok {
		return 0, 0, false
	}
	if centreX < region[0] || centreX > region[3] || centreZ < region[2] || centreZ > region[5] {
		return 0, 0, false
	}
	// vanilla: Level.getEntities box test, strict overlap
	if !overlaps(region, e) {
		return 0, 0, false
	}
	d := dirDelta[pp.Dir]
	return float64(d[0]) * pistonStep, float64(d[2]) * pistonStep, true
}

// pistonRideShape is a ride's collision shape at progress: its carried block,
// or for a retracting source piston the head, short once drawn in past a
// quarter.
// vanilla: PistonMovingBlockEntity.getCollisionRelatedBlockState
func pistonRideShape(moved uint16, extending, source bool, progress float64) uint16 {
	block := registry.BlockOf(moved)
	if extending || !source || !pistonBlocks[block] {
		return moved
	}
	head, _ := registry.WithBool(pistonHeadStateOf(facingOfState(moved), block == registry.BlockStickyPiston),
		registry.PropShort, progress > 0.25)
	return head
}

// pistonPushes books each live ride's sweep this tick: a new ride's first step,
// a tick-old one's second.
func (p *Physics) pistonPushes() {
	live := p.rides[:0]
	if len(p.rides) == 0 {
		return
	}
	// Sized up front: the effects point into the slab, so it must not move.
	p.pushes, p.sparePushes = p.sparePushes[:0], nil
	if cap(p.pushes) < len(p.rides) {
		p.pushes = make([]PistonPush, 0, len(p.rides))
	}
	for _, pos := range p.rides {
		r, age, ok := p.w.stepPistonRide(pos[0], pos[1], pos[2], p.now)
		if !ok {
			delete(p.rideSet, pos)
			continue
		}
		push := PistonPush{
			X: pos[0], Y: pos[1], Z: pos[2],
			Dir: int(r.dir), Extending: r.extending, Source: r.isSource,
		}
		if !r.extending {
			push.Dir = blocklogic.Opposite(push.Dir)
		}
		push.Progress = float64(age) * pistonStep
		shape := pistonRideShape(r.movedState, r.extending, r.isSource, push.Progress)
		push.Boxes = CollisionBoxes(shape)
		push.Top = CollisionTop(r.movedState)
		block := registry.BlockOf(r.movedState)
		push.Bounce = block == registry.BlockSlimeBlock
		push.Sticky = block == registry.BlockHoneyBlock
		if age >= 0 && age <= 1 {
			p.pushes = append(p.pushes, push)
			p.effects = append(p.effects, Effect{X: pos[0], Y: pos[1], Z: pos[2], PistonPush: &p.pushes[len(p.pushes)-1]})
		}
		if age < 1 {
			live = append(live, pos)
		} else {
			delete(p.rideSet, pos)
		}
	}
	p.rides = live
}

// MovingPistonBoxes is the moving piston's collision at the cell: its carried
// shape shifted along its facing by the ride's progress, none without a ride.
// The no-clip direction that lets a body inside the head through is not
// modelled.
// vanilla: PistonMovingBlockEntity.getCollisionShape
func (w *World) MovingPistonBoxes(x, y, z int32) []CollisionBox {
	r, ok := w.pistonRideAt(x, y, z)
	if !ok {
		return nil
	}
	moved, dir, extending, source, progress := r.movedState, int(r.dir), r.extending, r.isSource, r.rideProgress
	var out []CollisionBox
	shape := moved
	if source {
		block := registry.BlockOf(moved)
		if !extending && pistonBlocks[block] {
			// A pulled piston's own base stays out while its head draws in.
			out = append(out, CollisionBoxes(pistonStateOf(block == registry.BlockStickyPiston, dir, true))...)
		}
		shape, _ = registry.WithBool(pistonHeadStateOf(dir, false), registry.PropShort, extending != (1-progress < 0.25))
	}
	off := progress - 1
	if !extending {
		off = 1 - progress
	}
	f := dirDelta[dir]
	for _, c := range CollisionBoxes(shape) {
		out = append(out, CollisionBox{
			MinX: c.MinX + off*float64(f[0]), MinY: c.MinY + off*float64(f[1]), MinZ: c.MinZ + off*float64(f[2]),
			MaxX: c.MaxX + off*float64(f[0]), MaxY: c.MaxY + off*float64(f[1]), MaxZ: c.MaxZ + off*float64(f[2]),
		})
	}
	return out
}

// IsMovingPiston reports whether the state is the moving piston block.
func IsMovingPiston(state uint16) bool {
	collisionTablesOnce.Do(initCollisionTables)
	return collisionMoving[state]
}
