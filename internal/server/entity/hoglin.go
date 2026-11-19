package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The hoglin and zoglin: charging melee mobs that throw their target; a
// hoglin flees repellents and becomes a zoglin outside the Nether. Not
// modelled: the repellent sounds, the ambient sound when a retreat ends,
// and natural spawns. Approximated: courtship uses love mode instead of a
// breed target, the zoglin scans every 20 ticks instead of every tick, and
// an unreachable target is not dropped after 200 ticks.
// vanilla: Hoglin, HoglinAi, Zoglin, HoglinBase

const (
	TypeHoglin = registry.EntityTypeHoglin
	TypeZoglin = registry.EntityTypeZoglin
)

// The kinds are registered here, not in mob.go's table, so the pair stays in
// one file.
func init() {
	// vanilla: EntityTypes.HOGLIN and Hoglin.createAttributes. The follow
	// range stays Mob.createMobAttributes' 16.
	kinds[TypeHoglin] = &kind{
		fireAverse: true,
		name:       "Hoglin", width: 1.3964844, height: 1.4, eyeHeight: 1.4 * 0.85,
		babyWidth: 0.75, babyHeight: 0.85, babyEye: 0.625,
		maxHealth: 40, speed: float64(float32(0.3)), hostile: true, attackDamage: hoglinAttack, knockbackResist: float64(float32(0.6)),
		temptTag: &registry.ItemTagIDHoglinFood, followPace: 0.6, // vanilla: HoglinAi.initIdleActivity
		sounds: MobSounds{
			Ambient: registry.SoundEntityHoglinAmbient,
			Hurt:    registry.SoundEntityHoglinHurt,
			Death:   registry.SoundEntityHoglinDeath,
			Step:    registry.SoundEntityHoglinStep,
		},
		ambientInterval: 120, // vanilla: Animal.getAmbientSoundInterval
	}
	// vanilla: EntityTypes.ZOGLIN (fireImmune) and Zoglin.createAttributes
	kinds[TypeZoglin] = &kind{
		name: "Zoglin", width: 1.3964844, height: 1.4, eyeHeight: 1.4 * 0.85,
		babyWidth: 0.75, babyHeight: 0.85, babyEye: 0.625,
		maxHealth: 40, speed: float64(float32(0.3)), hostile: true, attackDamage: hoglinAttack, knockbackResist: float64(float32(0.6)),
		fireImmune: true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityZoglinAmbient,
			Hurt:    registry.SoundEntityZoglinHurt,
			Death:   registry.SoundEntityZoglinDeath,
			Step:    registry.SoundEntityZoglinStep,
		},
	}
	// vanilla: EntityTypes.HOGLIN, ZOGLIN clientTrackingRange(8)
	mobTracking[TypeHoglin] = tracking{rangeChunks: 8, updateInterval: 3}
	mobTracking[TypeZoglin] = tracking{rangeChunks: 8, updateInterval: 3}
}

