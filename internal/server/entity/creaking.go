package entity

import (
	"github.com/golem-mc/golem/registry"

	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// The creaking: frozen while a player looks at it, it hunts the player whose
// gaze woke it, and once bound to a creaking heart it only sways when hurt.
// Not modelled: the transient body's pathfinding malus.
// vanilla: Creaking, CreakingAi

// TypeCreaking is the creaking.
const TypeCreaking = registry.EntityTypeCreaking

func init() {
	// vanilla: EntityTypes.CREAKING and Creaking.createAttributes
	kinds[TypeCreaking] = &kind{
		name: "Creaking", width: 0.9, height: 2.7, eyeHeight: 2.3,
		maxHealth: 1, speed: float64(float32(0.4)), hostile: true,
		followRange: creakingFollowRange, attackDamage: 3, stepHeight: 1.0625,
		sounds: MobSounds{
			Ambient: registry.SoundEntityCreakingAmbient,
			Death:   registry.SoundEntityCreakingDeath,
			Step:    registry.SoundEntityCreakingStep,
		},
	}
	// vanilla: EntityTypes.CREAKING clientTrackingRange(8)
	mobTracking[TypeCreaking] = tracking{rangeChunks: 8, updateInterval: 3}
}

// The creaking's numbers.
// vanilla: Creaking, CreakingAi
const (
	creakingFollowRange   = 32
	creakingActivateDist2 = 144.0 // ACTIVATION_RANGE_SQ
	creakingCone          = 0.5   // the isLookingAtMe cone
	creakingSwayTicks     = 8     // INVULNERABILITY_ANIMATION_DURATION
	creakingAttackEvery   = 40    // ATTACK_INTERVAL
	creakingIdlePace      = 0.3   // SPEED_MULTIPLIER_WHEN_IDLING
	creakingStrollPrio    = 6
	creakingMeleePrio     = 4
	creakingStuckLimit    = 4 // MAX_PLAYER_STUCK_COUNTER

	// EntityStatusCreakingSway is the entity event that plays the sway.
	// vanilla: Creaking.handleEntityEvent
	creakingSwayStatus = 66
	// creakingAttackStatus is the entity event that plays the swing.
	creakingAttackStatus = 4
)

// CreakingTearDownTicks is TWITCH_DEATH_DURATION: how long a body the heart's
// loss killed twitches before it is taken away.
const CreakingTearDownTicks = 45

// creakingState is what a creaking keeps beyond the common mob.
type creakingState struct {
	// bound is set once the body has a heart; home is that heart's block.
	bound bool
	home  Waypoint
	// orphaned is the server's reading, taken before each tick, that the home
	// heart is loaded and does not list this body as its protector.
	orphaned bool
	// frozen is the inverse of CAN_MOVE, so the zero value moves.
	frozen bool
	// active is IS_ACTIVE: the body has seen a player look at it and hunts.
	active bool
	// tearingDown is IS_TEARING_DOWN, set when the heart's loss kills it.
	tearingDown bool
	// sway counts the ticks the body is untouchable after a blow.
	sway int
	// swayDue is a blow the next tick tells to the clients; hurtDue is a
	// player's blow the heart is told of.
	swayDue, hurtDue bool
	// stuckNow is whether a player's eyes sit inside the body this tick, and
	// stuck the heart's count of the checks that found one.
	stuckNow bool
	stuck    int
}

// CreakingView is the part of a creaking the metadata carries.
type CreakingView struct {
	Frozen, Active, TearingDown bool
	Bound                       bool
	Home                        [3]int32
}

// IsCreaking reports whether the mob is a creaking.
func (m *Mob) IsCreaking() bool { return m.Type == TypeCreaking }

// CreakingView reads the creaking's synched data.
func (m *Mob) CreakingView() CreakingView {
	c := &m.creaking
	return CreakingView{Frozen: c.frozen, Active: c.active, TearingDown: c.tearingDown,
		Bound: c.bound, Home: [3]int32{c.home.X, c.home.Y, c.home.Z}}
}

// CreakingHome is the block of the heart the creaking is bound to.
func (m *Mob) CreakingHome() (x, y, z int32, ok bool) {
	c := &m.creaking
	return c.home.X, c.home.Y, c.home.Z, c.bound
}

// CanUsePortal reports whether the body may cross a nether portal or end
// gateway: never the wither, nor a creaking bound to its heart.
// vanilla: WitherBoss.canUsePortal, Creaking.canUsePortal
func (m *Mob) CanUsePortal() bool {
	return m.Type != TypeWither && (m.Type != TypeCreaking || !m.creaking.bound)
}

// CreakingBind binds the body to the heart at (x, y, z).
// vanilla: Creaking.setTransient
func (m *Mob) CreakingBind(x, y, z int32) {
	m.SetMetadataCreakingBound(true)
	m.creaking.home = Waypoint{x, y, z}
	m.InvalidateMetadataCreakingHome()
}

// CreakingOrphaned tells a bound body whether its loaded home no longer holds
// it: the block is no creaking heart, or the heart lists another creaking. A
// home in an unloaded chunk is not lost.
// vanilla: Creaking.tick, CreakingHeartBlockEntity.isProtector
func (m *Mob) CreakingOrphaned(lost bool) { m.creaking.orphaned = lost }

// CreakingTearingDown reports whether the heart's loss is killing the body.
func (m *Mob) CreakingTearingDown() bool { return m.creaking.tearingDown }

// CreakingTearDown takes the body away without a death.
// vanilla: Creaking.tearDown
func (m *Mob) CreakingTearDown() { m.Discard() }

// CreakingKillByHeart is the death the heart's removal of a protector deals:
// the body twitches for CreakingTearDownTicks and is then taken away.
// vanilla: CreakingHeartBlockEntity.removeProtector
func (m *Mob) CreakingKillByHeart() {
	m.SetMetadataCreakingTearingDown(true)
	m.Kill()
}

// CreakingStuckCheck is Creaking.playerIsStuckInYou, asked by the heart on
// each of its checks: whether a player's eyes have been inside the body for
// more than four checks running.
func (m *Mob) CreakingStuckCheck() bool {
	c := &m.creaking
	if !c.stuckNow {
		c.stuck = 0
		return false
	}
	c.stuck++
	return c.stuck > creakingStuckLimit
}

// creakingSounds is Creaking.getAmbientSound and getHurtSound: silent while
// hunting; a bound body's hurt is the sway.
func (m *Mob) creakingSounds(s MobSounds) MobSounds {
	if m.creaking.active {
		s.Ambient = registry.SoundNone
	}
	s.Hurt = registry.SoundEntityGenericHurt
	if m.creaking.bound {
		s.Hurt = registry.SoundEntityCreakingSway
	}
	return s
}

// creakingShields reports whether a bound body refuses the hurt (all but
// bypassing types). A blow outside a sway starts one when a player is behind
// it or its direct entity is living or a projectile (entityBehind); only a
// player's blow tells the heart.
// vanilla: Creaking.hurtServer
func (m *Mob) creakingShields(damageType registry.DamageType, entityBehind, byPlayer bool) bool {
	c := &m.creaking
	if !c.bound || m.Dead || rules.InvulnerabilityBypassed(damageType) {
		return false
	}
	if (byPlayer || entityBehind) && c.sway == 0 {
		c.sway = creakingSwayTicks
		c.swayDue, c.hurtDue = true, byPlayer
	}
	return true
}

// creakingGoals is CreakingAi: the freeze above all, then the swing at 4 and
// the idle stroll and looks.
func creakingGoals(m *Mob) []Goal {
	look := &lookAtPlayer{m: m, priority: hostileLookAt}
	return []Goal{
		newFloatGoal(m),
		&creakingFreezeGoal{m: m},
		&meleeAttackGoal{m: m, priority: creakingMeleePrio, interval: creakingAttackEvery, brain: true,
			swingStatus: creakingAttackStatus, swingSound: registry.SoundEntityCreakingAttack, swingPitch: m.VoicePitch},
		&gatedGoal{Goal: newBrainIdle(m, creakingStrollPrio, creakingIdlePace, look.player),
			enabled: func() bool { return m.TargetID == 0 }},
		look,
		&randomLookAround{m: m, priority: hostileLookAt},
	}
}

// creakingFreezeGoal holds the body still while it is looked at: it takes
// the move, jump and look controls from every goal.
// vanilla: Creaking.CreakingMoveControl, CreakingLookControl, CreakingJumpControl
type creakingFreezeGoal struct{ m *Mob }

func (g *creakingFreezeGoal) Priority() int    { return 0 }
func (g *creakingFreezeGoal) Flags() GoalFlags { return GoalMove | GoalJump | GoalLook }

func (g *creakingFreezeGoal) CanStart(*tickView) bool       { return g.m.creaking.frozen }
func (g *creakingFreezeGoal) ShouldContinue(*tickView) bool { return g.m.creaking.frozen }
func (g *creakingFreezeGoal) Start(*tickView)               { g.m.stopSeeking() }
func (g *creakingFreezeGoal) Stop(*tickView)                {}
func (g *creakingFreezeGoal) RequiresEveryTick() bool       { return true }

func (g *creakingFreezeGoal) Tick(*tickView) {
	g.m.stopSeeking()
	g.m.VX, g.m.VZ = 0, 0
}

// creakingStep is Creaking.aiStep plus the brain's targeting: a body without
// its heart dies, the sway counts down, the freeze is decided, and an active
// body hunts the nearest visible player.
// vanilla: Creaking.aiStep, Creaking.checkCanMove, CreakingAi
func (m *Mob) creakingStep(t *tickView) {
	c := &m.creaking
	if !m.Dead && c.bound && c.orphaned {
		m.Kill()
	}
	if c.sway > 0 {
		c.sway--
	}
	if m.Dead {
		return
	}
	if c.swayDue {
		c.swayDue = false
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: creakingSwayStatus},
			MobEvent{Mob: m, Kind: MobActed, X: m.X, Y: m.Y, Z: m.Z},
			MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityCreakingSway, Volume: m.SoundVolume(), Pitch: m.VoicePitch()})
	}
	if c.hurtDue {
		c.hurtDue = false
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobCreakingHurt})
	}
	if m.NoAI {
		return
	}
	near := m.creakingPlayers(t)
	c.stuckNow = false
	for _, p := range near {
		if m.containsPoint(p.X, playerEye(p), p.Z) {
			c.stuckNow = true
		}
	}
	frozen := m.creakingCanMove(t, near)
	if frozen != c.frozen {
		m.SetMetadataCreakingFrozen(frozen)
		sound := registry.SoundEntityCreakingUnfreeze
		if frozen {
			m.stopSeeking()
			m.VX, m.VZ = 0, 0
			sound = registry.SoundEntityCreakingFreeze
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobActed, X: m.X, Y: m.Y, Z: m.Z},
			MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: m.SoundVolume(), Pitch: m.VoicePitch()})
	}
	if c.active {
		m.creakingTarget(t, near)
	}
}

