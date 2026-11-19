package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The flying move control, used instead of walk physics: a flying kind moves
// by its control's drive and the fluid's drag, with no gravity. The
// ghastling runs FlyingMoveControl (the full speed factor along all three
// axes each tick, NoGravity set; brain approximated by flyingBabyGoals).
// The grown happy ghast runs Ghast.GhastMoveControl (an impulse of flying
// speed every two to six ticks, dropped when the point is unreachable) with
// HappyGhast.registerGoals' float, tempt and wander. Both turn toward the
// point; vanilla's grown body faces its velocity, approximated as the point.
// vanilla: FlyingMoveControl.tick, Ghast.GhastMoveControl.tick, HappyGhast.travel

// The flying drive: the control's pace is the speed factor times
// FLYING_SPEED (the kind's; the ghast's movement_speed is the same 0.05),
// and HappyGhast.travel drives at 5/3 of that.
// vanilla: FlyingMoveControl.tick
const (
	flySpeedRatio = 5.0 / 3.0
	// flyYawTurn is the control's yaw turn per tick; pitch turns by the
	// ghastling FlyingMoveControl's whole maxTurn.
	flyYawTurn   = 90
	flyPitchTurn = 180
	// flyArriveSqr is FlyingMoveControl.tick's squared arrival distance.
	flyArriveSqr = 2.500000277905201e-7
	// flyOrderMin and flyOrderSpread bound the ticks a ghastling keeps one
	// order: 150 to 250 inclusive. vanilla: Behavior.tryStart
	flyOrderMin    = 150
	flyOrderSpread = 101
	// flyWaterDrag, flyLavaDrag and flyAirDrag are travelFlying's velocity
	// scales: the fluid's, or the air's 0.91.
	flyWaterDrag = 0.8
	flyLavaDrag  = 0.5
	flyAirDrag   = 0.91
	// flyWanderDistance and flyTargetAttempts are RandomFloatAroundGoal's: the
	// happy ghast's distanceToBlocks of 16, and 64 draws before settling for the
	// last.
	flyWanderDistance = 16
	flyTargetAttempts = 64
	// flyWanderPace is RandomFloatAroundGoal.start's full pace.
	flyWanderPace = 1.0
	// flyCorridorStep is the half-block spacing used to approximate
	// Ghast.GhastMoveControl.canReach's block sweep, so no crossed cell is
	// skipped.
	flyCorridorStep = 0.5
)

// flyAvoidBlocks is the happy_ghast_avoids tag, which the grown body's reach
// test will not press through.
var flyAvoidBlocks = registry.BlockTagOf("minecraft:happy_ghast_avoids")

// flyDirections is the six Direction.values() isGoodTarget walks.
var flyDirections = [6]struct{ dx, dy, dz int32 }{
	{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0},
}

// flyingWander is Ghast.RandomFloatAroundGoal: a point up to 16 blocks out on
// each axis, in air, with a solid block within 15 along one of the six
// directions, flown straight at. It is one-shot and re-picks only once the
// body is within a block, the point is over 60 blocks off, or the control
// dropped it (unreachable for the grown body, arrived or timed out for the
// ghastling); that drop keeps a walled-off body from pressing forever.
type flyingWander struct {
	m *Mob
	// priority replaces the stroll priority where nonzero.
	priority int
	// anywhere drops the solid-block requirement: the hostile ghast's
	// distanceToBlocks is zero.
	anywhere bool
}

func (g *flyingWander) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityStroll
}
func (g *flyingWander) Flags() GoalFlags {
	return GoalMove
}

func (g *flyingWander) CanStart(*tickView) bool {
	o := g.m.navOf()
	if !o.nav.flyActive {
		return true
	}
	dx := o.nav.flyX - g.m.X
	dy := o.nav.flyY - g.m.Y
	dz := o.nav.flyZ - g.m.Z
	dd := dx*dx + dy*dy + dz*dz
	return dd < 1 || dd > 3600
}

func (g *flyingWander) Start(t *tickView) {
	x, y, z := g.m.flyTarget(t.w, g.anywhere)
	g.m.seekToFly(x, y, z, flyWanderPace)
}

func (g *flyingWander) ShouldContinue(*tickView) bool { return false }
func (g *flyingWander) Tick(*tickView)                {}
func (g *flyingWander) Stop(*tickView)                {}