// The hoglin's numbers.
const (
	hoglinAttack         = 6.0 // Hoglin.ATTACK_DAMAGE
	hoglinBabyAttack     = 0.5 // Hoglin.BABY_ATTACK_DAMAGE
	hoglinAdultInterval  = 40  // HoglinAi.ATTACK_INTERVAL
	hoglinBabyInterval   = 15  // HoglinAi.BABY_ATTACK_INTERVAL
	hoglinGrudgeTicks    = 200 // HoglinAi.ATTACK_DURATION, Zoglin.setAttackTarget: a retaliation's expiry
	hoglinPacifyTicks    = 200 // HoglinAi.REPELLENT_PACIFY_TIME
	hoglinRepelReachXZ   = 8   // HoglinAi.REPELLENT_DETECTION_RANGE_HORIZONTAL
	hoglinRepelReachY    = 4   // HoglinAi.REPELLENT_DETECTION_RANGE_VERTICAL
	hoglinRetreatMin     = 100 // HoglinAi.RETREAT_DURATION: 5 to 20 seconds
	hoglinRetreatMax     = 400
	hoglinRetreatPace    = 1.3 // HoglinAi.SPEED_MULTIPLIER_WHEN_RETREATING
	hoglinRetreatReach   = 15  // HoglinAi.DESIRED_DISTANCE_FROM_PIGLIN_WHEN_RETREATING
	hoglinSensorInterval = 20  // Sensor.DEFAULT_SCAN_RATE
	hoglinConvertTicks   = 300 // Hoglin.CONVERSION_TIME
	hoglinStrollPace     = 0.4 // HoglinAi.SPEED_MULTIPLIER_WHEN_IDLING
	hoglinPiglinReach    = 8   // HoglinAi.DESIRED_DISTANCE_FROM_PIGLIN_WHEN_IDLING
	hoglinBabyOdds       = 0.2 // Hoglin.finalizeSpawn, Zoglin.finalizeSpawn
	// hoglinNylium is the walk-target value of a cell over crimson nylium.
	// vanilla: Hoglin.getWalkTargetValue
	hoglinNylium = 10.0
)

// hoglinState is the hoglin's and the zoglin's own state.
type hoglinState struct {
	// sensorIn counts the ticks until the repellent is looked for again.
	sensorIn int
	// repellent is the nearest repellent block of the last look, valid when
	// seen is set.
	repellent Waypoint
	seen      bool
	// pacified counts the ticks the hoglin does not hunt for.
	pacified int
	// inOverworld counts the ticks spent converting.
	inOverworld int
	// CannotBeHunted keeps piglins from hunting the hoglin.
	// vanilla: Hoglin.cannotBeHunted
	CannotBeHunted bool
	// retreat counts the ticks the hoglin keeps walking away from the body
	// retreatFrom (AVOID_TARGET).
	retreat     int
	retreatFrom int32
	// wasAngry is whether the last tick had a target, for the angry sound.
	wasAngry bool
}

// HoglinBlow rides a MobAttackedPlayer of a hoglin or zoglin: the attack
// sound and animation, and the throw the blow follows with.
type HoglinBlow struct {
	// Throw is whether the blow throws its target (an adult's), with the
	// rolled angle in radians, horizontal share and vertical share.
	Throw       bool
	Angle, H, V float64
	Zoglin      bool
}

// IsHoglinKind reports whether the type is the hoglin or the zoglin.
func IsHoglinKind(t MobType) bool { return t == TypeHoglin || t == TypeZoglin }

func (m *Mob) hoglinKind() bool { return IsHoglinKind(m.Type) }

// hoglinRepellents is the minecraft:hoglin_repellents block tag.
var hoglinRepellents = registry.BlockTagOf("minecraft:hoglin_repellents")

// piglinRepellents is the minecraft:piglin_repellents block tag.
var piglinRepellents = registry.BlockTagOf("minecraft:piglin_repellents")

func isHoglinRepellent(state uint16) bool {
	return registry.BlockOf(state).InTag(hoglinRepellents)
}

// isPiglinRepellent: a soul campfire only repels while lit.
// vanilla: PiglinSpecificSensor.isValidRepellent
func isPiglinRepellent(state uint16) bool {
	b := registry.BlockOf(state)
	return b.InTag(piglinRepellents) && (b != registry.BlockSoulCampfire || registry.StateBool(state, registry.PropLit))
}

