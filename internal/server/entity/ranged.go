package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The skeleton's ranged attack, RangedBowAttackGoal from the bytecode:
// walk toward the target while unseen or beyond the attack radius; within
// it, after twenty ticks of sight, stand and strafe while drawing. The
// arrow is aimed as AbstractSkeleton.performRangedAttack does, at power 1.6
// with difficulty-based inaccuracy.

// AbstractSkeleton's numbers: attack radius 15 (RangedBowAttackGoal's
// argument, squared by the goal), walk pace 1.0 (speedModifier), a 20-tick
// draw (tick's i >= 20), and reassessWeaponGoal's intervals: 40 ticks
// (getAttackInterval), 20 on hard (getHardAttackInterval).
const (
	rangedAttackRadiusSq = 15.0 * 15.0
	rangedWalkFactor     = 1.0
	rangedDrawTicks      = 20
	rangedInterval       = 40
	rangedIntervalHard   = 20

	// The strafe's own numbers, from RangedBowAttackGoal.tick.
	strafeRethinkTicks    = 20
	strafeFlipChance      = 0.3
	strafeBackOffFraction = 0.75
	strafePressFraction   = 0.25

	// The strafe's two halves, vanilla's move control's own: half a speed
	// forwards and half a speed sideways.
	strafeForwardShare = 0.5
	strafeSideShare    = 0.5

	// The 20 ticks of seeTime before strafing, and the -60 past which an
	// unseen draw is stopped.
	rangedSeeTicks  = 20
	rangedSeeGiveUp = -60

	// The arrow's flight, from AbstractSkeleton.performRangedAttack.
	rangedArrowPower                   = 1.6
	rangedArrowAimLift                 = 0.20000000298023224
	rangedArrowInaccuracy              = 14.0
	rangedInaccuracyPerDifficultyLevel = 4.0
)

// rangedAttackGoal is RangedBowAttackGoal(this, 1.0, interval, 15.0F) at
// priority 4, the interval set from the difficulty by reassessWeaponGoal.
// It holds MOVE and LOOK, and the aggressive bit stays up for the hunt.
type rangedAttackGoal struct {
	m *Mob
	// priority is the goal's own vanilla registration, the skeleton's four.
	priority int
	// walk is the walk pace factor (speedModifier); zero means the
	// skeleton's rangedWalkFactor.
	walk float64
	// attackTime is the countdown to the next draw, vanilla's attackTime,
	// which starts at minus one and runs only while the bow is not drawn.
	attackTime int
	// seeTime is vanilla's seeTime, counting seen ticks up and unseen ticks
	// down to -60, where the bow is relaxed.
	seeTime int
	// drawn is the ticks the bow has been drawn for, the count vanilla's
	// getTicksUsingItem answers.
	drawn int

	strafingClockwise bool
	strafingBackwards bool
	strafingTime      int
}

