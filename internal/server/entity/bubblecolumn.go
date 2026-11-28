package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// bubbleColumns walks the bubble columns in a box, yielding whether each
// drags down, whether the cell above is air (a surface cell), and its
// position.
// vanilla: BubbleColumnBlock.entityInside
func bubbleColumns(w World, box AABB, yield func(drag, surface bool, x, y, z int32)) {
	box.insideCells(func(x, y, z int32) {
		drag, ok := rules.BubbleColumnDrag(w.BlockAt(x, y, z))
		if !ok {
			return
		}
		yield(drag, bubbleSurfaceAt(w, x, y, z), x, y, z)
	})
}

// BubbleSurfaceCells yields the surface cells of the bubble columns a
// width x height box with its feet at (x, y, z) overlaps.
// vanilla: BubbleColumnBlock.entityInside, Player.onAboveBubbleColumn
func BubbleSurfaceCells(w World, x, y, z, width, height float64, yield func(x, y, z int32)) {
	hw := width / 2
	bubbleColumns(w, AABB{x - hw, y, z - hw, x + hw, y + height, z + hw}, func(_, surface bool, cx, cy, cz int32) {
		if surface {
			yield(cx, cy, cz)
		}
	})
}

// bubbleSurfaceAt reports a column cell with no collision and no fluid
// above, whose contact is the surface's.
func bubbleSurfaceAt(w World, x, y, z int32) bool {
	above := w.BlockAt(x, y+1, z)
	return blockExtent(above) == 0 && fluidCellOf(above).kind == fluidNone
}

// movedFarSqr is checkInsideBlocks' squared travel past which every cell a
// leg meets is a precise contact.
// vanilla: Entity.checkInsideBlocks, Mth.square(0.9999900000002526)
const movedFarSqr = 0.9999900000002526 * 0.9999900000002526

// projectileBubblePush applies the column cell (x, y, z), met on an inside
// walk's leg from box from with travel d, to a projectile's vertical speed.
// Only a precise contact counts: a leg of about a block or more, or a cell
// the box at the leg's end overlaps. A clamped body (a pearl or any
// non-projectile) takes the clamped entity speeds, every other projectile
// the unclamped ones; a surface cell is
// added to spots for its splash. ok is false for a cell that is no column.
// vanilla: BubbleColumnBlock.entityInside, Projectile.onAboveBubbleColumn,
// Projectile.onInsideBubbleColumn, ThrownEnderpearl.onAboveBubbleColumn
func projectileBubblePush(w World, x, y, z int32, state uint16, from AABB, dx, dy, dz, vy float64, clamped bool, spots *[]bubbleSpot) (newVY float64, ok bool) {
	drag, ok := rules.BubbleColumnDrag(state)
	if !ok {
		return vy, false
	}
	to := from.moved(dx, dy, dz)
	to.deflate(insideShave)
	precise := dx*dx+dy*dy+dz*dz > movedFarSqr ||
		to.minX < float64(x+1) && to.maxX > float64(x) &&
			to.minY < float64(y+1) && to.maxY > float64(y) &&
			to.minZ < float64(z+1) && to.maxZ > float64(z)
	if !precise {
		return vy, true
	}
	return projectileBubbleSpeed(w, x, y, z, drag, vy, clamped, spots), true
}

// projectileBubbleSpeed is a projectile's vertical speed after a precise
// contact with the column cell (x, y, z).
func projectileBubbleSpeed(w World, x, y, z int32, drag bool, vy float64, clamped bool, spots *[]bubbleSpot) float64 {
	if bubbleSurfaceAt(w, x, y, z) {
		*spots = append(*spots, bubbleSpot{x, y, z})
		switch {
		case clamped:
			return rules.BubbleColumnAboveSpeed(vy, drag)
		case drag:
			return vy - 0.03
		}
		return vy + 0.1
	}
	switch {
	case clamped:
		return rules.BubbleColumnInsideSpeed(vy, drag)
	case drag:
		return vy - 0.03
	}
	return vy + 0.06
}

// bubbleSpot is the cell of a column whose surface a body rides.
type bubbleSpot struct{ x, y, z int32 }

// bubbleTeleportSqr is the squared distance between two base ticks past
// which the mob is taken to have been teleported, not walked, so no leg is
// walked.
const bubbleTeleportSqr = 8 * 8