// hoglinGoals is HoglinAi's idle and fight activities: grudge, repellent
// flight, player hunt, melee, piglin avoidance, courtship, slow stroll and
// glances.
func hoglinGoals(m *Mob) []Goal {
	look := &lookAtPlayer{m: m, priority: hostileLookAt}
	return []Goal{
		&hoglinRetreatGoal{m: m},
		// A retreating hoglin does not answer a piglin's blow.
		// vanilla: HoglinAi.maybeRetaliate
		&hoglinHurtGoal{grudgeGoal{m: m, priority: 1, brain: true, ignore: func(t MobType) bool {
			return t == TypeHoglin || t == TypePiglin && m.hoglin.retreat > 0
		}}},
		&hoglinRepelGoal{m: m, priority: 2, pace: meleePace,
			source: func() (Waypoint, bool) { return m.hoglin.repellent, m.hoglin.seen }},
		&hoglinTargetGoal{m: m, priority: 2, interval: hoglinSensorInterval,
			refuse: func() bool { return m.hoglin.pacified > 0 || m.IsInLove() }},
		&meleeAttackGoal{m: m, priority: 3, brain: true},
		&hoglinAvoidPiglinGoal{m: m},
		&breedGoal{m: m, priority: 4},
		&gatedGoal{Goal: &followParentGoal{m: m, ranged: &followRange{min: 5, max: 16, scan: 16}},
			controls: GoalMove, enabled: func() bool { return m.TargetID == 0 && m.hoglin.retreat == 0 }},
		newBrainIdle(m, hostileStroll, hoglinStrollPace, look.player),
		look,
		&randomLookAround{m: m, priority: hostileLookAround},
	}
}

// zoglinGoals is Zoglin's idle and fight activities: grudge, a hunt of the
// nearest living non-zoglin, non-creeper, melee, stroll and glances.
func zoglinGoals(m *Mob) []Goal {
	look := &lookAtPlayer{m: m, priority: hostileLookAt}
	return []Goal{
		&grudgeGoal{m: m, priority: 1, brain: true},
		&zoglinTargetGoal{m: m, priority: 2},
		&meleeAttackGoal{m: m, priority: 3, brain: true},
		newBrainIdle(m, hostileStroll, hoglinStrollPace, look.player),
		look,
		&randomLookAround{m: m, priority: hostileLookAround},
	}
}

// hoglinHurtGoal is HoglinAi.wasHurtBy: a blow lifts the pacification, a
// hoglin does not answer another hoglin's, a baby retreats instead, and an
// adult sends the adult hoglins it sees after the attacker.
type hoglinHurtGoal struct{ grudgeGoal }

func (g *hoglinHurtGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.retaliate != 0 && m.IsBaby() && g.canHunt(t, m.retaliate) {
		m.hoglin.pacified = 0
		m.hoglinRetreat(t, m.retaliate)
		m.retaliate = 0
		return false
	}
	return g.grudgeGoal.CanStart(t)
}

func (g *hoglinHurtGoal) Start(t *tickView) {
	g.m.hoglin.pacified = 0
	if g.m.retaliate != 0 {
		g.m.hoglinBroadcast(t, g.m.retaliate)
	}
	g.grudgeGoal.Start(t)
}

func (g *hoglinHurtGoal) Tick(t *tickView) {
	if id := g.m.retaliate; id != 0 && g.canHunt(t, id) {
		g.m.hoglin.pacified = 0
		g.m.hoglinBroadcast(t, id)
	}
	g.grudgeGoal.Tick(t)
}

// hoglinBroadcast gives every adult hoglin this adult sees the target,
// unless it is pacified or already after something nearer.
// vanilla: HoglinAi.broadcastAttackTarget, setAttackTargetIfCloserThanCurrent
func (m *Mob) hoglinBroadcast(t *tickView, id int32) {
	target := m.targetByID(t, id)
	if target == nil || m.IsBaby() || m.Type != TypeHoglin {
		return
	}
	for _, o := range t.others {
		if o == m || o.Type != TypeHoglin || o.IsBaby() || !o.Alive() || o.hoglin.pacified > 0 || o.TargetID == id || !m.sees(t, o) {
			continue
		}
		if cur := o.targetByID(t, o.TargetID); cur != nil &&
			Dist2(o.X, o.Y, o.Z, cur.X, cur.Y, cur.Z) < Dist2(o.X, o.Y, o.Z, target.X, target.Y, target.Z) {
			continue
		}
		o.SetMetadataTargetID(id)
		o.alerted = id
	}
}