// creakingPlayers are the players within the follow range, nearest first.
// vanilla: PlayerSensor
func (m *Mob) creakingPlayers(t *tickView) []*Player {
	var out []*Player
	for i := range t.players {
		p := &t.players[i]
		if p.Spectator || p.Dead {
			continue
		}
		if Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < creakingFollowRange*creakingFollowRange {
			out = append(out, p)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && Dist2(m.X, m.Y, m.Z, out[j].X, out[j].Y, out[j].Z) <
			Dist2(m.X, m.Y, m.Z, out[j-1].X, out[j-1].Y, out[j-1].Z); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// containsPoint reports whether the point is inside the body's box, the max
// faces excluded.
// vanilla: AABB.contains
func (m *Mob) containsPoint(x, y, z float64) bool {
	half := m.width() / 2
	return x >= m.X-half && x < m.X+half && z >= m.Z-half && z < m.Z+half &&
		y >= m.Y && y < m.Y+m.height()
}

// creakingCanMove is Creaking.checkCanMove but returns frozen (the inverse):
// a player's look wakes a sleeping body and holds a hunting one.
func (m *Mob) creakingCanMove(t *tickView, near []*Player) (frozen bool) {
	c := &m.creaking
	if len(near) == 0 {
		if c.active {
			m.creakingDeactivate(t)
		}
		return false
	}
	potential := false
	for _, p := range near {
		if !p.Huntable {
			continue
		}
		potential = true
		if (c.active && m.gazeDisguised(p)) || !m.lookedAt(t.w, p) {
			continue
		}
		if c.active {
			return true
		}
		if Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < creakingActivateDist2 {
			m.SetMetadataCreakingActive(true)
			m.SetMetadataTargetID(p.EntityID)
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityCreakingActivate, Volume: m.SoundVolume(), Pitch: m.VoicePitch()},
				MobEvent{Mob: m, Kind: MobEntityAction})
			return true
		}
	}
	if !potential && c.active {
		m.creakingDeactivate(t)
	}
	return false
}

func (m *Mob) creakingDeactivate(t *tickView) {
	m.SetMetadataCreakingActive(false)
	m.SetMetadataTargetID(0)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityCreakingDeactivate, Volume: m.SoundVolume(), Pitch: m.VoicePitch()},
		MobEvent{Mob: m, Kind: MobEntityAction})
}

