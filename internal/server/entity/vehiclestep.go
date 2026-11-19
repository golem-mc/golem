package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// MovementEvent is the vibration a vehicle's move produced, zero for none.
type MovementEvent struct {
	Kind       rules.GameEvent
	X, Y, Z    float64
	BlockState uint16
}

// vehicleEvent implements MovementEmission.EVENTS, without step or swim sounds.
// vanilla: Entity.applyMovementEmissionAndPlaySound, Entity.vibrationAndSoundEffectsFromBlock
func (st *stepState) vehicleEvent(w World, move bodyMove, halfWidth float64, onGround, onRails, inWater bool) MovementEvent {
	dx, dy, dz := move.tx-move.fx, move.ty-move.fy, move.tz-move.fz
	distance := float32(math.Hypot(dx, dz) * stepDistScale)
	var supporting uint16
	looked := false
	if dy != 0 {
		x, y, z := rules.StandingBlock(w, move.tx, move.ty, move.tz, halfWidth)
		supporting, looked = w.BlockAt(x, y, z), true
		if rules.IsStateClimbable(supporting) {
			distance = float32(math.Sqrt(dx*dx+dy*dy+dz*dz) * stepDistScale)
		}
	}
	if !st.advance(distance) {
		return MovementEvent{}
	}
	if !looked {
		x, y, z := rules.StandingBlock(w, move.tx, move.ty, move.tz, halfWidth)
		supporting = w.BlockAt(x, y, z)
	}
	if rules.IsAir(supporting) {
		return MovementEvent{}
	}
	event := MovementEvent{X: move.tx, Y: move.ty, Z: move.tz}
	switch {
	case onGround || onRails || rules.IsStateClimbable(supporting):
		event.Kind, event.BlockState = rules.GameEventStep, supporting
	case inWater:
		event.Kind = rules.GameEventSwim
	default:
		return MovementEvent{}
	}
	st.nextStep = float32(int32(st.moveDist) + 1)
	return event
}

// RiddenMovement reports the vibration from an accepted driver's movement.
func (b *Boat) RiddenMovement(w World, fromX, fromY, fromZ float64) MovementEvent {
	b.updateFluids(w, false)
	return b.stride.vehicleEvent(w, bodyMove{fx: fromX, fy: fromY, fz: fromZ, tx: b.X, ty: b.Y, tz: b.Z}, BoatHalfWidth, b.OnGround, false, b.water.height > 0)
}