// hoglinOnHit is HoglinAi.onHitTarget: an adult hoglin that strikes a piglin
// while piglins outnumber the hoglins it sees retreats from it with the
// adult hoglins it sees; any other blow sends them after the target.
func (m *Mob) hoglinOnHit(t *tickView, id int32) {
	if m.Type != TypeHoglin || m.IsBaby() {
		return
	}
	if o := t.mobByID(id); o != nil && o.Type == TypePiglin && m.hoglinPiglinsOutnumber(t) {
		m.hoglinAvoid(t, id)
		for _, h := range t.others {
			if h != m && adultHoglin(h) && m.sees(t, h) {
				h.hoglinRetreat(t, id)
			}
		}
		return
	}
	m.hoglinBroadcast(t, id)
}

// hoglinPiglinsOutnumber is HoglinAi.piglinsOutnumberHoglins: the adult
// piglins (not brutes) the adult sees outnumber the adult hoglins it sees,
// itself included.
// vanilla: HoglinSpecificSensor.doTick
func (m *Mob) hoglinPiglinsOutnumber(t *tickView) bool {
	if m.IsBaby() {
		return false
	}
	hoglins, piglins := m.visibleCounts(t, func(o *Mob) bool { return o.Type == TypePiglin && !o.IsBaby() })
	return piglins > hoglins+1
}

// hoglinRetreat starts a walk away from the nearest of the attacker and the
// current avoid and attack targets.
// vanilla: HoglinAi.retreatFromNearestTarget
func (m *Mob) hoglinRetreat(t *tickView, attacker int32) {
	h := &m.hoglin
	best, from := math.MaxFloat64, int32(0)
	for _, id := range [...]int32{attacker, h.retreatFrom, m.TargetID} {
		if id == h.retreatFrom && h.retreat <= 0 {
			continue
		}
		if p := m.targetByID(t, id); p != nil {
			if d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z); d < best {
				best, from = d, id
			}
		}
	}
	m.hoglinAvoid(t, from)
}

// hoglinAvoid drops the attack target and the walk and avoids the body for 5
// to 20 seconds.
// vanilla: HoglinAi.setAvoidTarget
func (m *Mob) hoglinAvoid(t *tickView, from int32) {
	h := &m.hoglin
	if h.retreat <= 0 {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityHoglinRetreat, Volume: 1, Pitch: m.VoicePitch()})
	}
	m.SetMetadataTargetID(0)
	m.stopSeeking()
	h.retreatFrom = from
	h.retreat = hoglinRetreatMin + m.Rand.Intn(hoglinRetreatMax-hoglinRetreatMin+1)
}

// hoglinRetreatGoal is the avoid activity: walk away from the avoided body
// until the timer runs out or, for an adult, piglins stop outnumbering the
// hoglins.
// vanilla: HoglinAi.initRetreatActivity, wantsToStopFleeing
type hoglinRetreatGoal struct{ m *Mob }

func (g *hoglinRetreatGoal) Priority() int           { return 0 }
func (g *hoglinRetreatGoal) Flags() GoalFlags        { return GoalMove | GoalTarget }
func (g *hoglinRetreatGoal) RequiresEveryTick() bool { return false }
func (g *hoglinRetreatGoal) Tick(t *tickView)        { g.flee(t) }
func (g *hoglinRetreatGoal) Start(t *tickView)       { g.flee(t) }

func (g *hoglinRetreatGoal) CanStart(t *tickView) bool { return g.ShouldContinue(t) }

func (g *hoglinRetreatGoal) ShouldContinue(t *tickView) bool {
	h := &g.m.hoglin
	return h.retreat > 0 && g.m.targetByID(t, h.retreatFrom) != nil &&
		(g.m.IsBaby() || g.m.hoglinPiglinsOutnumber(t))
}

func (g *hoglinRetreatGoal) Stop(*tickView) {
	g.m.hoglin.retreat = 0
	g.m.stopSeeking()
}

