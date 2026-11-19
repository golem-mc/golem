package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The wither: a fire-immune flying boss that stands invulnerable for 220
// ticks after its summoning, blasts the cells around it, then shoots wither
// skulls from three heads. Hunting targets players only here.
// vanilla: WitherBoss

// TypeWither is the wither.
const TypeWither = registry.EntityTypeWither

func init() {
	// vanilla: EntityTypes.WITHER and WitherBoss.createAttributes: a 0.9 by
	// 3.5 box (eyes at the default 0.85 of it), 300 health, 0.6 movement and
	// flying speed, a follow range of 40 and 4 armor.
	kinds[TypeWither] = &kind{
		name: "Wither", width: 0.9, height: 3.5,
		maxHealth: 300, speed: float64(float32(0.6)), hostile: true, followRange: 40, armor: 4,
		fireImmune: true, flying: true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityWitherAmbient,
			Hurt:    registry.SoundEntityWitherHurt,
			Death:   registry.SoundEntityWitherDeath,
		},
	}
	// vanilla: EntityTypes.WITHER clientTrackingRange(10)
	mobTracking[TypeWither] = tracking{rangeChunks: 10, updateInterval: 3}
}

// The wither's numbers.
// vanilla: WitherBoss
const (
	witherInvulTicks     = 220 // INVULNERABLE_TICKS
	witherHealEvery      = 10  // ticks between the summoning's 10-health heals
	witherSummonHeal     = 10.0
	witherRestHealEvery  = 20 // ticks between the 1-health heals
	witherDestroyDelay   = 20 // ticks from a hit to the block-breaking pass
	witherHurtIdle       = 3  // idle head updates a hit adds
	witherBlastPower     = 7.0
	witherSideRange      = 20.0 // TARGETING_CONDITIONS range
	witherSideDropSq     = 900.0
	witherHeadReachXZ    = 20.0 // the side heads' search box, inflate(20, 8, 20)
	witherHeadReachY     = 8.0
	witherHeadOffset     = 1.3 // side heads sit this far out along the body yaw
	witherCentreHeadY    = 3.0
	witherSideHeadY      = 2.2
	witherIdleShots      = 15 // idle updates before a side head shoots at random
	witherDangerChance   = 0.001
	witherRise           = 0.3 // the climb's and the chase's target speed
	witherRiseEase       = 0.6
	witherAttackInterval = 40 // the ranged goal's min and max interval
	witherRangedRange    = 20.0
	witherLoseHeight     = 5.0 // the unpowered wither stays this far above its target
	witherChaseSq        = 9.0
	witherAttackPrio     = 2
	witherFloatPrio      = 5
	witherWanderRange    = 10.0 // WaterAvoidingRandomFlyingGoal's HoverRandomPos horizontal reach
	witherLookPrio       = 6
	witherLookAroundPri  = 7
)

// witherState is what a wither keeps beyond the common mob.
type witherState struct {
	// invul is DATA_ID_INV, the ticks of summoning invulnerability left.
	invul int
	// heads are DATA_TARGET_A to C: the entity IDs the three heads aim at, 0
	// for none.
	heads [3]int32
	// nextHead and idleHead are the side heads' next update tick (by Age) and
	// their idle update counts.
	nextHead, idleHead [2]int
	// destroyTicks counts down from a hit to the block-breaking pass; 0 is idle.
	destroyTicks int
}

// IsWither reports whether the mob is a wither.
func (m *Mob) IsWither() bool { return m.Type == TypeWither }

// WitherInvul is the ticks of summoning invulnerability left.
func (m *Mob) WitherInvul() int { return m.wither.invul }

// WitherHeads are the entity IDs of the three heads' targets.
func (m *Mob) WitherHeads() [3]int32 { return m.wither.heads }

// MakeInvulnerable is the summoning's start: 220 ticks of invulnerability at
// a third of the health.
// vanilla: WitherBoss.makeInvulnerable
func (m *Mob) MakeInvulnerable() {
	m.SetMetadataWitherInvul(witherInvulTicks)
	m.SetMetadataHealth(m.MaxHealth / 3)
}

// WitherBarFill is the boss bar's progress.
// vanilla: WitherBoss.customServerAiStep
func (m *Mob) WitherBarFill() float32 {
	if m.wither.invul > 0 {
		return float32(1 - float64(m.wither.invul)/witherInvulTicks)
	}
	return float32(m.Health / m.MaxHealth)
}