func (g *flyingWander) RequiresEveryTick() bool { return false }

// flyTarget is RandomFloatAroundGoal.getSuitableFlyToPosition at
// distanceToBlocks 16 (happy ghast) or 0 (hostile ghast). Draws outside the
// home are dropped; if all are, a fresh one stands. A fallback above the
// terrain is mirrored below the body's height, so the flight dips toward
// the ground.
// vanilla: Ghast.RandomFloatAroundGoal
func (m *Mob) flyTarget(w World, anywhere bool) (float64, float64, float64) {
	var x, y, z float64
	drawn := false
	for range flyTargetAttempts {
		x = m.X + (m.Rand.Float64()*2-1)*flyWanderDistance
		y = m.Y + (m.Rand.Float64()*2-1)*flyWanderDistance
		z = m.Z + (m.Rand.Float64()*2-1)*flyWanderDistance
		if m.home.set && !m.withinHomeAt(x, y, z) {
			drawn = false
			continue
		}
		drawn = true
		if anywhere || flyTargetGood(w, x, y, z) {
			return x, y, z
		}
	}
	if !drawn {
		x = m.X + (m.Rand.Float64()*2-1)*flyWanderDistance
		y = m.Y + (m.Rand.Float64()*2-1)*flyWanderDistance
		z = m.Z + (m.Rand.Float64()*2-1)*flyWanderDistance
	}
	// vanilla: getHeight is the first free y, one above the highest block
	cell := cellAt(x, y, z)
	if height := w.MotionBlockingHeight(cell.X, cell.Z) + 1; height < cell.Y && height > w.MinY() {
		y = m.Y - math.Abs(m.Y-y)
	}
	return x, y, z
}

// flyTargetGood is RandomFloatAroundGoal.isGoodTarget: the point's cell is
// air and a non-air block lies within the distance along one of the six
// directions.
func flyTargetGood(w World, x, y, z float64) bool {
	cell := cellAt(x, y, z)
	if !flyCellAir(w, cell.X, cell.Y, cell.Z) {
		return false
	}
	for _, d := range flyDirections {
		for i := int32(1); i < flyWanderDistance; i++ {
			if !flyCellAir(w, cell.X+d.dx*i, cell.Y+d.dy*i, cell.Z+d.dz*i) {
				return true
			}
		}
	}
	return false
}

// flyCellAir is vanilla BlockState.isAir through this package's block read.
func flyCellAir(w World, x, y, z int32) bool {
	return world.IsAirState(w.BlockAt(x, y, z))
}

// seekToFly sets the flight's point and pace, as the flying navigation's
// moveTo(Entity) holds a target between the goal's asks.
func (m *Mob) seekToFly(x, y, z, speedFactor float64) {
	o := m.navOf()
	o.nav.speedFactor = speedFactor
	if !o.nav.flyActive {
		o.nav.flyTimeout = flyOrderMin + m.Rand.Intn(flyOrderSpread)
	}
	o.nav.flyActive = true
	o.nav.flyX, o.nav.flyY, o.nav.flyZ = x, y, z
}

// flyInput is LivingEntity.travel's input from the control's speed: the
// factor's pace forward, rising or falling by the vertical sign. Combined
// input above unit length is normalized before scaling by the travel speed.
// vanilla: MoveControl.setSpeed, Entity.getInputVector, LivingEntity.travel
func (m *Mob) flyInput(speedFactor, dy float64) {
	pace := speedFactor * m.kind().speed
	air := m.kind().speed * flySpeedRatio
	if m.Type == TypeBee || m.Type == TypeParrot {
		// vanilla: Bee has no travel override; LivingEntity.travelInAir
		// accelerates by getFlyingSpeed() (0.02) and FLYING_SPEED sets the input
		pace = speedFactor * m.attrOr(registry.AttributeFlyingSpeed, flyingSpeedBase(m.Type))
		air = beeAirAcceleration
	}
	if m.Type == TypeAllay {
		// vanilla: Allay.travel is travelFlying(input, getSpeed()), the speed the
		// move control set
		pace = speedFactor * m.attrOr(registry.AttributeFlyingSpeed, flyingSpeedBase(m.Type))
		air = pace
	}
	// FlyingMoveControl.tick rises only for a point above; level is a fall.
	rise := -pace
	if dy > 0 {
		rise = pace
	}
	if length := math.Hypot(pace, rise); length > 1 {
		pace /= length
		rise /= length
	}
	sin, cos := math.Sincos(float64(m.Yaw) * math.Pi / 180)
	m.VX += -pace * sin * air
	m.VZ += pace * cos * air
	m.VY += rise * air
}