// flee picks a cell away from the avoided body when the walk has run out.
// vanilla: SetWalkTargetAwayFrom.entity
func (g *hoglinRetreatGoal) flee(t *tickView) {
	m := g.m
	o := m.targetByID(t, m.hoglin.retreatFrom)
	if o == nil || len(m.navOf().nav.path) > 0 ||
		Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) >= hoglinRetreatReach*hoglinRetreatReach {
		return
	}
	m.walkAwayFrom(t, o.X, o.Z, hoglinRetreatPace)
}

// hoglinAvoidPiglinGoal is the idle SetWalkTargetAwayFrom over
// NEAREST_VISIBLE_ADULT_PIGLIN: an adult with no target and no walk that
// sees an adult piglin within 8 blocks strolls away from it.
// vanilla: HoglinAi.initIdleActivity
type hoglinAvoidPiglinGoal struct{ m *Mob }

// hoglinPiglinNear is the largest squared distance below 8 blocks
// (Vec3.closerThan is strict, nearestSeen's bound inclusive).
var hoglinPiglinNear = math.Nextafter(hoglinPiglinReach*hoglinPiglinReach, 0)

func (g *hoglinAvoidPiglinGoal) Priority() int           { return 3 }
func (g *hoglinAvoidPiglinGoal) Flags() GoalFlags        { return GoalMove }
func (g *hoglinAvoidPiglinGoal) RequiresEveryTick() bool { return false }
func (g *hoglinAvoidPiglinGoal) Start(*tickView)         {}
func (g *hoglinAvoidPiglinGoal) Tick(*tickView)          {}
func (g *hoglinAvoidPiglinGoal) Stop(*tickView)          { g.m.stopSeeking() }

// ShouldContinue gives way to an attack target at once (HoglinAi.java:93).
func (g *hoglinAvoidPiglinGoal) ShouldContinue(*tickView) bool {
	return g.m.TargetID == 0 && len(g.m.navOf().nav.path) > 0
}

func (g *hoglinAvoidPiglinGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.IsBaby() || m.TargetID != 0 || len(m.navOf().nav.path) > 0 {
		return false
	}
	p := m.nearestSeen(t, hoglinPiglinNear, func(o *Mob) bool { return o.Type == TypePiglin && !o.IsBaby() })
	return p != nil && m.walkAwayFrom(t, p.X, p.Z, hoglinStrollPace)
}

// grudgeGoal is a retaliation, whose attack target expires after two
// hundred ticks.
// vanilla: HoglinAi.setAttackTarget, Zoglin.setAttackTarget
type grudgeGoal struct {
	hurtByGoal
	since int
}

func (g *grudgeGoal) Start(t *tickView) {
	g.since = g.m.Age
	g.hurtByGoal.Start(t)
}

// Tick takes up a blow struck while the grudge runs: each one restarts the
// expiry.
// vanilla: HoglinAi.setAttackTarget
func (g *grudgeGoal) Tick(t *tickView) {
	if g.m.retaliate != 0 && g.canHunt(t, g.m.retaliate) {
		g.Start(t)
	}
}

func (g *grudgeGoal) ShouldContinue(t *tickView) bool {
	return g.m.Age-g.since <= hoglinGrudgeTicks && g.hurtByGoal.ShouldContinue(t)
}

// hoglinTargetGoal is StartAttacking over the nearest visible player; the
// target has no expiry.
// vanilla: HoglinAi.findNearestValidAttackTarget, StartAttacking
type hoglinTargetGoal struct{ nearestTargetGoal }

func (g *hoglinTargetGoal) ShouldContinue(t *tickView) bool {
	return g.m.hoglin.pacified == 0 && !g.m.IsInLove() && g.nearestTargetGoal.ShouldContinue(t)
}

// zoglinTargetGoal is StartAttacking over the nearest visible living body
// that is no zoglin or creeper.
// vanilla: Zoglin.findNearestValidAttackTarget
type zoglinTargetGoal struct {
	m        *Mob
	priority int
}

func (g *zoglinTargetGoal) Priority() int    { return g.priority }
func (g *zoglinTargetGoal) Flags() GoalFlags { return GoalTarget }