// witherPowered is isPowered: half the health or less.
func (m *Mob) witherPowered() bool { return m.Health <= m.MaxHealth/2 }

// witherRefuses is the part of WitherBoss.hurtServer that depends on the
// damage type alone: wither_immune_to, and anything the summoning's
// invulnerability does not let through.
// vanilla: WitherBoss.hurtServer
func (m *Mob) witherRefuses(damageType registry.DamageType) bool {
	if damageType.InTag(registry.DamageTypeTagIDWitherImmuneTo) {
		return true
	}
	return m.wither.invul > 0 && !rules.InvulnerabilityBypassed(damageType)
}

// noteWitherHurt arms the block-breaking pass and speeds up the side heads'
// idle shots when a hit gets past the wither's refusals.
// vanilla: WitherBoss.hurtServer
func (m *Mob) noteWitherHurt() {
	if m.Type != TypeWither {
		return
	}
	if m.wither.destroyTicks <= 0 {
		m.wither.destroyTicks = witherDestroyDelay
	}
	for i := range m.wither.idleHead {
		m.wither.idleHead[i] += witherHurtIdle
	}
}

// witherDeflects reports whether a powered wither shrugs off an arrow or a
// wind charge.
// vanilla: WitherBoss.hurtServer
func (m *Mob) witherDeflects() bool { return m.Type == TypeWither && m.witherPowered() }

// witherSpares reports whether a wither ignores a hit whose source is of the
// type: its own kind and every wither_friends mob.
// vanilla: WitherBoss.hurtServer
func (m *Mob) witherSpares(src MobType) bool {
	return m.Type == TypeWither && (src == TypeWither || src.InTag(registry.EntityTypeTagIDWitherFriends))
}

// witherGoals is WitherBoss.registerGoals: the summoning's do-nothing at 0,
// the ranged attack at 2, the flight at 5, the looks at 6 and 7; the grudge
// at 1 and the nearest living non-friend at 2.
func witherGoals(m *Mob) []Goal {
	return []Goal{
		&witherIdleGoal{m: m},
		&hurtByGoal{m: m, priority: 1},
		&mobTargetGoal{m: m, priority: witherAttackPrio, eager: true, unsighted: true, players: true,
			matches: func(o *Mob) bool { return !o.Type.InTag(registry.EntityTypeTagIDWitherFriends) }},
		&witherRangedGoal{m: m, attackTime: -1},
		&randomStroll{m: m, priority: witherFloatPrio, pick: func(t *tickView) (Waypoint, bool) { return m.airWanderPos(t, witherWanderRange) }},
		&lookAtPlayer{m: m, priority: witherLookPrio},
		&randomLookAround{m: m, priority: witherLookAroundPri},
	}
}

// witherIdleGoal is WitherDoNothingGoal: while invulnerable it holds the
// move, jump and look flags and does nothing.
type witherIdleGoal struct{ m *Mob }

func (g *witherIdleGoal) Priority() int                 { return 0 }
func (g *witherIdleGoal) Flags() GoalFlags              { return GoalMove | GoalJump | GoalLook }
func (g *witherIdleGoal) CanStart(*tickView) bool       { return g.m.wither.invul > 0 }
func (g *witherIdleGoal) ShouldContinue(*tickView) bool { return g.m.wither.invul > 0 }
func (g *witherIdleGoal) Start(*tickView)               {}
func (g *witherIdleGoal) Stop(*tickView)                {}
func (g *witherIdleGoal) Tick(*tickView)                {}
func (g *witherIdleGoal) RequiresEveryTick() bool       { return false }

// witherRangedGoal is RangedAttackGoal(1.0, 40, 20): close in until the
// target is seen for five ticks inside 20 blocks, face it, and shoot the
// centre head every 40 ticks.
// vanilla: RangedAttackGoal
type witherRangedGoal struct {
	m          *Mob
	attackTime int
	seeTime    int
}

func (g *witherRangedGoal) Priority() int    { return witherAttackPrio }
func (g *witherRangedGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *witherRangedGoal) CanStart(t *tickView) bool { return g.m.targetIn(t) != nil }

func (g *witherRangedGoal) ShouldContinue(t *tickView) bool {
	return g.CanStart(t) || g.m.targetIn(t) != nil && g.m.navOf().nav.flyActive
}

func (g *witherRangedGoal) Start(*tickView) {}

func (g *witherRangedGoal) Stop(*tickView) {
	g.seeTime = 0
	g.attackTime = -1
}