// bubbleColumnContact moves the mob with the columns its last move's cells
// crossed (the straight way from where the previous base tick found it);
// inside one below the surface its fall is cleared.
// vanilla: Entity.checkInsideBlocks, Entity.onInsideBubbleColumn, Entity.onAboveBubbleColumn
func (m *Mob) bubbleColumnContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	fx, fy, fz := m.X, m.Y, m.Z
	if m.bubbleFromSet {
		dx, dy, dz := m.X-m.bubbleFrom[0], m.Y-m.bubbleFrom[1], m.Z-m.bubbleFrom[2]
		if dx*dx+dy*dy+dz*dz <= bubbleTeleportSqr {
			fx, fy, fz = m.bubbleFrom[0], m.bubbleFrom[1], m.bubbleFrom[2]
		}
	}
	m.bubbleFrom, m.bubbleFromSet = [3]float64{m.X, m.Y, m.Z}, true
	rel := m.mobBox().moved(-m.X, -m.Y, -m.Z)
	box := rel.moved(m.X, m.Y, m.Z)
	if fx != m.X || fy != m.Y || fz != m.Z {
		// The box swept: the start's and the end's together.
		start := rel.moved(fx, fy, fz)
		box = AABB{math.Min(box.minX, start.minX), math.Min(box.minY, start.minY), math.Min(box.minZ, start.minZ),
			math.Max(box.maxX, start.maxX), math.Max(box.maxY, start.maxY), math.Max(box.maxZ, start.maxZ)}
	}
	if !scan.touches(w, box, contactBubbleColumn) {
		return events
	}
	var spots []bubbleSpot
	walkInside(w, []bodyMove{{fx, fy, fz, m.X, m.Y, m.Z, m.X - fx, m.Z - fz, true}}, rel,
		func(x, y, z int32, state uint16, _ int, from AABB, dx, dy, dz float64) bool {
			vy, ok := projectileBubblePush(w, x, y, z, state, from, dx, dy, dz, m.VY, true, &spots)
			if !ok {
				return true
			}
			m.VY = vy
			if len(spots) == 0 {
				m.FallDistance = 0
			}
			for _, p := range spots {
				events = append(events, MobEvent{Mob: m, Kind: MobBubbleSurface, X: float64(p.x), Y: float64(p.y), Z: float64(p.z)})
			}
			spots = spots[:0]
			return true
		})
	return events
}

const (
	// boatBubbleTicks is how long a boat must ride a column's surface before
	// being thrown; boatBubbleLift and boatBubbleSink are the throw speeds.
	// vanilla: AbstractBoat.tickBubbleColumn
	boatBubbleTicks = 60
	boatBubbleLift  = 0.6
	boatBubbleSink  = 0.7
)

// BoatThrow is the throw a boat's spell on a column's surface ended in, for
// the server to tell as an entity event.
type BoatThrow uint8

const (
	BoatThrowNone BoatThrow = iota
	// BoatThrowLaunch is an upward column's throw (entity event 72).
	BoatThrowLaunch
	// BoatThrowSink is a downward column's pull, which ejects the riders
	// (entity event 71).
	BoatThrowSink
)

// bubbleColumn moves the boat with its box's columns. Inside a column the
// generic speed cap applies; on the surface the boat banks time, and a full
// spell throws it. A ridden hull keeps only the bookkeeping, its driver's
// client applying the speeds when told the throw, except that the sink
// ejects the riders first and so moves the emptied hull itself.
// vanilla: AbstractBoat.onAboveBubbleColumn, AbstractBoat.tickBubbleColumn
func (b *Boat) bubbleColumn(w World, ridden bool) {
	// vanilla applies the block effects twice a tick: AbstractBoat.tick
	for range 2 {
		vy := b.VY
		bubbleColumns(w, b.boatBox(), func(drag, surface bool, _, _, _ int32) {
			if surface {
				b.aboveBubble = true
				b.bubbleDrag = drag
				if b.BubbleTime == 0 {
					b.BubbleTime = boatBubbleTicks
				}
				return
			}
			vy = rules.BubbleColumnInsideSpeed(vy, drag)
		})
		if !ridden {
			b.VY = vy
		}
	}
	if !b.aboveBubble {
		b.BubbleTime = 0
	}
	if b.BubbleTime > 0 {
		b.BubbleTime--
		if boatBubbleTicks-b.BubbleTime-1 > 0 && b.BubbleTime == 0 {
			if b.bubbleDrag {
				b.Thrown = BoatThrowSink
				b.EjectPassengers = true
				if ridden {
					// The riders are ejected before the effect, so the
					// server runs the emptied hull from a ridden hull's
					// zero delta. vanilla: AbstractBoat.java:233
					b.VX, b.VY, b.VZ = 0, -boatBubbleSink, 0
				} else {
					b.VY -= boatBubbleSink
				}
			} else {
				b.Thrown = BoatThrowLaunch
				if !ridden {
					b.VY = boatBubbleLift
				}
			}
		}
		b.aboveBubble = false
	}
}
