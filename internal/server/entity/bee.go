package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The bee: a flying neutral animal that stings a player it is angry at once,
// then dies within a minute. It pollinates flowers, brings nectar to a hive
// (beehive.go; the hive's side is internal/server/hive.go and internal/server/hivetick.go) and
// grows crops on the way. It breeds on flowers and follows one held out, and
// flies by a planned path (flypath.go) except to sting and to pollinate.
// vanilla: Bee

// TypeBee is minecraft:bee.
const TypeBee = registry.EntityTypeBee

func init() {
	// vanilla: EntityTypes.BEE and Bee.createAttributes: 0.55 by 0.5, eyes
	// 0.3, ten health, speed 0.3, attack damage 2; getSoundVolume 0.4. No
	// ambient sound: the buzz is a client-side loop.
	kinds[TypeBee] = &kind{
		name: "Bee", width: 0.55, height: 0.5, eyeHeight: 0.3,
		maxHealth: 10, speed: float64(float32(0.3)), attackDamage: 2, volume: 0.4, flying: true, flyPath: &beeFlight,
		// The baby is Entity.getDimensions scaled by LivingEntity.DEFAULT_BABY_SCALE.
		babyWidth: 0.275, babyHeight: 0.25, babyEye: 0.15,
		temptTag: &registry.ItemTagIDBeeFood,
		// vanilla: Bee.registerGoals' FollowParentGoal(this, 1.25) and TemptGoal(this, 1.25)
		followPace: 1.25, temptPace: 1.25,
		sounds: MobSounds{
			Hurt:  registry.SoundEntityBeeHurt,
			Death: registry.SoundEntityBeeDeath,
		},
		// The hurt reset still counts it.
		ambientInterval: 120, // vanilla: Animal.getAmbientSoundInterval
	}
	// vanilla: EntityTypes.BEE clientTrackingRange(8)
	mobTracking[TypeBee] = tracking{rangeChunks: 8, updateInterval: 3}
}

// The bee's goal priorities that sit between the modelled ones.
// vanilla: Bee.registerGoals
const (
	beePriorityBreed        = 2
	beePriorityTempt        = 3
	beePriorityFollowParent = 5
)

// The bee's numbers.
const (
	// beeFlyingSpeed is the FLYING_SPEED attribute, which the flying move
	// control scales its input by.
	beeFlyingSpeed = float64(float32(0.6))
	// beeAirAcceleration is LivingEntity.getFlyingSpeed, the acceleration an
	// airborne body applies to its input.
	beeAirAcceleration = float64(float32(0.02))
	// beePitchTurn is the maxTurn of FlyingMoveControl(this, 20, true).
	beePitchTurn = 20
	// beeAttackPace is BeeAttackGoal's speed modifier.
	beeAttackPace = 1.4
	// beeStingDeath is STING_DEATH_COUNTDOWN: by this many ticks after the
	// sting the bee is certain to have died.
	beeStingDeath = 1200
	// beeDrownAfter is the ticks in water before the bee starts to drown.
	beeDrownAfter = 20
	// beeWanderRange and beeWanderRise are BeeWanderGoal's horizontal reach
	// and vertical spread.
	beeWanderRange = 8
	beeWanderRise  = 7
	beeWanderTries = 10
)

// Bee flag bits, the byte at metadata index 18.
// vanilla: Bee.FLAG_ROLL, FLAG_HAS_STUNG, FLAG_HAS_NECTAR
const (
	BeeFlagRoll   = 2
	BeeFlagStung  = 4
	BeeFlagNectar = 8
)

// beeState is what a bee keeps beyond the common mob.
type beeState struct {
	stung, nectar, rolling bool
	// angerStartedNow is set when angerStep starts the anger timer this tick and
	// cleared at the next angerStep. Vanilla starts it after the goal selector,
	// so a goal pass on that tick still sees a calm bee.
	angerStartedNow bool
	// sinceSting counts the ticks since the sting, timeSinceSting.
	sinceSting int
	// underWater counts the ticks spent in water.
	underWater int
	mem        beeMemory
}

// HasStung is Bee.hasStung.
func (m *Mob) HasStung() bool { return m.bee.stung }

// HasNectar is Bee.hasNectar.
func (m *Mob) HasNectar() bool { return m.bee.nectar }