func (g *witherRangedGoal) RequiresEveryTick() bool { return true }

func (g *witherRangedGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	distSq := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	_, eye := p.targetBody(t)
	seen := m.seesPoint(t.w, p.X, p.Y+eye, p.Z)
	if seen {
		g.seeTime++
	} else {
		g.seeTime = 0
	}
	if distSq > witherRangedRange*witherRangedRange || g.seeTime < 5 {
		m.seekTo(t.w, p.X, p.Y, p.Z, 1.0, navReach)
	} else {
		m.stopSeeking()
	}
	m.lookAtTarget(p)
	g.attackTime--
	if g.attackTime == 0 {
		if !seen {
			return
		}
		m.witherShoot(t, 0, p.X, p.Y+eye*0.5, p.Z, m.Rand.Float32() < witherDangerChance)
		g.attackTime = witherAttackInterval
	} else if g.attackTime < 0 {
		g.attackTime = witherAttackInterval
	}
}

// witherHead is getHeadX, getHeadY and getHeadZ: the centre head over the
// body, a side head 1.3 out along the body yaw, the other 180 degrees round.
// vanilla: WitherBoss.getHeadX, getHeadY, getHeadZ
func (m *Mob) witherHead(index int) (x, y, z float64) {
	if index <= 0 {
		return m.X, m.Y + witherCentreHeadY, m.Z
	}
	angle := float64(float32(m.Yaw)+float32(180*(index-1))) * (math.Pi / 180)
	return m.X + math.Cos(angle)*witherHeadOffset, m.Y + witherSideHeadY, m.Z + math.Sin(angle)*witherHeadOffset
}

// witherShoot queues a wither skull from the head toward the point.
// vanilla: WitherBoss.performRangedAttack
func (m *Mob) witherShoot(t *tickView, head int, tx, ty, tz float64, dangerous bool) {
	hx, hy, hz := m.witherHead(head)
	amount := 0
	if dangerous {
		amount = 1
	}
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobShotWitherSkull,
		X: hx, Y: hy, Z: hz, VX: tx - hx, VY: ty - hy, VZ: tz - hz, Amount: amount})
}

// witherStep is WitherBoss.aiStep and customServerAiStep ahead of the goals:
// the body steers toward the centre head's target, the summoning counts
// down and blasts, and the side heads pick and shoot targets.
// vanilla: WitherBoss.aiStep, WitherBoss.customServerAiStep
func (m *Mob) witherStep(t *tickView) {
	if m.Dead {
		return
	}
	// The centre head's target steers the body; y keeps 60% of its speed.
	vy := m.VY * witherRiseEase
	if id := m.wither.heads[0]; id > 0 {
		if p := m.targetByID(t, id); p != nil {
			if m.Y < p.Y || !m.witherPowered() && m.Y < p.Y+witherLoseHeight {
				vy = math.Max(0, vy)
				vy += witherRise - vy*float64(float32(0.6))
			}
			dx, dz := p.X-m.X, p.Z-m.Z
			if d2 := dx*dx + dz*dz; d2 > witherChaseSq {
				d := math.Sqrt(d2)
				m.VX += dx/d*witherRise - m.VX*witherRiseEase
				m.VZ += dz/d*witherRise - m.VZ*witherRiseEase
			}
		}
	}
	m.VY = vy
	if m.VX*m.VX+m.VZ*m.VZ > 0.05 {
		m.Yaw = float32(math.Atan2(m.VZ, m.VX)*57.295776 - 90)
	}
	if m.NoAI {
		return
	}
	w := &m.wither
	if w.invul > 0 {
		m.SetMetadataWitherInvul(w.invul - 1)
		if w.invul <= 0 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobWitherExploded, X: m.X, Y: m.Y + m.eyeHeight(), Z: m.Z,
				Radius: witherBlastPower})
		}
		if m.Age%witherHealEvery == 0 {
			m.heal(witherSummonHeal)
		}
		return
	}
	for i := 1; i < 3; i++ {
		m.witherSideHead(t, i)
	}
	if m.TargetID != 0 {
		heads := w.heads
		heads[0] = m.TargetID
		m.SetMetadataWitherHeads(heads)
	} else {
		heads := w.heads
		heads[0] = 0
		m.SetMetadataWitherHeads(heads)
	}
	if w.destroyTicks > 0 {
		w.destroyTicks--
		if w.destroyTicks == 0 && t.w.BoolRule(world.RuleMobGriefing) {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobWitherSmashed, X: m.X, Y: m.Y, Z: m.Z,
				Radius: math.Floor(m.width()/2 + 1), Amount: int(math.Floor(m.height()))})
		}
	}
	if m.Age%witherRestHealEvery == 0 {
		m.heal(1)
	}
}

