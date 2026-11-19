package entity

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// lilyPadState is the lily pad's only block state.
var lilyPadState, _ = registry.FindState("minecraft:lily_pad", nil)

// PortalState is a nether or end portal overlapping the hull, or zero.
// vanilla: Entity.checkInsideBlocks, EndPortalBlock.getEntityInsideCollisionShape
func (b *Boat) PortalState(w World) (state uint16) {
	box := b.boatBox()
	box.insideCells(func(x, y, z int32) {
		cell := w.BlockAt(x, y, z)
		// vanilla: EndPortalBlock.SHAPE, Block.column(16, 6, 12)
		end := cell == rules.EndPortalState && box.Crosses(AABB{
			float64(x), float64(y) + 6.0/16, float64(z),
			float64(x + 1), float64(y) + 12.0/16, float64(z + 1),
		})
		if rules.IsPortalState(cell) || end {
			state = cell
		}
	})
	return state
}

// LilyPadCell is a block cell holding a lily pad.
type LilyPadCell struct{ X, Y, Z int32 }

// InsideBlocks is the hull's block effects over the cells its box swept
// this tick, from (fx, fy, fz), its position as the tick began (a ridden
// hull's before its driver moved it): it appends to pads[:0] the cells
// holding a lily pad (whose inside shape is the whole cell), leaves the stuck
// multiplier for the next move, and appends to flakes[:0] the Y of each
// powder snow cell when the known movement (moveX, moveZ) is sideways. The
// tick applies its block effects twice, each a snowflake's chance: over the
// moves, then the straight line from the tick's start. An unridden hull's
// known movement is its velocity, a driven one its driver's.
// vanilla: Entity.applyEffectsFromBlocks, AbstractBoat.tick,
// LilyPadBlock.entityInside, ServerPlayer.getKnownMovement
func (b *Boat) InsideBlocks(w World, fx, fy, fz, moveX, moveZ float64, pads []LilyPadCell, flakes []int32) ([]LilyPadCell, []int32) {
	pads, flakes = pads[:0], flakes[:0]
	sideways := moveX != 0 || moveZ != 0
	// A driver's moves are recorded before the tick's start, so its second
	// pass sees only the cells where the hull stands.
	first := bodyMove{fx, fy, fz, b.X, b.Y, b.Z, b.X - fx, b.Z - fz, true}
	second := bodyMove{fx: b.X, fy: b.Y, fz: b.Z, tx: b.X, ty: b.Y, tz: b.Z}
	if b.stepMoved {
		first = b.stepMove
		second = bodyMove{fx: fx, fy: fy, fz: fz, tx: b.X, ty: b.Y, tz: b.Z}
	}
	rel := AABB{-BoatHalfWidth, 0, -BoatHalfWidth, BoatHalfWidth, BoatHeight, BoatHalfWidth}
	for _, m := range [2]bodyMove{first, second} {
		burns, fizzles := vehicleBurnPass(w, m, rel, &b.FireTicks, &b.Dead, b.Hurt, func(x, y, z int32, state uint16, _ int, _ AABB, _, _, _ float64) bool {
			// The first pass breaks a pad, so the second finds air there.
			if state == lilyPadState && !slices.Contains(pads, LilyPadCell{x, y, z}) {
				pads = append(pads, LilyPadCell{x, y, z})
			}
			if insideNonLiving(state, sideways, &b.stuck) {
				flakes = append(flakes, y)
			}
			return true
		})
		b.LavaHurts += burns
		b.Extinguishes += fizzles
	}
	return pads, flakes
}