// SetBeeState restores the saved flags.
func (m *Mob) SetBeeState(stung, nectar bool) {
	m.SetMetadataBeeStung(stung)
	m.SetMetadataBeeNectar(nectar)
}

// BeeFlags is the byte of Bee.DATA_FLAGS_ID.
func (m *Mob) BeeFlags() uint8 {
	var f uint8
	if m.bee.rolling {
		f |= BeeFlagRoll
	}
	if m.bee.stung {
		f |= BeeFlagStung
	}
	if m.bee.nectar {
		f |= BeeFlagNectar
	}
	return f
}

// Sting is the end of Bee.doHurtTarget for a sting that hurt: the bee has
// stung and forgets its anger.
func (m *Mob) Sting() {
	m.SetMetadataBeeStung(true)
	m.stopBeingAngry()
}

// beeGoals is Bee.registerGoals, the part that is modelled, with the three
// target goals among the others by priority.
func beeGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&beeAttackGoal{m: m},
		&beeEnterHiveGoal{m: m},
		// vanilla: BeeHurtByOtherGoal alerts the bees that see the attacker
		&beeHurtByGoal{hurtByGoal{m: m, priority: priorityHurtBy, alertOthers: true, sight: true}},
		&breedGoal{m: m, priority: beePriorityBreed},
		&beeAngryTargetGoal{nearestTargetGoal{m: m, priority: priorityNearest, accept: func(t *tickView, p *Player) bool {
			return m.angryAt(t.w, p)
		}}},
		&temptGoal{m: m, priority: beePriorityTempt},
		&resetAngerGoal{m: m, alertSame: true},
		&beePollinateGoal{m: m},
		&followParentGoal{m: m, priority: beePriorityFollowParent},
		&beeLocateHiveGoal{m: m},
		&beeGoToHiveGoal{m: m},
		&beeGoToKnownFlowerGoal{m: m},
		&beeGrowCropGoal{m: m},
		&beeWanderGoal{m: m},
	}
}

// beeHurtByGoal is BeeHurtByOtherGoal, which holds only while the bee is
// angry.
type beeHurtByGoal struct{ hurtByGoal }

func (g *beeHurtByGoal) ShouldContinue(t *tickView) bool {
	return g.m.Angry() && g.hurtByGoal.ShouldContinue(t)
}

// beeAngryTargetGoal is BeeBecomeAngryTargetGoal: it names a player only for
// a bee that is angry and has not stung.
type beeAngryTargetGoal struct{ nearestTargetGoal }

func (g *beeAngryTargetGoal) beeCanTarget() bool { return g.m.Angry() && !g.m.bee.stung }

func (g *beeAngryTargetGoal) CanStart(t *tickView) bool {
	return g.beeCanTarget() && g.nearestTargetGoal.CanStart(t)
}

func (g *beeAngryTargetGoal) ShouldContinue(t *tickView) bool {
	return g.beeCanTarget() && g.m.TargetID != 0 && g.nearestTargetGoal.ShouldContinue(t)
}

// beeAttackGoal is BeeAttackGoal: a MeleeAttackGoal at 1.4 for an angry bee
// that has not stung. The flying navigation drives straight at the target.
type beeAttackGoal struct {
	m                *Mob
	ticksUntilAttack int
	window           canUseWindow
}

func (g *beeAttackGoal) Priority() int    { return 0 }
func (g *beeAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *beeAttackGoal) CanStart(t *tickView) bool {
	// vanilla: MeleeAttackGoal.canUse runs first, so a refused check still
	// uses up the window.
	return g.window.open(g.m.Age) && g.angryForCanUse() && g.holds(t)
}

// holds is canContinueToUse, which has no window.
func (g *beeAttackGoal) holds(t *tickView) bool {
	return g.m.targetIn(t) != nil && g.m.Angry() && !g.m.bee.stung
}

// angryForCanUse is isAngry as the goal selector saw it before this tick's
// updatePersistentAnger.
func (g *beeAttackGoal) angryForCanUse() bool {
	return !g.m.bee.angerStartedNow
}

func (g *beeAttackGoal) ShouldContinue(t *tickView) bool { return g.holds(t) }

func (g *beeAttackGoal) Start(*tickView) {
	g.m.stopSeeking()
	g.m.SetMetadataAggressive(true)
	g.ticksUntilAttack = 0
}