func (g *zoglinTargetGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.TargetID != 0 || (m.Age+int(m.ID))%hoglinSensorInterval != 0 {
		return false
	}
	follow := m.followRange()
	best, id := math.MaxFloat64, int32(0)
	for i := range t.players {
		p := &t.players[i]
		if d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z); p.Huntable && d <= follow*follow && d < best &&
			m.seesTarget(t.w, p) {
			best, id = d, p.EntityID
		}
	}
	for _, o := range t.others {
		if o == m || !o.Alive() || o.Invulnerable || o.Type == TypeZoglin || o.Type == TypeCreeper {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); d <= follow*follow && d < best &&
			rayClear(t.w, m.X, m.Y+m.eyeHeight(), m.Z, o.X, o.Y+o.eyeHeight(), o.Z) {
			best, id = d, o.ID
		}
	}
	if id == 0 {
		return false
	}
	m.SetMetadataTargetID(id)
	return true
}

func (g *zoglinTargetGoal) Start(*tickView) {}

func (g *zoglinTargetGoal) ShouldContinue(t *tickView) bool { return g.m.targetIn(t) != nil }

func (g *zoglinTargetGoal) Tick(*tickView)               {}
func (g *zoglinTargetGoal) Stop(*tickView)               { g.m.SetMetadataTargetID(0) }
func (g *zoglinTargetGoal) RequiresEveryTick() bool      { return false }
func (g *hoglinTargetGoal) RequiresEveryTick() bool      { return false }
func (g *hoglinRepelGoal) RequiresEveryTick() bool       { return false }
func (g *hoglinRepelGoal) Priority() int                 { return g.priority }
func (g *hoglinRepelGoal) Flags() GoalFlags              { return GoalMove }
func (g *hoglinRepelGoal) Start(*tickView)               {}
func (g *hoglinRepelGoal) Stop(*tickView)                { g.m.stopSeeking() }
func (g *hoglinRepelGoal) Tick(*tickView)                {}
func (g *hoglinRepelGoal) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }

// hoglinRepelGoal is SetWalkTargetAwayFrom.pos(NEAREST_REPELLENT, pace, 8):
// within eight blocks of a repellent the mob walks away from it. The piglin
// shares it.
type hoglinRepelGoal struct {
	m        *Mob
	priority int
	pace     float64
	// source is the remembered repellent and whether one is set.
	source func() (Waypoint, bool)
	// idle makes the goal wait for a mob with no target, activity or walk
	// (interruptCurrentWalk == false).
	idle func() bool
}

func (g *hoglinRepelGoal) CanStart(t *tickView) bool {
	m := g.m
	rep, seen := g.source()
	if !seen || g.idle != nil && !g.idle() {
		return false
	}
	rx, rz := float64(rep.X)+0.5, float64(rep.Z)+0.5
	if Dist2(m.X, m.Y, m.Z, rx, float64(rep.Y), rz) >= hoglinRepelReachXZ*hoglinRepelReachXZ {
		return false
	}
	return m.walkAwayFrom(t, rx, rz, g.pace)
}

// hoglinStep is the hoglin's brain tick outside the goals: the repellent
// sensor, the pacification it causes and the countdowns.
// vanilla: HoglinSpecificSensor.doTick, BecomePassiveIfMemoryPresent
func (m *Mob) hoglinStep(t *tickView) {
	h := &m.hoglin
	if h.pacified > 0 {
		h.pacified--
	}
	if h.retreat > 0 {
		h.retreat--
	}
	if angry := m.TargetID != 0; angry && !h.wasAngry {
		sound := registry.SoundEntityHoglinAngry
		if m.Type == TypeZoglin {
			sound = registry.SoundEntityZoglinAngry
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: m.SoundVolume(), Pitch: m.VoicePitch()})
		h.wasAngry = true
	} else if !angry {
		h.wasAngry = false
	}
	if h.sensorIn--; h.sensorIn <= 0 {
		h.sensorIn = hoglinSensorInterval
		h.repellent, h.seen = nearestRepellent(t.w, m.cell(), isHoglinRepellent)
	}
	if h.seen {
		h.pacified = hoglinPacifyTicks
		m.SetMetadataTargetID(0)
	}
}