// witherSideHead is one pass of customServerAiStep's loop for side head i:
// every 10 to 19 ticks a stray skull after enough idle updates, then a
// shot at its target, or a new one picked from the players near.
// vanilla: WitherBoss.customServerAiStep
func (m *Mob) witherSideHead(t *tickView, i int) {
	w := &m.wither
	if m.Age < w.nextHead[i-1] {
		return
	}
	w.nextHead[i-1] = m.Age + 10 + m.Rand.Intn(10)
	if d := t.w.Difficulty(); d == rules.DifficultyNormal || d == rules.DifficultyHard {
		w.idleHead[i-1]++
		if w.idleHead[i-1]-1 > witherIdleShots {
			// Mth.nextDouble(random, lo, hi) is lo + (hi - lo) * nextDouble.
			xt := m.X - 10 + 20*m.Rand.Float64()
			yt := m.Y - 5 + 10*m.Rand.Float64()
			zt := m.Z - 10 + 20*m.Rand.Float64()
			m.witherShoot(t, i+1, xt, yt, zt, true)
			w.idleHead[i-1] = 0
		}
	}
	if id := w.heads[i]; id > 0 {
		p := m.targetByID(t, id)
		if p == nil || !p.Huntable || Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) > witherSideDropSq {
			heads := w.heads
			heads[i] = 0
			m.SetMetadataWitherHeads(heads)
			return
		}
		_, eye := p.targetBody(t)
		if !m.seesPoint(t.w, p.X, p.Y+eye, p.Z) {
			heads := w.heads
			heads[i] = 0
			m.SetMetadataWitherHeads(heads)
			return
		}
		m.witherShoot(t, i+1, p.X, p.Y+eye*0.5, p.Z, false)
		w.nextHead[i-1] = m.Age + 40 + m.Rand.Intn(20)
		w.idleHead[i-1] = 0
		return
	}
	var near []*Player
	for j := range t.players {
		p := &t.players[j]
		if !p.Huntable || math.Abs(p.X-m.X) > witherHeadReachXZ+0.3 || math.Abs(p.Z-m.Z) > witherHeadReachXZ+0.3 ||
			p.Y > m.Y+m.height()+witherHeadReachY || p.Y+p.height() < m.Y-witherHeadReachY {
			continue
		}
		if m.alliedTo(p) {
			continue
		}
		reach := max(witherSideRange*visibilityPercent(m, p), minVisibilityDistance)
		if Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) <= reach*reach && m.seesTarget(t.w, p) {
			near = append(near, p)
		}
	}
	mb := m.mobBox()
	minX, minY, minZ, maxX, maxY, maxZ := mb.Corners()
	reach := AABB{minX - witherHeadReachXZ, minY - witherHeadReachY, minZ - witherHeadReachXZ,
		maxX + witherHeadReachXZ, maxY + witherHeadReachY, maxZ + witherHeadReachXZ}
	for _, o := range t.others {
		if o == m || !o.Alive() || o.Invulnerable || o.Type.InTag(registry.EntityTypeTagIDWitherFriends) || m.alliedToMob(o) ||
			!reach.Crosses(o.mobBox()) {
			continue
		}
		if Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) <= witherSideRange*witherSideRange && m.seesPoint(t.w, o.X, o.Y+o.eyeHeight(), o.Z) {
			p := o.asTarget()
			near = append(near, &p)
		}
	}
	if len(near) > 0 {
		heads := w.heads
		heads[i] = near[m.Rand.Intn(len(near))].EntityID
		m.SetMetadataWitherHeads(heads)
	}
}

// MobMayBreak reports whether a mob of the type breaks the block as part of
// its own tick: a ravager's leaves and crops, anything but a wither-immune block for a
// wither.
// vanilla: Ravager.aiStep, WitherBoss.canDestroy
func MobMayBreak(t MobType, state uint16) bool {
	if t == TypeWither {
		return int(state) < registry.StateCount && !registry.BlockOf(state).InTag(registry.TagIDWitherImmune)
	}
	if t == TypeRavager && rules.IsTrampledCrop(state) {
		return true
	}
	return IsLeaves(state)
}