func (g *rangedAttackGoal) Priority() int    { return g.priority }
func (g *rangedAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

// CanStart is RangedBowAttackGoal.canUse: a target named and a bow held.
func (g *rangedAttackGoal) CanStart(*tickView) bool {
	return g.m.TargetID != 0 && g.m.holdsBow()
}

func isHeldWeapon(stack protocol.ItemStack, weapon registry.Item) bool {
	return registry.Item(stack.ItemID) == weapon && !stack.Empty()
}

// vanilla: ProjectileUtil.getWeaponHoldingHand
func (m *Mob) weaponHoldingSlot(weapon registry.Item) int {
	if isHeldWeapon(m.MainHand, weapon) {
		return protocol.EquipMainHand
	}
	return protocol.EquipOffHand
}

// startUsingWeapon starts a use and returns the selected equipment slot.
// vanilla: LivingEntity.startUsingItem
func (m *Mob) startUsingWeapon(weapon registry.Item) int {
	slot := m.weaponHoldingSlot(weapon)
	m.SetMetadataUsingItem(true)
	m.SetMetadataUseOffHand(slot == protocol.EquipOffHand)
	return slot
}

// holdsBow is LivingEntity.isHolding(Items.BOW), in either hand.
func (m *Mob) holdsBow() bool {
	return isHeldWeapon(m.MainHand, registry.ItemBow) || isHeldWeapon(m.OffHand, registry.ItemBow)
}

func (g *rangedAttackGoal) Start(*tickView) {
	g.m.SetMetadataAggressive(true)
}

// ShouldContinue is RangedBowAttackGoal.canContinueToUse: a target or an
// unfinished walk, with the bow still in hand.
func (g *rangedAttackGoal) ShouldContinue(t *tickView) bool {
	return (g.m.TargetID != 0 || len(g.m.navOf().nav.path) > 0) && g.m.holdsBow()
}

// Stop is RangedBowAttackGoal.stop: arms down, sight count and attack
// countdown reset, bow relaxed with no arrow.
func (g *rangedAttackGoal) Stop(*tickView) {
	g.m.SetMetadataAggressive(false)
	g.seeTime = 0
	g.attackTime = -1
	g.m.SetMetadataUsingItem(false)
	g.m.stopSeeking()
}

// Tick is RangedBowAttackGoal.tick: walk or strafe, keep the head on the
// target, and draw and loose the bow.
func (g *rangedAttackGoal) Tick(t *tickView) {
	p := g.m.targetIn(t)
	if p == nil {
		return
	}
	// Feet-to-feet squared distance.
	// vanilla: distanceToSqr
	dx, dy, dz := p.X-g.m.X, p.Y-g.m.Y, p.Z-g.m.Z
	distanceSq := dx*dx + dy*dy + dz*dz
	seen := g.m.seesTarget(t.w, p)

	// The sight count: a change between seen and unseen wipes it.
	if (g.seeTime > 0) != seen {
		g.seeTime = 0
	}
	if seen {
		g.seeTime++
	} else {
		g.seeTime--
	}

	if distanceSq <= rangedAttackRadiusSq && g.seeTime >= rangedSeeTicks {
		g.m.stopSeeking()
		g.strafingTime++
	} else {
		walk := g.walk
		if walk == 0 {
			walk = rangedWalkFactor
		}
		g.m.seekTo(t.w, p.X, p.Y, p.Z, walk, navReach)
		g.strafingTime = -1
	}

	// The strafe's own dice: every twenty ticks of it, a three-in-ten roll
	// each for turning the circle and for turning the walk around.
	if g.strafingTime >= strafeRethinkTicks {
		if g.m.Rand.Float64() < strafeFlipChance {
			g.strafingClockwise = !g.strafingClockwise
		}
		if g.m.Rand.Float64() < strafeFlipChance {
			g.strafingBackwards = !g.strafingBackwards
		}
		g.strafingTime = 0
	}

	// The head tracks the target whatever the legs do, clamped to 30 degrees
	// a tick.
	// vanilla: LookControl.setLookAt
	gdx, gdz := p.X-g.m.X, p.Z-g.m.Z
	yaw := float32(yawToward(gdx, gdz))
	// Straight overhead there is no horizontal direction and getYRotD is
	// empty, so the head yaw is left alone rather than set to atan2's zero.
	if gdx != 0 || gdz != 0 {
		g.m.HeadYaw = turnToward(g.m.HeadYaw, yaw, headTurnSpeed)
	}
	g.m.Pitch = turnToward(g.m.Pitch,
		float32(pitchToward(gdx, p.Y+p.eyeHeight()-(g.m.Y+g.m.eyeHeight()), gdz)), headTurnSpeed)

	if g.strafingTime > -1 {
		if distanceSq > rangedAttackRadiusSq*strafeBackOffFraction {
			g.strafingBackwards = false
		} else if distanceSq < rangedAttackRadiusSq*strafePressFraction {
			g.strafingBackwards = true
		}
		g.strafe(t.w, g.strafingBackwards, g.strafingClockwise)

		// The strafe does not turn the body, so turn the controlled mount toward the
		// target (not the head, which could send it the wrong way for a few
		// ticks): mount first, then rider, as vanilla's two lookAt calls do. The
		// strafe is rotated by the mount's yaw, so an unturned mount circles on
		// a stale heading. Unlike the head, Mob.lookAt has no empty-direction
		// guard, and gives -90 rather than yawToward's 0 straight overhead.
		// vanilla: RangedBowAttackGoal.tick, Mob.lookAt
		bodyYaw := yaw
		if gdx == 0 && gdz == 0 {
			bodyYaw = -90
		}
		if v := g.m.Vehicle; v != nil && v.controllingPassenger() == g.m {
			v.Yaw = turnToward(v.Yaw, bodyYaw, headTurnSpeed)
		}
		g.m.Yaw = turnToward(g.m.Yaw, bodyYaw, headTurnSpeed)
	}

	// While drawn: 60 unseen ticks relax the bow; a seen target with 20 ticks
	// drawn gets the arrow. The draw keeps counting while hidden, so a shot
	// past 20 waits only for sight. While not drawn, the next-draw countdown
	// runs.
	if g.m.UsingItem {
		if !seen && g.seeTime < rangedSeeGiveUp {
			g.m.SetMetadataUsingItem(false)
			g.drawn = 0
		} else {
			g.drawn++
			if seen && g.drawn >= rangedDrawTicks {
				g.m.SetMetadataUsingItem(false)
				g.drawn = 0
				g.shoot(t, p)
				g.attackTime = g.m.rangedIntervalFor(t.w.Difficulty())
			}
		}
	} else {
		g.attackTime--
		if g.attackTime <= 0 && g.seeTime >= rangedSeeGiveUp {
			g.m.startUsingWeapon(registry.ItemBow)
			g.drawn = 0
		}
	}
}

// arrowAim is a mob arrow's origin (the eyes less 0.1) and its aim at a
// third of the target's height (Height, or a player's when unset), lifted by lift per block of horizontal
// distance.
func arrowAim(m *Mob, p *Player, lift float64) (ox, oy, oz, ax, ay, az float64) {
	ox, oy, oz = m.X, m.Y+m.eyeHeight()-ArrowSpawnBelow, m.Z
	_, h, _ := p.box()
	ax, ay, az = p.X-ox, p.Y+h/3-oy, p.Z-oz
	ay += math.Sqrt(ax*ax+az*az) * lift
	return
}

// shoot is AbstractSkeleton.performRangedAttack: from the eyes less 0.1,
// aimed at a third of the target's height with lift for the fall, at fixed
// power with the difficulty's inaccuracy and a mob arrow's base damage. The
// event carries it all; the server creates the arrow.
func (g *rangedAttackGoal) shoot(t *tickView, p *Player) {
	m := g.m
	originX, originY, originZ, aimX, aimY, aimZ := arrowAim(m, p, rangedArrowAimLift)
	inaccuracy := rangedArrowInaccuracy - rangedInaccuracyPerDifficultyLevel*float64(t.w.Difficulty())
	vx, vy, vz := ShootVelocity(aimX, aimY, aimZ, rangedArrowPower, inaccuracy, m.Rand)
	damage := MobArrowDamage(1.0, t.w.Difficulty(), m.Rand)
	pitch := float32(1 / (m.Rand.Float32()*0.4 + 0.8)) // the sound's pitch, rolled last
	t.events = append(t.events, MobEvent{
		Mob:  m,
		Kind: MobShotArrow,
		X:    originX, Y: originY, Z: originZ,
		VX: vx, VY: vy, VZ: vz,
		Damage: damage,
		Pitch:  pitch,
		Player: *p,
		Effect: m.kind().arrowEffect,
	})
}

// strafe sets the move control's sideways walk: forward and sideways shares
// rotated by the yaw, at their combined diagonal pace. It lands on the body
// whose move control the rider holds (the base mount), for that body's next
// move.
func (g *rangedAttackGoal) strafe(w World, backwards, clockwise bool) {
	forwards := strafeForwardShare
	if backwards {
		forwards = -forwards
	}
	side := -strafeSideShare
	if clockwise {
		side = strafeSideShare
	}
	g.m.navOf().strafeInput(w, forwards, side)
}

// strafeInput sets the rider's strafe walk, falling back to forwards at the
// modified speed when the probe cell is not walkable.
// vanilla: MoveControl.strafe, MoveControl.tick (STRAFE)
func (m *Mob) strafeInput(w World, forwards, right float64) {
	yaw := float64(m.Yaw) * math.Pi / 180
	sin, cos := math.Sin(yaw), math.Cos(yaw)
	pace := m.pace()
	modifiedSpeed := pace * 0.25 // vanilla: MoveControl.strafe's speedModifier
	length := math.Hypot(forwards, right)
	if length < 1e-9 {
		return
	}
	dirX, dirZ := (right*cos-forwards*sin)/length, (forwards*cos+right*sin)/length
	speed := pace * length
	// Vanilla probes a different direction from moveRelative's actual step.
	// Inputs below unit length are not normalised for this probe.
	scale := modifiedSpeed / max(1, length)
	xa, za := forwards*scale, right*scale
	dx, dz := xa*cos-za*sin, za*cos+xa*sin
	if !m.walkableCell(w, m.X+dx, m.Z+dz) {
		dirX, dirZ, speed = -sin, cos, modifiedSpeed
	}
	m.riderWalk.dirX, m.riderWalk.dirZ = dirX, dirZ
	m.riderWalk.speed = speed
	m.riderWalk.set = true
}

// rangedIntervalFor is reassessWeaponGoal's interval: 20 on hard, 40
// otherwise, or the kind's own pair.
func (m *Mob) rangedIntervalFor(difficulty uint8) int {
	k := m.kind()
	if difficulty == rules.DifficultyHard {
		if k.rangedIntervalHard > 0 {
			return k.rangedIntervalHard
		}
		return rangedIntervalHard
	}
	if k.rangedInterval > 0 {
		return k.rangedInterval
	}
	return rangedInterval
}

// RequiresEveryTick is RangedBowAttackGoal's own override: the draw, the
// sight count and the strafe's clock are ticks of the hunt.
func (g *rangedAttackGoal) RequiresEveryTick() bool { return true }