// creakingTarget keeps an active body on the nearest player it sees and
// drops one it no longer can.
// vanilla: CreakingAi.initIdleActivity StartAttacking, initFightActivity
func (m *Mob) creakingTarget(t *tickView, near []*Player) {
	var first *Player
	for _, p := range near {
		if p.Huntable && m.seesTarget(t.w, p) {
			if first == nil {
				first = p
			}
			if p.EntityID == m.TargetID {
				return
			}
		}
	}
	m.SetMetadataTargetID(0)
	if first != nil {
		m.SetMetadataTargetID(first.EntityID)
	}
}

// lookedAt is LivingEntity.isLookingAtMe for the three heights the creaking
// asks: the eyes, the middle and half a block above the feet.
// vanilla: Creaking.checkCanMove
func (m *Mob) lookedAt(w World, p *Player) bool {
	eyeY := m.Y + m.eyeHeight()
	yaw, pitch := p.Yaw*math.Pi/180, p.Pitch*math.Pi/180
	lx := -math.Sin(yaw) * math.Cos(pitch)
	ly := -math.Sin(pitch)
	lz := math.Cos(yaw) * math.Cos(pitch)
	for _, gaze := range [3]float64{eyeY, m.Y + 0.5, (eyeY + m.Y) / 2} {
		dx, dy, dz := m.X-p.X, gaze-playerEye(p), m.Z-p.Z
		dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if dist == 0 {
			continue
		}
		if (lx*dx+ly*dy+lz*dz)/dist > 1-creakingCone && rayClearVisual(w, p.X, playerEye(p), p.Z, m.X, gaze, m.Z) {
			return true
		}
	}
	return false
}
