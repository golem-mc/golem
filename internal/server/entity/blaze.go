package entity

import (
	"github.com/golem-mc/golem/registry"

	"math"
)

// The blaze: a fire-immune monster that rises on its own thrust, burns off a
// player with three small fireballs in a row, and is hurt by water.
// vanilla: Blaze, Blaze.BlazeAttackGoal

// TypeBlaze is the blaze.
const TypeBlaze = registry.EntityTypeBlaze

func init() {
	// vanilla: EntityTypes.BLAZE and Blaze.createAttributes. The box is 0.6
	// by 1.8 with the default eye height of 0.85 of it.
	kinds[TypeBlaze] = &kind{
		name: "Blaze", width: 0.6, height: 1.8,
		maxHealth: 20, speed: float64(float32(0.23)), hostile: true,
		followRange: 48, attackDamage: 6, fireImmune: true, waterSensitive: true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityBlazeAmbient,
			Hurt:    registry.SoundEntityBlazeHurt,
			Death:   registry.SoundEntityBlazeDeath,
		},
	}
	// vanilla: EntityTypes.BLAZE clientTrackingRange(8)
	mobTracking[TypeBlaze] = tracking{rangeChunks: 8, updateInterval: 3}
}

// The blaze's numbers.
// vanilla: Blaze, Blaze.BlazeAttackGoal
const (
	blazeDefaultOffset  = 0.5   // Blaze.allowedHeightOffset's start
	blazeOffsetMode     = 0.5   // random.triangle(0.5, 6.891)
	blazeOffsetSpread   = 6.891 // the triangle's deviation
	blazeOffsetEvery    = 100   // nextHeightOffsetChangeTick
	blazeRise           = 0.3   // the climb's target speed, (double) 0.3f
	blazeRiseEase       = 0.3
	blazeFallDamping    = 0.6
	blazeMeleeDistSq    = 4.0
	blazeMeleeCooldown  = 20
	blazeChargeTicks    = 60  // attackStep 1
	blazeBurstGap       = 6   // attackStep 2 to 4
	blazeBurstPause     = 100 // attackStep 5
	blazeBurstSteps     = 4
	blazeSpread         = 2.297 // the fireball's triangle deviation, times sqd
	blazeUnseenChase    = 5     // lastSeen below this keeps chasing
	blazeWaterDamage    = 1.0
	blazeStrollPriority = 7
	blazeHomePriority   = 5
	blazeAttackPriority = 4
)

// blazeState is what a blaze keeps beyond the common mob.
type blazeState struct {
	// offset is allowedHeightOffset: how far above the blaze's eyes the
	// target's may be before it climbs.
	offset float64
	// nextOffset counts down to the next roll of offset.
	nextOffset int
	// charged is DATA_FLAGS_ID bit 0, set during the three-shot burst's
	// wind-up.
	charged bool
}

// IsBlaze reports whether the mob is a blaze.
func (m *Mob) IsBlaze() bool { return m.Type == TypeBlaze }

// BlazeCharged is the blaze's DATA_FLAGS_ID charged bit.
func (m *Mob) BlazeCharged() bool { return m.blaze.charged }

// blazeGoals is Blaze.registerGoals: the attack at 4, the return home at 5,
// the stroll at 7, the looks at 8; the grudge at 1 and the nearest player at 2.
func blazeGoals(m *Mob) []Goal {
	m.blaze.offset = blazeDefaultOffset
	return []Goal{
		&hurtByGoal{m: m, priority: 1, alertOthers: true},
		&nearestTargetGoal{m: m, priority: 2},
		&blazeAttackGoal{m: m},
		&homeGoal{m: m, priority: blazeHomePriority, pace: 1},
		&randomStroll{m: m, priority: blazeStrollPriority},
		&lookAtPlayer{m: m, priority: hostileLookAt},
		&randomLookAround{m: m, priority: hostileLookAt},
	}
}

