package entity

import (
	"math"
	"math/rand/v2"

	"github.com/golem-mc/golem/internal/server/rules"
)

// StandBody is the shared LivingEntity travel state of armor stands and mannequins.
type StandBody struct {
	Place
	VX, VY, VZ   float64
	OnGround     bool
	FallDistance float64
	// LandedFall is the fall a landing this tick ended, 0 when none.
	LandedFall  float64
	water, lava fluidTracker
	stuck       stuckFactor
}

// Push is Entity.push for an explosion's knockback; the body's tick sends
// it.
// vanilla: Entity.push
func (s *StandBody) Push(vx, vy, vz float64) {
	s.VX += vx
	s.VY += vy
	s.VZ += vz
}

// KnockbackAlong is LivingEntity.knockback along the attacker's yaw: halve
// the old velocity, add the push along the facing, and lift a grounded
// body, capped at 0.4. Bodies have no knockback resistance.
// vanilla: LivingEntity.knockback, ArmorStand.createAttributes
func (s *StandBody) KnockbackAlong(yaw, power float64) {
	if power <= 0 {
		return
	}
	ux, uz := rules.KnockbackFacing(yaw)
	s.knockbackVelocity(ux, uz, power)
}

func (s *StandBody) knockbackVelocity(ux, uz, power float64) {
	s.VX = s.VX/2 + ux*power
	s.VZ = s.VZ/2 + uz*power
	if s.OnGround {
		s.VY = math.Min(0.4, s.VY/2+power)
	}
}

// KnockbackFrom shoves the body away from a source position.
func (s *StandBody) KnockbackFrom(x, z, power float64) {
	if power <= 0 {
		return
	}
	dx, dz := x-s.X, z-s.Z
	for dx*dx+dz*dz < 1e-5 {
		dx = (rand.Float64() - rand.Float64()) * 0.01
		dz = (rand.Float64() - rand.Float64()) * 0.01
	}
	length := math.Hypot(dx, dz)
	s.knockbackVelocity(-dx/length, -dz/length, power)
}

// updateFluids reads the fluids the stand's box stands in and takes the
// current's push onto the velocity, at the water's strength and the lava's.
func (s *StandBody) updateFluids(w World, box AABB, eyeHeight float64) {
	eye := s.Y + eyeHeight
	updateFluid(w, box, s.X, eye, s.Z, true, &s.water, &s.lava)
	if s.water.height > 0 {
		if dx, dy, dz, any := s.water.currentImpulse(waterCurrent, s.VX, s.VZ); any {
			s.VX += dx
			s.VY += dy
			s.VZ += dz
		}
	}
	if s.lava.height > 0 {
		scale := lavaCurrent
		if w.FastLava() {
			scale = fastLavaCurrent
		}
		if dx, dy, dz, any := s.lava.currentImpulse(scale, s.VX, s.VZ); any {
			s.VX += dx
			s.VY += dy
			s.VZ += dz
		}
	}
}

func (s *StandBody) settleVelocity() {
	// A velocity component under 0.003 is zeroed before the walk, so a slow
	// stand settles instead of creeping.
	// vanilla: LivingEntity.aiStep
	if math.Abs(s.VX) < 0.003 {
		s.VX = 0
	}
	if math.Abs(s.VY) < 0.003 {
		s.VY = 0
	}
	if math.Abs(s.VZ) < 0.003 {
		s.VZ = 0
	}
}

// travel moves before applying gravity and drag, returning the landed fall distance.
// vanilla: LivingEntity.travelInAir, travelInWater, travelInLava
func (s *StandBody) travel(w World, box AABB, halfWidth, gravity float64) (landed float64) {
	s.settleVelocity()

	if s.water.height > 0 || s.lava.height > 0 {
		s.OnGround, landed = s.move(w, box, halfWidth, gravity)
		if s.water.height > 0 {
			s.VX *= defaultWaterSlowDown
			s.VZ *= defaultWaterSlowDown
			s.VY *= 0.8
			s.VY -= gravity / 16
		} else {
			if s.lava.height <= lavaShallowDepth {
				s.VX *= 0.5
				s.VZ *= 0.5
				s.VY *= 0.8
				s.VY -= gravity / 16
			} else {
				s.VX *= 0.5
				s.VY *= 0.5
				s.VZ *= 0.5
			}
			s.VY -= gravity / 4
		}
	} else {
		onGround := s.OnGround
		s.OnGround, landed = s.move(w, box, halfWidth, gravity)
		s.VY = (s.VY - gravity) * ArmorStandDrag
		g := ArmorStandAirDrag
		if onGround {
			g *= groundFrictionBelow(w, s.X, s.Y, s.Z, halfWidth)
		}
		s.VX *= g
		s.VZ *= g
	}
	return landed
}

// move moves the stand along its velocity as far as blocks allow, axis by
// axis in vanilla's Y, X, Z order, zeroing stopped axes. It reports whether
// it ended on the ground; a standing body with no velocity keeps its
// ground.
func (s *StandBody) move(w World, box AABB, halfWidth, gravity float64) (bool, float64) {
	onGround := s.OnGround
	// grounded is the actual downward collision; the carried flag (a /tp sets
	// it) must not count as a landing for fall damage.
	// vanilla: Entity.move, Entity.checkFallDamage
	var grounded bool
	ctx, startY := fallCtx(s.FallDistance), s.Y
	dx, dy, dz, stuck := s.stuck.scale(s.VX, s.VY, s.VZ)
	if stuck {
		s.VX, s.VY, s.VZ = 0, 0, 0
	}
	for _, axis := range [...]int{axisY, axisX, axisZ} {
		d := dy
		switch axis {
		case axisX:
			d = dx
		case axisZ:
			d = dz
		}
		if d == 0 {
			continue
		}
		var blocked bool
		box, _, blocked = clipMove(w, ctx, box, axis, d)
		cx, cz := box.centerXZ()
		s.SetPos(cx, box.minY, cz)
		if !blocked {
			if axis == axisY {
				onGround = false
			}
			continue
		}
		switch axis {
		case axisY:
			onGround = d < 0
			grounded = onGround
			if onGround {
				s.VY = bounceOffGround(w, s.X, s.Y, s.Z, halfWidth, s.VY, s.Y-startY, gravity, airDragFloat, true)
			} else {
				s.VY = 0
			}
		case axisX:
			s.VX = 0
		case axisZ:
			s.VZ = 0
		}
	}
	f := blockSpeedFactor(w, s.X, s.Y, s.Z)
	s.VX *= f
	s.VZ *= f
	s.stuck = stuckIn(w, box, s.X, s.Y, s.Z, true, false, false, false)
	return onGround, bankFall(&s.FallDistance, startY, s.Y, grounded)
}
