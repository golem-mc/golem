package entity

import (
	"github.com/golem-mc/golem/registry"

	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// The ghast: a fire-immune flyer on the happy ghast's move control that
// faces its target and fires a large fireball after a charge. Only a
// player-reflected large fireball kills it.
// vanilla: Ghast

// TypeGhast is the ghast.
const TypeGhast = registry.EntityTypeGhast

func init() {
	// vanilla: EntityTypes.GHAST and Ghast.createAttributes: a 4 by 4 box with
	// eyes 2.6 up, ten health, a follow range of 100 and the 0.06 flying speed.
	kinds[TypeGhast] = &kind{
		name: "Ghast", width: 4, height: 4, eyeHeight: 2.6,
		maxHealth: 10, speed: 0.06, hostile: true, followRange: 100,
		fireImmune: true, flying: true, volume: 5.0,
		sounds: MobSounds{
			Ambient: registry.SoundEntityGhastAmbient,
			Hurt:    registry.SoundEntityGhastHurt,
			Death:   registry.SoundEntityGhastDeath,
		},
	}
	// vanilla: EntityTypes.GHAST clientTrackingRange(10)
	mobTracking[TypeGhast] = tracking{rangeChunks: 10, updateInterval: 3}
}

// The ghast's numbers.
// vanilla: Ghast, Ghast.GhastShootFireballGoal
const (
	ghastDefaultPower   = 1  // Ghast.DEFAULT_EXPLOSION_POWER
	ghastWarnTick       = 10 // chargeTime of the warning sound
	ghastShootTick      = 20 // chargeTime of the launch
	ghastCooldown       = -40
	ghastSightSq        = 4096.0 // 64 blocks
	ghastMuzzle         = 4.0    // how far ahead of the body the fireball is born
	ghastTargetBand     = 4.0    // |target y - ghast y| the target selector allows
	ghastTargetPriority = 1      // priority of the selector
	ghastFloatPrio      = 5
	ghastLookPrio       = 7
)

// ReflectedFireballKills reports whether a large fireball a player owns
// kills this mob outright.
// vanilla: Ghast.isReflectedFireball, Ghast.hurtServer
func (m *Mob) ReflectedFireballKills() bool { return m.Type == TypeGhast }

// ReflectedFireballDamage is the damage a player's large fireball deals a
// ghast.
// vanilla: Ghast.hurtServer
const ReflectedFireballDamage = 1000.0

// HurtByReflectedFireball is a large fireball a player owns striking the
// ghast: the blow is 1000 and passes the fire immunity the type has.
// vanilla: Ghast.isInvulnerableTo, Ghast.hurtServer
func (m *Mob) HurtByReflectedFireball(flightVX, flightVZ float64, shooterID int32, creative bool) (landed, softened bool) {
	m.reflected = true
	defer func() { m.reflected = false }()
	return m.HurtByProjectile(ReflectedFireballDamage, rules.DamageTypeFireball, flightVX, flightVZ, shooterID, true, creative)
}

// ghastState is what a ghast keeps beyond the common mob.
type ghastState struct {
	// power is the explosion power of the fireballs it shoots.
	power int
	// charging is DATA_IS_CHARGING, set while the shoot goal's chargeTime is
	// past ten.
	charging bool
}

// IsGhast reports whether the mob is a ghast.
func (m *Mob) IsGhast() bool { return m.Type == TypeGhast }

// GhastCharging is the ghast's DATA_IS_CHARGING flag.
func (m *Mob) GhastCharging() bool { return m.ghast.charging }

// GhastExplosionPower is the blast power of the ghast's fireballs.
func (m *Mob) GhastExplosionPower() int { return m.ghast.power }

// SetGhastExplosionPower sets the blast power of the ghast's fireballs.
func (m *Mob) SetGhastExplosionPower(power int) { m.ghast.power = power }

// ghastGoals is Ghast.registerGoals: the float at 5, the look and the
// shooting at 7, and the nearest player within four blocks of height at 1.
func ghastGoals(m *Mob) []Goal {
	m.ghast.power = ghastDefaultPower
	return []Goal{
		&nearestTargetGoal{m: m, priority: ghastTargetPriority, accept: func(_ *tickView, p *Player) bool {
			return math.Abs(p.Y-m.Y) <= ghastTargetBand
		}},
		&flyingWander{m: m, priority: ghastFloatPrio, anywhere: true},
		&ghastLookGoal{m: m},
		&ghastShootGoal{m: m},
	}
}

// ghastHeadTurnSpeed is the degrees a tick the head follows the body.
const ghastHeadTurnSpeed = 10

// ghastLookGoal is Ghast.GhastLookGoal: the body faces its target within 64
// blocks, else the way it moves.
// vanilla: Ghast.faceMovementDirection
type ghastLookGoal struct{ m *Mob }

func (g *ghastLookGoal) Priority() int                 { return ghastLookPrio }
func (g *ghastLookGoal) Flags() GoalFlags              { return GoalLook }
func (g *ghastLookGoal) CanStart(*tickView) bool       { return true }
func (g *ghastLookGoal) ShouldContinue(*tickView) bool { return true }
func (g *ghastLookGoal) Start(*tickView)               {}
func (g *ghastLookGoal) Stop(*tickView)                {}
func (g *ghastLookGoal) RequiresEveryTick() bool       { return true }

func (g *ghastLookGoal) Tick(t *tickView) {
	m := g.m
	dx, dz := m.VX, m.VZ
	if p := m.targetIn(t); p != nil {
		if Dist2(p.X, p.Y, p.Z, m.X, m.Y, m.Z) >= ghastSightSq {
			return
		}
		dx, dz = p.X-m.X, p.Z-m.Z
	}
	m.Yaw = float32(yawToward(dx, dz))
	// The look control turns the head 10 degrees a tick toward the body.
	// vanilla: LookControl.tick
	m.HeadYaw = turnToward(m.HeadYaw, m.Yaw, ghastHeadTurnSpeed)
}

// ghastShootGoal is Ghast.GhastShootFireballGoal: with a visible target in
// 64 blocks the charge counts up, warns at 10 and fires at 20, then waits 40
// ticks; out of sight it counts back down.
type ghastShootGoal struct {
	m          *Mob
	chargeTime int
}

func (g *ghastShootGoal) Priority() int                   { return ghastLookPrio }
func (g *ghastShootGoal) Flags() GoalFlags                { return 0 }
func (g *ghastShootGoal) CanStart(t *tickView) bool       { return g.m.targetIn(t) != nil }
func (g *ghastShootGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *ghastShootGoal) Start(*tickView)                 { g.chargeTime = 0 }
func (g *ghastShootGoal) Stop(*tickView)                  { g.m.SetMetadataGhastCharging(false) }
func (g *ghastShootGoal) RequiresEveryTick() bool         { return true }

func (g *ghastShootGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	if Dist2(p.X, p.Y, p.Z, m.X, m.Y, m.Z) < ghastSightSq && m.seesTarget(t.w, p) {
		g.chargeTime++
		if g.chargeTime == ghastWarnTick {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobGhastWarned})
		}
		if g.chargeTime == ghastShootTick {
			g.shoot(t, p)
			g.chargeTime = ghastCooldown
		}
	} else if g.chargeTime > 0 {
		g.chargeTime--
	}
	m.SetMetadataGhastCharging(g.chargeTime > ghastWarnTick)
}

// shoot aims at half the target's height from four blocks ahead of the body
// along its view.
// vanilla: Ghast.GhastShootFireballGoal.tick
func (g *ghastShootGoal) shoot(t *tickView, p *Player) {
	m := g.m
	vx, _, vz := lookVector(m.Yaw, m.Pitch)
	x, z := m.X+vx*ghastMuzzle, m.Z+vz*ghastMuzzle
	half := m.Y + m.height()*0.5
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobShotLargeFireball,
		X: x, Y: half + 0.5, Z: z,
		VX: p.X - x, VY: p.Y + p.height()*0.5 - (half + 0.5), VZ: p.Z - z,
		Radius: float64(m.ghast.power)})
}
