package entity

import "github.com/golem-mc/golem/internal/server/rules"

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

// bubbleColumnPush applies the box's columns to a vertical speed and
// reports whether the body was in a column and whether it rode a surface;
// each surface cell is added to spots for its splash.
func bubbleColumnPush(w World, box AABB, vy float64, spots *[]bubbleSpot) (newVY float64, inside, surface bool) {
	newVY = vy
	bubbleColumns(w, box, func(d, top bool, x, y, z int32) {
		if top {
			surface = true
			newVY = rules.BubbleColumnAboveSpeed(newVY, d)
			*spots = append(*spots, bubbleSpot{x, y, z})
		} else {
			inside = true
			newVY = rules.BubbleColumnInsideSpeed(newVY, d)
		}
	})
	return newVY, inside, surface
}

// bubbleColumnDrift applies the box's columns to a vertical speed and
// reports a body inside one below the surface, so its fall can be cleared.
// It sends no particles; its callers' kinds have none written.
// vanilla: Entity.onInsideBubbleColumn, Entity.onAboveBubbleColumn
func bubbleColumnDrift(w World, box AABB, vy float64) (newVY float64, inside bool) {
	var spots []bubbleSpot
	newVY, inside, _ = bubbleColumnPush(w, box, vy, &spots)
	return newVY, inside
}

// bubbleColumnContact moves the mob with its box's columns; inside one
// below the surface its fall is cleared.
// vanilla: Entity.onInsideBubbleColumn, Entity.onAboveBubbleColumn
func (m *Mob) bubbleColumnContact(w World, scan *contactScan, events []MobEvent) []MobEvent {
	if !m.touches(w, scan, contactBubbleColumn) {
		return events
	}
	var spots []bubbleSpot
	vy, inside, _ := bubbleColumnPush(w, m.mobBox(), m.VY, &spots)
	m.VY = vy
	if inside {
		m.FallDistance = 0
	}
	for _, p := range spots {
		events = append(events, MobEvent{Mob: m, Kind: MobBubbleSurface, X: float64(p.x), Y: float64(p.y), Z: float64(p.z)})
	}
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