// flyMoveStep is the flyer's travel instead of walk physics: the move
// control drives, the jump block spends the float goal's ask, then the
// velocity is carried and dragged by the fluid's scale. No gravity and no
// banked fall. The control is the ghastling's FlyingMoveControl or the grown
// body's GhastMoveControl.
// vanilla: FlyingMoveControl.tick, Ghast.GhastMoveControl.tick, LivingEntity.travelFlying
func (m *Mob) flyMoveStep(t *tickView) {
	o := m.navOf()
	if m.kind().flyPath != nil {
		m.flyFollowPath()
	}
	if m.IsBaby() || m.Type == TypeBee || m.Type == TypeAllay || m.Type == TypeWither {
		m.flyBabyControl(&o.nav)
	} else {
		m.flyGhastControl(&o.nav, t.w)
	}
	// The jump block runs between control and travel, so a flyer in deep
	// water rises on the float goal's ask.
	m.jumpStep(t.w)
	m.move(t.w, m.VX, m.VY, m.VZ)
	m.jump = false
	switch {
	case m.InWater():
		m.VX, m.VY, m.VZ = m.VX*flyWaterDrag, m.VY*flyWaterDrag, m.VZ*flyWaterDrag
	case m.InLava():
		m.VX, m.VY, m.VZ = m.VX*flyLavaDrag, m.VY*flyLavaDrag, m.VZ*flyLavaDrag
	case m.Type == TypeWither && m.gravity() != 0:
		// vanilla: LivingEntity.travelInAir (WitherBoss hoversInPlace=false; the others fly without gravity)
		m.VX, m.VY, m.VZ = m.VX*flyAirDrag, (m.VY-m.effectiveGravity())*verticalDrag, m.VZ*flyAirDrag
	default:
		m.VX, m.VY, m.VZ = m.VX*flyAirDrag, m.VY*flyAirDrag, m.VZ*flyAirDrag
	}
}

// flyBabyControl is FlyingMoveControl.tick. Vanilla resets the operation to
// WAIT every tick but the flying navigation re-sets it each tick of a
// standing order, so here the order stands until the body is in the point's
// block (not while a path's waypoint is the point), its 150-250 ticks run
// out, or a goal drops it. It sets NoGravity
// and, if not there yet, turns toward the point and presses the full speed
// factor along all three axes. NoGravity outlives the drive (hoversInPlace),
// so the ghastling never falls again.
// vanilla: MoveToTargetSink.tick
// vanilla: FlyingMoveControl.tick
func (m *Mob) flyBabyControl(o *mobNav) {
	if !o.flyActive {
		if m.Type == TypeWither {
			// vanilla: WitherBoss (FlyingMoveControl hoversInPlace=false)
			m.SetMetadataNoGravity(false)
		}
		return
	}
	m.SetMetadataNoGravity(true)
	dx := o.flyX - m.X
	dy := o.flyY - m.Y
	dz := o.flyZ - m.Z
	o.flyTimeout--
	if o.flyTimeout <= 0 || dx*dx+dy*dy+dz*dz < flyArriveSqr ||
		len(o.path) == 0 && cellAt(m.X, m.Y, m.Z) == cellAt(o.flyX, o.flyY, o.flyZ) {
		o.flyActive = false
		return
	}
	m.Yaw = turnToward(m.Yaw, float32(yawToward(dx, dz)), flyYawTurn)
	pitchTurn := float64(flyPitchTurn)
	if m.Type == TypeBee || m.Type == TypeAllay {
		// vanilla: FlyingMoveControl(this, 20, true)
		pitchTurn = beePitchTurn
	}
	if m.Type == TypeParrot {
		pitchTurn = parrotPitchTurn
	}
	m.Pitch = turnToward(m.Pitch, float32(pitchToward(dx, dy, dz)), pitchTurn)
	m.flyInput(o.speedFactor, dy)
}