// nearestRepellent is BlockPos.findClosestMatch over repellents: least
// Manhattan distance within 8 sideways and 4 vertically.
func nearestRepellent(w World, at Waypoint, match func(state uint16) bool) (Waypoint, bool) {
	best, found := math.MaxInt32, false
	var out Waypoint
	for dy := -hoglinRepelReachY; dy <= hoglinRepelReachY; dy++ {
		for dx := -hoglinRepelReachXZ; dx <= hoglinRepelReachXZ; dx++ {
			for dz := -hoglinRepelReachXZ; dz <= hoglinRepelReachXZ; dz++ {
				d := abs(dx) + abs(dy) + abs(dz)
				if d >= best {
					continue
				}
				c := Waypoint{at.X + int32(dx), at.Y + int32(dy), at.Z + int32(dz)}
				if match(w.BlockAt(c.X, c.Y, c.Z)) {
					best, out, found = d, c, true
				}
			}
		}
	}
	return out, found
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// hoglinWalkTargetValue is Hoglin.getWalkTargetValue: a cell near the
// repellent is worth -1 and one over crimson nylium ten. A zoglin is a
// Monster and takes Monster.getWalkTargetValue.
func (m *Mob) hoglinWalkTargetValue(w World, cell Waypoint) float64 {
	if m.Type != TypeHoglin {
		return monsterWalkTargetValue(w, cell.X, cell.Y, cell.Z)
	}
	if r := m.hoglin.repellent; m.hoglin.seen {
		dx, dy, dz := float64(r.X-cell.X), float64(r.Y-cell.Y), float64(r.Z-cell.Z)
		if dx*dx+dy*dy+dz*dz < hoglinRepelReachXZ*hoglinRepelReachXZ {
			return -1
		}
	}
	if registry.BlockOf(w.BlockAt(cell.X, cell.Y-1, cell.Z)) == registry.BlockCrimsonNylium {
		return hoglinNylium
	}
	return 0
}

// hoglinConversionStep is Hoglin.customServerAiStep: a non-immune hoglin
// where piglins zombify counts up and becomes a zoglin past 300 ticks;
// elsewhere the count resets.
func (m *Mob) hoglinConversionStep(w World) MobType {
	h := &m.hoglin
	if m.Type != TypeHoglin || m.ZombificationImmune || !w.PiglinsZombify() {
		h.inOverworld = 0
		return NoMobType
	}
	if h.inOverworld++; h.inOverworld > hoglinConvertTicks {
		return TypeZoglin
	}
	return NoMobType
}

// hoglinBlow is HoglinBase.hurtAndThrowTarget: an adult deals half its
// attack plus up to its attack and throws the target; a baby deals a flat
// half point.
// vanilla: HoglinBase.hurtAndThrowTarget, throwTarget
func (m *Mob) hoglinBlow(ev *MobEvent) {
	b := &HoglinBlow{Zoglin: m.Type == TypeZoglin}
	ev.Hoglin = b
	if m.IsBaby() {
		ev.Damage = hoglinBabyAttack
		return
	}
	attack := m.attackDamage()
	ev.Damage = rules.HoglinBlowDamage(attack, m.Rand.Intn(max(int(attack), 1)))
	b.Throw = true
	b.Angle = float64(m.Rand.Intn(21) - 10)
	b.H = float64(m.Rand.Float32()*0.5 + 0.2)
	b.V = float64(m.Rand.Float32()) * 0.5
}

// hoglinInterval is the cooldown a blow buys: forty ticks for an adult,
// fifteen for a baby.
func (m *Mob) hoglinInterval() int {
	if m.IsBaby() {
		return hoglinBabyInterval
	}
	return hoglinAdultInterval
}
