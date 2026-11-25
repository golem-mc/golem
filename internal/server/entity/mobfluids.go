package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The mob's fluid machinery from Entity, Mob and LivingEntity: the base
// tick's fluid interaction, the jump threshold, the liquid jump, and the
// water and lava travel branches.

// mobBox is getBoundingBox: width about the centre, height up from the
// feet.
func (m *Mob) mobBox() AABB {
	if m.Type == TypeShulker {
		return m.shulkerBox()
	}
	hw := m.width() / 2
	return AABB{
		minX: m.X - hw, minY: m.Y, minZ: m.Z - hw,
		maxX: m.X + hw, maxY: m.Y + m.height(), maxZ: m.Z + hw,
	}
}

// InWater is Entity.isInWater (wasTouchingWater from the tick's start): any
// water anywhere in the box, however shallow.
func (m *Mob) InWater() bool { return m.waterFluid.height > 0 }

// EyeInWater is Entity.isEyeInFluid(WATER) from the tick's start.
func (m *Mob) EyeInWater() bool { return m.waterFluid.eyesIn }

// InLava is Entity.isInLava, the same read for the lava.
func (m *Mob) InLava() bool { return m.lavaFluid.height > 0 }

// updateMobFluids is baseTick's updateFluidInteraction: refill the trackers,
// clear the fall in water, and add the current's push to the velocity
// (applyCurrentTo) for every body but water creatures. The edge splash is
// left out; these mobs carry no splash sound. It runs at the top of the
// tick, so goals and jumps see the water as the tick began.
func (m *Mob) updateMobFluids(w World) {
	updateFluid(w, m.mobBox(), m.X, m.Y+m.eyeHeight(), m.Z, true, &m.waterFluid, &m.lavaFluid)
	pushed := !m.kind().water && m.Type != TypeTurtle && m.Type != TypeAxolotl && !IsNautilus(m.Type) && m.Type != TypeFrog
	if m.waterFluid.height > 0 {
		// vanilla: updateFluidInteraction — in water the fall is no fall
		m.FallDistance = 0
		// vanilla: Entity.isPushedByFluid — water creatures and the turtle
		// ignore currents
		if pushed {
			if dx, dy, dz, any := m.waterFluid.currentImpulse(waterCurrent, m.VX, m.VZ); any {
				m.VX += dx
				m.VY += dy
				m.VZ += dz
			}
		}
	}
	if m.lavaFluid.height > 0 {
		// vanilla: Entity.baseTick — a carried fall halves each tick in lava
		m.FallDistance *= 0.5
		scale := lavaCurrent
		if w.FastLava() {
			scale = fastLavaCurrent
		}
		if dx, dy, dz, any := m.lavaFluid.currentImpulse(scale, m.VX, m.VZ); pushed && any {
			m.VX += dx
			m.VY += dy
			m.VZ += dz
		}
	}
}

// fluidJumpThreshold is LivingEntity.getFluidJumpThreshold: 0.4 when the
// eyes are that high, else 0 (a chick finds any water deep).
func (m *Mob) fluidJumpThreshold() float64 {
	if m.Type == TypeSulfurCube {
		return m.height() * 0.2 // vanilla: SulfurCube.getFluidJumpThreshold
	}
	if m.eyeHeight() < 0.4 {
		return 0
	}
	return 0.4
}

// jumpInLiquid is Mob.jumpInLiquid: a body that can float (the float goal
// set it) gets LivingEntity.jumpInLiquid's 0.04 kick; one that cannot gets
// three tenths of a full leap, which lifts a drowning zombie off a pond's
// bottom. Vanilla reads only the navigation flag, not the fluid.
func (m *Mob) jumpInLiquid(fluid fluidKind) {
	if m.Type == TypeMagmaCube && fluid == fluidLava {
		// vanilla: MagmaCube.jumpInLiquid
		m.VY = float64(0.22 + float32(m.Size)*0.05)
		m.Impulse = true
		return
	}
	if m.canFloat {
		m.VY += 0.04
	} else {
		m.VY += 0.3
	}
}

// jumpFromGround is LivingEntity.jumpFromGround: the JUMP_STRENGTH hop
// (0.42), never less than the current rise. factor is the block's jump
// factor, scaling the strength but not the Jump Boost added after.
// vanilla: LivingEntity.getJumpPower
func (m *Mob) jumpFromGround(factor float64) {
	if m.Type == TypeRabbit {
		m.rabbitJump(factor)
		return
	}
	jumpPower := float64(float32(m.attrOr(registry.AttributeJumpStrength, 0.42))*float32(factor) + float32(rules.JumpBoostPower(m.effects.All())))
	if m.isCube() {
		// vanilla: AbstractCubeMob.jumpFromGround, MagmaCube.jumpFromGround
		m.VY = jumpPower + m.cubeJumpBonus()
		m.Impulse = true
		return
	}
	if jumpPower <= 1e-5 {
		return
	}
	if jumpPower > m.VY {
		m.VY = jumpPower
	}
}