func (g *beeAttackGoal) Tick(t *tickView) {
	p := g.m.targetIn(t)
	if p == nil {
		return
	}
	// The BeeLookControl turns nothing while angry; the move control turns
	// the body.
	g.m.seekToFly(p.X, p.Y, p.Z, beeAttackPace)
	if g.ticksUntilAttack > 0 {
		g.ticksUntilAttack--
	}
	if g.ticksUntilAttack == 0 && g.m.inMeleeReach(p) && g.m.seesTarget(t.w, p) {
		g.ticksUntilAttack = attackInterval
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobAttackedPlayer, Damage: g.m.attackDamage(), Player: *p})
	}
}

func (g *beeAttackGoal) Stop(*tickView) {
	g.m.SetMetadataAggressive(false)
	g.m.stopSeeking()
}

func (g *beeAttackGoal) RequiresEveryTick() bool { return true }

// beeWanderGoal is BeeWanderGoal: one tick in ten once the flight is done, a
// point in a half circle ahead. Vanilla's HoverRandomPos lifts it above the
// ground; here any free air cell is taken.
// vanilla: Bee.BeeWanderGoal
type beeWanderGoal struct{ m *Mob }

func (g *beeWanderGoal) Priority() int    { return 8 }
func (g *beeWanderGoal) Flags() GoalFlags { return GoalMove }

func (g *beeWanderGoal) CanStart(*tickView) bool {
	return !g.m.flightOn() && g.m.Rand.Intn(10) == 0
}

func (g *beeWanderGoal) Start(t *tickView) {
	sin, cos := math.Sincos(float64(g.m.Yaw) * math.Pi / 180)
	base := math.Atan2(cos, -sin)
	for range beeWanderTries {
		angle := base + (g.m.Rand.Float64()*2-1)*math.Pi/2
		dist := math.Sqrt(g.m.Rand.Float64()) * beeWanderRange
		x := g.m.X + math.Cos(angle)*dist
		z := g.m.Z + math.Sin(angle)*dist
		y := g.m.Y + float64(g.m.Rand.Intn(2*beeWanderRise+1)-beeWanderRise)
		cell := cellAt(x, y, z)
		if flyCellPassable(t.w, cell.X, cell.Y, cell.Z, false, false, true) {
			g.m.seekTo(t.w, float64(cell.X)+0.5, float64(cell.Y)+0.5, float64(cell.Z)+0.5, 1.0, navReach)
			return
		}
	}
}

func (g *beeWanderGoal) ShouldContinue(*tickView) bool { return g.m.flightOn() }
func (g *beeWanderGoal) Tick(*tickView)                {}
func (g *beeWanderGoal) Stop(*tickView)                {}
func (g *beeWanderGoal) RequiresEveryTick() bool       { return false }

// beeStep is the part of Bee.aiStep and customServerAiStep that is modelled:
// the roll, the drowning in water and the death that follows a sting.
// vanilla: Bee.aiStep, Bee.customServerAiStep
func (m *Mob) beeStep(t *tickView) {
	b := &m.bee
	m.beeMemoryStep(t)
	if p := m.targetIn(t); p != nil {
		m.SetMetadataBeeRolling(m.Angry() && !b.stung && Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < 4)
	} else {
		m.SetMetadataBeeRolling(false)
	}
	if m.InWater() {
		b.underWater++
	} else {
		b.underWater = 0
	}
	if b.underWater > beeDrownAfter {
		if landed, soft := m.plainHurt(1, rules.DamageTypeDrown, false); landed {
			m.noteLastDamage(rules.DamageTypeDrown)
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobBurned,
				DamageType: rules.DamageTypeDrown, Softened: soft, NoBubble: true})
		}
	}
	if !b.stung {
		return
	}
	b.sinceSting++
	if b.sinceSting%5 == 0 && m.Rand.Intn(max(1, min(beeStingDeath-b.sinceSting, beeStingDeath))) == 0 {
		if landed, soft := m.plainHurt(m.Health, rules.DamageTypeGeneric, false); landed {
			m.noteLastDamage(rules.DamageTypeGeneric)
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobBurned,
				DamageType: rules.DamageTypeGeneric, Softened: soft})
		}
	}
}