// blazeStep is Blaze.aiStep and customServerAiStep before the goals: a
// falling blaze keeps 60% of its fall each tick, the allowed height offset
// is rerolled every 100 ticks, and a target above it lifts the blaze.
// vanilla: Blaze.aiStep, Blaze.customServerAiStep
func (m *Mob) blazeStep(t *tickView) {
	if m.Dead {
		return
	}
	if !m.OnGround && m.VY < 0 {
		m.VY *= blazeFallDamping
	}
	if m.NoAI {
		return
	}
	b := &m.blaze
	b.nextOffset--
	if b.nextOffset <= 0 {
		b.nextOffset = blazeOffsetEvery
		b.offset = float64(float32(blazeOffsetMode + blazeOffsetSpread*(m.Rand.Float64()-m.Rand.Float64())))
	}
	if p := m.targetIn(t); p != nil && p.Y+p.eyeHeight() > m.Y+m.eyeHeight()+b.offset {
		m.VY += (float64(float32(blazeRise)) - m.VY) * blazeRiseEase
	}
}

// blazeAttackGoal is Blaze.BlazeAttackGoal.
type blazeAttackGoal struct {
	m          *Mob
	attackStep int
	attackTime int
	lastSeen   int
}

func (g *blazeAttackGoal) Priority() int    { return blazeAttackPriority }
func (g *blazeAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *blazeAttackGoal) CanStart(t *tickView) bool { return g.m.targetIn(t) != nil }

func (g *blazeAttackGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *blazeAttackGoal) Start(*tickView) { g.attackStep = 0 }

func (g *blazeAttackGoal) Stop(*tickView) {
	g.m.SetMetadataBlazeCharged(false)
	g.lastSeen = 0
}

// RequiresEveryTick is the goal's own override: the burst counts every tick.
func (g *blazeAttackGoal) RequiresEveryTick() bool { return true }

func (g *blazeAttackGoal) Tick(t *tickView) {
	m := g.m
	g.attackTime--
	p := m.targetIn(t)
	if p == nil {
		return
	}
	seen := m.seesTarget(t.w, p)
	if seen {
		g.lastSeen = 0
	} else {
		g.lastSeen++
	}
	distSq := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	follow := m.followRange()
	switch {
	case distSq < blazeMeleeDistSq:
		if !seen {
			return
		}
		if g.attackTime <= 0 {
			g.attackTime = blazeMeleeCooldown
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.attackDamage(), Player: *p})
		}
		m.seekTo(t.w, p.X, p.Y, p.Z, 1.0, navReach)
	case distSq < follow*follow && seen:
		g.burst(t, p, distSq)
		m.lookAtTarget(p)
	case g.lastSeen < blazeUnseenChase:
		m.seekTo(t.w, p.X, p.Y, p.Z, 1.0, navReach)
	}
}

// burst advances the fireball cycle: a 60-tick charge, three fireballs six
// ticks apart, then a 100-tick pause.
// vanilla: Blaze.BlazeAttackGoal.tick
func (g *blazeAttackGoal) burst(t *tickView, p *Player, distSq float64) {
	m := g.m
	if g.attackTime > 0 {
		return
	}
	g.attackStep++
	switch {
	case g.attackStep == 1:
		g.attackTime = blazeChargeTicks
		m.SetMetadataBlazeCharged(true)
	case g.attackStep <= blazeBurstSteps:
		g.attackTime = blazeBurstGap
	default:
		g.attackTime = blazeBurstPause
		g.attackStep = 0
		m.SetMetadataBlazeCharged(false)
	}
	if g.attackStep <= 1 {
		return
	}
	// The origin and the aim are both taken at half the body's height.
	half := m.height() * 0.5
	sqd := math.Sqrt(math.Sqrt(distSq)) * 0.5
	dev := blazeSpread * sqd
	dx := p.X - m.X + dev*(m.Rand.Float64()-m.Rand.Float64())
	dy := p.Y + p.height()*0.5 - (m.Y + half)
	dz := p.Z - m.Z + dev*(m.Rand.Float64()-m.Rand.Float64())
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobShotFireball,
		X: m.X, Y: m.Y + half + 0.5, Z: m.Z, VX: dx, VY: dy, VZ: dz})
}