// travelInFluid is LivingEntity.travelInFluid's dispatch: water branch or
// lava branch by the trackers. canStandOnFluid is false for every kind
// here, so the trackers alone decide.
func (m *Mob) travelInFluid(w World) {
	isFalling := m.VY <= 0
	oldY := m.Y
	if m.InWater() && m.Swimming {
		m.drownedSwimTravel(w)
	} else if m.InWater() {
		m.travelInWater(w, m.effectiveGravity(), isFalling, oldY)
	} else {
		m.travelInLava(w, m.effectiveGravity(), isFalling, oldY)
	}
	if m.Type == TypeSulfurCube {
		m.sulfurFloat()
	}
}

// waterSlowDown is the kind's horizontal water drag.
func (m *Mob) waterSlowDown() float64 {
	switch m.Type {
	case TypePolarBear:
		return 0.98 // vanilla: PolarBear.getWaterSlowDown
	case TypeSkeletonHorse:
		return 0.96 // vanilla: SkeletonHorse.getWaterSlowDown
	default:
		return defaultWaterSlowDown
	}
}

// travelInWater is LivingEntity.travelInWater without sprinting: kind-specific
// drag and acceleration 0.02, both pulled toward land travel by
// water_movement_efficiency (halved off the ground), and Dolphin's Grace's
// 0.96 drag, in float32 arithmetic to match vanilla's rounding.
// The wanted direction applies before the move (the pace times 0.8 of 98,
// capped at 1); a body climbing against a wall gets the climb before the
// drag.
func (m *Mob) travelInWater(w World, gravity float64, isFalling bool, oldY float64) {
	if m.Type == TypeDolphin {
		m.smoothSwimTravel(w, m.dolphin.speed, m.dolphin.up)
		if m.TargetID == 0 {
			m.VY -= 0.005 // vanilla: Dolphin.travelInWater
		}
		return
	}
	if IsNautilus(m.Type) {
		m.nautilusTravel(w)
		return
	}
	if m.Type == TypeTadpole {
		// vanilla: AbstractFish.travelInWater
		m.smoothSwimTravel(w, 0.01, m.tadpole.up)
		m.VY -= 0.005
		return
	}
	if m.Type == TypeAxolotl {
		m.axolotlTravelInWater(w)
		return
	}
	if m.Type == TypeTurtle {
		m.turtleTravelInWater(w)
		return
	}
	if m.Type == TypeFrog {
		// vanilla: Frog.travelInWater, which has no gravity term
		m.smoothSwimTravel(w, m.frog.speed, m.frog.up)
		return
	}
	slowDown := float32(m.waterSlowDown())
	speed := float32(waterSpeed)
	var walker float32
	if m.attrs != nil {
		walker = float32(m.attrs.Value(registry.AttributeWaterMovementEfficiency))
	}
	if !m.OnGround {
		walker *= 0.5
	}
	if walker > 0 {
		slowDown += (0.54600006 - slowDown) * walker
		speed += (float32(m.walk.speed) - speed) * walker
	}
	if m.hasEffect(registry.MobEffectDolphinsGrace) {
		slowDown = 0.96
	}
	if m.walk.speed > 0 {
		input := math.Min(1, m.walk.speed*0.98)
		m.VX += m.walk.dirX * float64(speed) * input
		m.VZ += m.walk.dirZ * float64(speed) * input
	}
	m.move(w, m.VX, m.VY, m.VZ)
	if m.horizontalCollision && m.onClimbable(w) {
		m.VY = 0.2
	}
	m.VX *= float64(slowDown)
	m.VY *= 0.8
	m.VZ *= float64(slowDown)
	// vanilla: getFluidFallingAdjustedMovement — a sixteenth of gravity off
	// the vertical. Its -0.003 clamp never applies: gravity/16 is exactly
	// the 0.005 the comparison excludes.
	m.VY -= gravity / 16
	m.jumpOutOfFluid(w, oldY)
}

// travelInLava is LivingEntity.travelInLava: the water's acceleration, then
// depth-dependent drag (shallow: 0.8 on the vertical then a sixteenth of the
// fall off; deep: everything halved), then a quarter of gravity down.
func (m *Mob) travelInLava(w World, gravity float64, isFalling bool, oldY float64) {
	if m.walk.speed > 0 {
		input := math.Min(1, m.walk.speed*0.98)
		m.VX += m.walk.dirX * waterSpeed * input
		m.VZ += m.walk.dirZ * waterSpeed * input
	}
	m.move(w, m.VX, m.VY, m.VZ)
	if m.lavaFluid.height <= m.fluidJumpThreshold() {
		m.VX *= 0.5
		m.VY *= 0.8
		m.VZ *= 0.5
		m.VY -= gravity / 16
	} else {
		m.VX *= 0.5
		m.VY *= 0.5
		m.VZ *= 0.5
	}
	if gravity != 0 {
		m.VY -= gravity / 4
	}
	m.jumpOutOfFluid(w, oldY)
}

// jumpOutOfFluid is LivingEntity.jumpOutOfFluid: against a wall with the
// body's height free 0.6 above where the move began, the velocity becomes a
// 0.3 rise, the hop out of a pool. Measuring from the move's start is what
// lets the hop reach the ledge.
func (m *Mob) jumpOutOfFluid(w World, oldY float64) {
	if !m.horizontalCollision {
		return
	}
	rise := m.VY + 0.6 - m.Y + oldY
	if boxFits(w, m.collideCtx(), m.X+m.VX, m.Z+m.VZ, m.Y+rise, m.height(), m.width()/2) {
		m.VY = 0.3
	}
}
