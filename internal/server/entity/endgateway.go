package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// gatewaySweepCells caps the cells one tick's sweep visits.
// vanilla: Entity.maxMovementIterations
const gatewaySweepCells = 16

// EndGatewayInside finds a gateway overlapping the body or its movement.
// vanilla: Entity.checkInsideBlocks, applyEffectsFromBlocks
func EndGatewayInside(w World, from, to AABB) (x, y, z int32, found bool) {
	hit := func(box AABB) bool {
		box.insideCells(func(bx, by, bz int32) {
			if !found && w.BlockAt(bx, by, bz) == rules.EndGatewayState {
				x, y, z, found = bx, by, bz, true
			}
		})
		return found
	}
	if hit(to) {
		return
	}
	fx, fz := from.centerXZ()
	tx, tz := to.centerXZ()
	dx, dy, dz := tx-fx, to.minY-from.minY, tz-fz
	length2 := dx*dx + dy*dy + dz*dz
	if length2 == 0 {
		return
	}
	visited := 0
	world.TraverseBlocks(fx, from.minY, fz, dx, dy, dz, func(cx, cy, cz, _ int32) bool {
		if visited++; visited > gatewaySweepCells {
			return true
		}
		t := ((float64(cx)+0.5-fx)*dx + (float64(cy)+0.5-from.minY)*dy + (float64(cz)+0.5-fz)*dz) / length2
		t = math.Max(0, math.Min(1, t))
		return hit(from.moved(dx*t, dy*t, dz*t))
	})
	return
}