// flyGhastControl is Ghast.GhastMoveControl.tick: at most one impulse every
// two to six ticks, the whole flying speed (5/3 of the attribute) toward the
// point, and if canReach refuses the point is dropped so the next selection
// picks another. The count runs every tick regardless. The goal's speed
// factor is never read.
// vanilla: Ghast.GhastMoveControl.tick
func (m *Mob) flyGhastControl(o *mobNav, w World) {
	// vanilla: Ghast.GhastMoveControl's shouldBeStopped drops the order
	if m.stillTimeout > 0 {
		o.flyActive = false
	}
	if !o.flyActive {
		return
	}
	dx := o.flyX - m.X
	dy := o.flyY - m.Y
	dz := o.flyZ - m.Z
	// The count is read before it is spent, vanilla's floatDuration-- <= 0.
	old := o.flyFloatDuration
	o.flyFloatDuration--
	if old <= 0 {
		o.flyFloatDuration += m.Rand.Intn(5) + 2
		if !m.flyCanReach(w, dx, dy, dz) {
			o.flyActive = false
			return
		}
		if length := math.Sqrt(dx*dx + dy*dy + dz*dz); length > 1e-7 {
			impulse := m.kind().speed * flySpeedRatio
			m.VX += dx / length * impulse
			m.VY += dy / length * impulse
			m.VZ += dz / length * impulse
		}
	}
	if m.Type != TypeGhast {
		// The hostile ghast's look goal faces it.
		m.Yaw = turnToward(m.Yaw, float32(yawToward(dx, dz)), flyYawTurn)
	}
}

// flyCanReach is Ghast.GhastMoveControl.canReach: the box at the destination
// of the whole move is clear, as is each box along the way. The way is a
// walk of boxes approximating vanilla's block sweep.
// vanilla: Ghast.GhastMoveControl.canReach
func (m *Mob) flyCanReach(w World, dx, dy, dz float64) bool {
	box := m.mobBox()
	dest := box.moved(dx, dy, dz)
	// Vanilla's careful pass grows the destination by one each side, which
	// also rejects points too near rock or water, refusing fluids even for a
	// swimmer.
	careful := m.Type != TypeGhast
	inflated := AABB{dest.minX - 1, dest.minY - 1, dest.minZ - 1, dest.maxX + 1, dest.maxY + 1, dest.maxZ + 1}
	if careful && !m.flyBoxClear(w, inflated, false, false, careful) {
		return false
	}
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
	steps := int(math.Ceil(distance / flyCorridorStep))
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		if !m.flyBoxClear(w, box.moved(dx*f, dy*f, dz*f), m.InWater(), m.InLava(), careful) {
			return false
		}
	}
	return true
}

// flyBoxClear is blockTraversalPossible over BlockPos.betweenClosed: every
// cell is air, or is not in the avoid tag, not a fluid (unless canWater or
// canLava allows it), and has empty collision (the plants a ghast drifts
// over).
func (m *Mob) flyBoxClear(w World, box AABB, canWater, canLava, careful bool) bool {
	for x := int32(math.Floor(box.minX)); x <= int32(math.Floor(box.maxX)); x++ {
		for y := int32(math.Floor(box.minY)); y <= int32(math.Floor(box.maxY)); y++ {
			for z := int32(math.Floor(box.minZ)); z <= int32(math.Floor(box.maxZ)); z++ {
				if !flyCellPassable(w, x, y, z, canWater, canLava, careful) {
					return false
				}
			}
		}
	}
	return true
}

// flyCellPassable is one cell of flyBoxClear; a non-careful body (the
// hostile ghast) is stopped by collision alone.
func flyCellPassable(w World, x, y, z int32, canWater, canLava, careful bool) bool {
	state := w.BlockAt(x, y, z)
	if world.IsAirState(state) {
		return true
	}
	if !careful {
		return len(blockBoxes(state)) == 0
	}
	if registry.BlockOf(state).InTag(flyAvoidBlocks) {
		return false
	}
	switch fluidCellOf(state).kind {
	case fluidWater:
		if !canWater {
			return false
		}
	case fluidLava:
		if !canLava {
			return false
		}
	}
	return len(blockBoxes(state)) == 0
}
