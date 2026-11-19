package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The piglin's hoglin hunt and retreat: an adult piglin that sees an adult
// hoglin sets itself and the adult piglins nearby on it, then rests from
// hunting for 30 to 120 seconds; struck by a hoglin while hoglins outnumber
// the visible piglins, it runs from the hoglin instead.
// Also here: anger at a player who opened or broke something guarded.
// The ambient sound follows the activity (piglinAmbient).
// vanilla: StartHuntingHoglin, PiglinAi.wasHurtBy, PiglinSpecificSensor

const (
	// piglinSenseInterval is the sensor's scan rate.
	// vanilla: Sensor.DEFAULT_SCAN_RATE
	piglinSenseInterval = 20
	// piglinHuntRestMin and piglinHuntRestMax bound HUNTED_RECENTLY in ticks:
	// TIME_BETWEEN_HUNTS is 30 to 120 seconds.
	// vanilla: PiglinAi.TIME_BETWEEN_HUNTS
	piglinHuntRestMin = 600
	piglinHuntRestMax = 2400
	// piglinRetreatMin and piglinRetreatMax bound AVOID_TARGET in ticks:
	// RETREAT_DURATION is 5 to 20 seconds.
	// vanilla: PiglinAi.RETREAT_DURATION
	piglinRetreatMin = 100
	piglinRetreatMax = 400
	// piglinRetreatSoundRange is the distance to the avoided body inside
	// which the retreat sound plays.
	// vanilla: PiglinAi.isNearAvoidTarget
	piglinRetreatSoundRange = 12.0
	// piglinBruteGrowl is the per-tick chance a fighting brute growls.
	// vanilla: PiglinBruteAi.maybePlayActivitySound
	piglinBruteGrowl = 0.0125
	// priorityRetreat ranks the retreat under admiring and above the fight.
	priorityRetreat = 1
	// piglinBabyFlee is a struck baby's AVOID_TARGET in ticks.
	// vanilla: PiglinAi.BABY_FLEE_DURATION_AFTER_GETTING_HIT
	piglinBabyFlee = 100
)

// piglinActivity ranks upward: the later, the higher in updateActivity.
type piglinActivity uint8

const (
	activityIdle piglinActivity = iota
	activityRide
	activityCelebrate
	activityAvoid
	activityFight
	activityAdmire
)

// huntState is the hunt and retreat memories of a piglin.
type huntState struct {
	// huntRest counts down HUNTED_RECENTLY; a piglin with it set starts no
	// hoglin hunt.
	huntRest int
	// retreat counts down AVOID_TARGET and from is the hoglin it names.
	retreat int
	from    int32
	// activity is the last step's activity, for the sound that plays when it
	// changes.
	activity piglinActivity
	// activityKnown is set once the first step has run: vanilla's previous
	// activity is null then, so the first update always counts as a change.
	activityKnown bool
	// ambient is the piglin's ambient sound for its current activity, set
	// each step.
	ambient registry.Sound
	// provoker is the player whose opened container or broken gold block
	// angers this piglin at its next step; 0 when none. needSight makes the
	// piglin first have to see the player.
	provoker  int32
	needSight bool
	// repellent is NEAREST_REPELLENT and repelSeen whether it is set.
	repellent Waypoint
	repelSeen bool
	celebrateState
	rideState
	spearState
}

// piglinProvokeReach is how far from the player a piglin still notices a
// guarded block being opened or broken.
// vanilla: PiglinAi.angerNearbyPiglins
const piglinProvokeReach = 16.0

// Provoke is PiglinAi.angerNearbyPiglins for one piglin: a player at (x, y, z)
// opened a guarded container (needSight) or broke a guarded block. The brute
// is not a Piglin and takes no part. The check of the piglin's activity and
// sight runs at its next step, from the player's position then; a second
// provocation before that step replaces the first.
func (m *Mob) Provoke(id int32, x, y, z, width, height float64, needSight bool) {
	if m.Type != TypePiglin || !m.Alive() || !boxesOverlap(
		x, y-piglinProvokeReach, z, width+2*piglinProvokeReach, height+2*piglinProvokeReach,
		m.X, m.Y, m.Z, m.width(), m.height()) {
		return
	}
	m.barter.provoker, m.barter.needSight = id, needSight
}

// takeProvocation angers an idle piglin at the provoker, or with universal
// anger at the nearest player it sees.
// vanilla: PiglinAi.angerNearbyPiglins, setAngerTarget, isIdle
func (m *Mob) takeProvocation(t *tickView) {
	h := &m.barter
	id, needSight := h.provoker, h.needSight
	h.provoker = 0
	p := m.targetByID(t, id)
	if p == nil || m.TargetID != 0 || h.admiring > 0 || h.retreat > 0 ||
		needSight && !m.seesTarget(t.w, p) {
		return
	}
	if t.w.BoolRule(world.RuleUniversalAnger) {
		if near := m.nearestSeenPlayer(t); near != nil {
			p = near
		}
	}
	if p.Huntable {
		m.setAnger(p.EntityID)
	}
}

// nearestSeenPlayer is the nearest huntable player the piglin sees within its
// follow range, or nil.
// vanilla: PiglinAi.getNearestVisibleTargetablePlayer
func (m *Mob) nearestSeenPlayer(t *tickView) *Player {
	best, near := m.followRange()*m.followRange(), (*Player)(nil)
	for i := range t.players {
		q := &t.players[i]
		// The first of equally near players wins.
		if d := Dist2(m.X, m.Y, m.Z, q.X, q.Y, q.Z); q.Huntable && (d < best || near == nil && d <= best) &&
			m.seesTarget(t.w, q) {
			best, near = d, q
		}
	}
	return near
}

// canHunt is Piglin.canHunt: neither the brute, a baby nor a piglin saved
// with CannotHunt hunts.
func (m *Mob) canHunt() bool { return m.Type == TypePiglin && !m.IsBaby() && !m.barter.CannotHunt }

// adultHoglin is a living adult hoglin, which VISIBLE_ADULT_HOGLIN_COUNT
// counts whether or not it may be hunted.
func adultHoglin(o *Mob) bool { return o.Type == TypeHoglin && o.Alive() && !o.IsBaby() }

// huntableHoglin is Hoglin.canBeHunted.
func huntableHoglin(o *Mob) bool { return adultHoglin(o) && !o.hoglin.CannotBeHunted }

// sees reports whether the piglin sees another body within its follow range,
// the reach of the nearest-entities sensor (16, the brute's 12).
// vanilla: NearestLivingEntitySensor.doTick
func (m *Mob) sees(t *tickView, o *Mob) bool {
	r := m.followRange()
	return Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) <= r*r &&
		rayClear(t.w, m.X, m.Y+m.eyeHeight(), m.Z, o.X, o.Y+o.eyeHeight(), o.Z)
}

// nearestSeen finds the nearest other living mob within distSq that passes
// accept (nil accepts all), checking distance before raycasting.
func (m *Mob) nearestSeen(t *tickView, distSq float64, accept func(*Mob) bool) *Mob {
	var best *Mob
	for _, o := range t.others {
		if o == m || !o.Alive() || accept != nil && !accept(o) {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); d <= distSq && m.sees(t, o) {
			best, distSq = o, d
		}
	}
	return best
}

// visibleCounts counts visible adult hoglins and adult bodies accepted by
// piglin, bar this one.
// vanilla: PiglinSpecificSensor.doTick, HoglinSpecificSensor.doTick
func (m *Mob) visibleCounts(t *tickView, piglin func(*Mob) bool) (hoglins, piglins int) {
	for _, o := range t.others {
		if o == m || !o.Alive() || o.Type == TypePiglin && o.IsBaby() ||
			!adultHoglin(o) && !piglin(o) || !m.sees(t, o) {
			continue
		}
		if piglin(o) {
			piglins++
		} else {
			hoglins++
		}
	}
	return hoglins, piglins
}

// hoglinsOutnumber is PiglinAi.hoglinsOutnumberPiglins, the piglin counting
// itself.
func (m *Mob) hoglinsOutnumber(t *tickView) bool {
	hoglins, piglins := m.visibleCounts(t, (*Mob).isPiglin)
	return hoglins > piglins+1
}

// huntNemesisGoal is the nemesis branch of PiglinAi.findNearestValidAttackTarget:
// an adult piglin with no target attacks the nearest visible wither skeleton.
type huntNemesisGoal struct {
	m        *Mob
	priority int
}

func (g *huntNemesisGoal) Priority() int           { return g.priority }
func (g *huntNemesisGoal) Flags() GoalFlags        { return GoalTarget }
func (g *huntNemesisGoal) Start(*tickView)         {}
func (g *huntNemesisGoal) Tick(*tickView)          {}
func (g *huntNemesisGoal) Stop(*tickView)          { g.m.SetMetadataTargetID(0) }
func (g *huntNemesisGoal) RequiresEveryTick() bool { return false }

func (g *huntNemesisGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.IsBaby() || m.TargetID != 0 || m.barter.retreat > 0 || m.barter.admiring > 0 ||
		(m.Age+int(m.ID))%piglinSenseInterval != 0 || m.nearZombified(t) {
		return false
	}
	var prey *Mob
	best := math.MaxFloat64
	for _, o := range t.others {
		if o.Type != TypeWitherSkeleton || !o.Alive() || !m.sees(t, o) {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); d < best {
			best, prey = d, o
		}
	}
	if prey == nil {
		return false
	}
	m.SetMetadataTargetID(prey.ID)
	return true
}

func (g *huntNemesisGoal) ShouldContinue(t *tickView) bool {
	o := t.mobByID(g.m.TargetID)
	return o != nil && o.Type == TypeWitherSkeleton && o.Alive() && g.m.inFollowRange(&Player{X: o.X, Y: o.Y, Z: o.Z})
}

// huntHoglinGoal is StartHuntingHoglin: the nearest visible adult hoglin
// becomes the target of the piglin and of the adult piglins around it.
type huntHoglinGoal struct {
	m        *Mob
	priority int
}

func (g *huntHoglinGoal) Priority() int           { return g.priority }
func (g *huntHoglinGoal) Flags() GoalFlags        { return GoalTarget }
func (g *huntHoglinGoal) Start(*tickView)         {}
func (g *huntHoglinGoal) Tick(*tickView)          {}
func (g *huntHoglinGoal) Stop(*tickView)          { g.m.SetMetadataTargetID(0) }
func (g *huntHoglinGoal) RequiresEveryTick() bool { return false }

func (g *huntHoglinGoal) CanStart(t *tickView) bool {
	m := g.m
	if !m.canHunt() || m.TargetID != 0 || m.barter.huntRest > 0 || m.barter.retreat > 0 || m.barter.admiring > 0 ||
		(m.Age+int(m.ID))%piglinSenseInterval != 0 || m.nearZombified(t) {
		return false
	}
	var prey *Mob
	best := math.MaxFloat64
	for _, o := range t.others {
		if o == m || !o.Alive() || !m.sees(t, o) {
			continue
		}
		if o.isPiglin() && o.barter.huntRest > 0 {
			return false
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); huntableHoglin(o) && d < best {
			best, prey = d, o
		}
	}
	if prey == nil {
		return false
	}
	m.SetMetadataTargetID(prey.ID)
	m.barter.huntRest = m.huntRestTicks()
	m.broadcastAnger(t, prey.ID)
	return true
}

// broadcastAnger is PiglinAi.broadcastAngerTarget: every adult piglin and
// brute whose box meets this piglin's box grown by the follow range takes the
// target up unless its own is nearer. A hoglin goes only to piglins that can
// hunt, and only when it can be hunted.
func (m *Mob) broadcastAnger(t *tickView, id int32) {
	target := m.targetByID(t, id)
	if target == nil {
		return
	}
	hoglin := t.mobByID(id)
	if hoglin != nil && hoglin.Type != TypeHoglin {
		hoglin = nil
	}
	for _, o := range t.others {
		if o == m || !o.Alive() || !o.isPiglin() || o.IsBaby() || !m.nearbyBox(o) {
			continue
		}
		if hoglin != nil && (!o.canHunt() || !huntableHoglin(hoglin)) {
			continue
		}
		o.angerIfCloser(t, id, target, hoglin != nil)
	}
}

// nearbyBox is the NEAREST_LIVING_ENTITIES query: o's box meets this body's
// box inflated by the follow range on every axis (12 for the brute).
// vanilla: NearestLivingEntitySensor.doTick
func (m *Mob) nearbyBox(o *Mob) bool {
	return m.boxNear(o, m.followRange())
}

// angerIfCloser is PiglinAi.setAngerTargetIfCloserThanCurrent: the current
// target stays unless the new one is strictly nearer. A hoglin target also
// starts the hunt rest.
func (m *Mob) angerIfCloser(t *tickView, id int32, target *Player, hoglin bool) {
	if m.TargetID == id {
		return
	}
	if cur := m.targetByID(t, m.TargetID); cur != nil &&
		Dist2(m.X, m.Y, m.Z, cur.X, cur.Y, cur.Z) < Dist2(m.X, m.Y, m.Z, target.X, target.Y, target.Z) {
		return
	}
	m.setAnger(id)
	if hoglin {
		m.barter.huntRest = m.huntRestTicks()
	}
}

func (m *Mob) setAnger(id int32) {
	m.SetMetadataTargetID(id)
	m.alerted = id
}

func (m *Mob) huntRestTicks() int {
	return piglinHuntRestMin + m.Rand.Intn(piglinHuntRestMax-piglinHuntRestMin+1)
}

func (g *huntHoglinGoal) ShouldContinue(t *tickView) bool {
	o := t.mobByID(g.m.TargetID)
	return o != nil && o.Type == TypeHoglin && o.Alive() && g.m.inFollowRange(&Player{X: o.X, Y: o.Y, Z: o.Z})
}

// piglinHurtByGoal is HurtByTargetGoal with PiglinAi.wasHurtBy in front: a
// piglin struck by a hoglin that outnumbers the visible piglins retreats
// instead of fighting back, and calls the piglins it sees to do the same.
type piglinHurtByGoal struct{ *hurtByGoal }

func (g *piglinHurtByGoal) CanStart(t *tickView) bool {
	g.endCelebrationIfHurt(t)
	if g.babyFlees(t) || g.retreatIfOutnumbered(t) {
		return false
	}
	return g.hurtByGoal.CanStart(t)
}

// babyFlees is the baby branch of PiglinAi.wasHurtBy: a struck baby avoids
// the attacker for 100 ticks, calls the piglins nearby on it and never fights
// back.
func (g *piglinHurtByGoal) babyFlees(t *tickView) bool {
	m := g.m
	if !m.IsBaby() || m.retaliate == 0 || !g.canHunt(t, m.retaliate) {
		return false
	}
	id := m.retaliate
	m.retaliate = 0
	m.alerted = 0
	m.SetMetadataTargetID(0)
	m.stopSeeking()
	m.barter.from, m.barter.retreat = id, piglinBabyFlee
	m.broadcastAnger(t, id)
	return true
}

// endCelebrationIfHurt erases the celebration on any blow but a piglin's,
// whether or not the attacker can then be hunted; an alert alone leaves it.
// vanilla: PiglinAi.wasHurtBy
func (g *piglinHurtByGoal) endCelebrationIfHurt(t *tickView) {
	if g.m.retaliate == 0 {
		return
	}
	if o := t.mobByID(g.m.retaliate); o != nil && g.ignore != nil && g.ignore(o.Type) {
		return
	}
	g.m.endCelebration()
}

// Start spreads the grudge to the nearby piglins, nearer attackers winning.
// With universal anger a player's blow angers the struck piglin and every
// adult piglin near it at the nearest player each one sees instead.
// vanilla: PiglinAi.maybeRetaliate, broadcastUniversalAnger
func (g *piglinHurtByGoal) Start(t *tickView) {
	m := g.m
	if attacker := t.playerByID(m.retaliate); attacker != nil && t.w.BoolRule(world.RuleUniversalAnger) {
		for _, o := range t.others {
			if o != m && o.Alive() && o.isPiglin() && !o.IsBaby() && m.nearbyBox(o) {
				if p := o.nearestSeenPlayer(t); p != nil {
					o.setAnger(p.EntityID)
				}
			}
		}
		if p := m.nearestSeenPlayer(t); p != nil {
			attacker = p
		}
		m.SetMetadataTargetID(attacker.EntityID)
		m.retaliate = 0
		m.alerted = 0
		return
	}
	if m.retaliate != 0 {
		m.broadcastAnger(t, m.retaliate)
	}
	g.hurtByGoal.Start(t)
}

// ShouldContinue also runs the retreat check: while this goal holds the
// target, the selector never offers it a start, so a piglin fighting its
// last attacker would ignore a hoglin's bite.
func (g *piglinHurtByGoal) ShouldContinue(t *tickView) bool {
	if g.retreatIfOutnumbered(t) {
		return false
	}
	return g.hurtByGoal.ShouldContinue(t)
}

// retreatIfOutnumbered is the hoglin branch of PiglinAi.wasHurtBy. The struck
// piglin lets go of what it admires; the piglins it sees only change what
// they avoid.
func (g *piglinHurtByGoal) retreatIfOutnumbered(t *tickView) bool {
	m := g.m
	if m.retaliate == 0 {
		return false
	}
	o := t.mobByID(m.retaliate)
	if o == nil || o.Type != TypeHoglin || m.Type != TypePiglin || m.IsBaby() || !m.hoglinsOutnumber(t) {
		return false
	}
	m.retaliate, m.alerted = 0, 0
	m.retreatFrom(t, o)
	for _, p := range t.others {
		if p != m && p.Type == TypePiglin && p.Alive() && m.sees(t, p) {
			p.retreatFrom(t, o)
		}
	}
	return true
}

// retreatFrom is PiglinAi.retreatFromNearestTarget into
// setAvoidTargetAndDontHuntForAWhile: the nearest of the avoided hoglin, the
// attack target and the new one is avoided. An admiring piglin keeps
// admiring: the admire activity outranks the avoid one. A target that is not
// a hoglin cannot be avoided here.
func (m *Mob) retreatFrom(t *tickView, from *Mob) {
	nearest := from
	for _, id := range [2]int32{m.retreatTarget(), m.TargetID} {
		o := t.mobByID(id)
		if o != nil && o.Type == TypeHoglin && o.Alive() &&
			Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) < Dist2(m.X, m.Y, m.Z, nearest.X, nearest.Y, nearest.Z) {
			nearest = o
		}
	}
	m.SetMetadataTargetID(0)
	m.stopSeeking()
	m.barter.huntRest = m.huntRestTicks()
	m.barter.from = nearest.ID
	m.barter.retreat = piglinRetreatMin + m.Rand.Intn(piglinRetreatMax-piglinRetreatMin+1)
}

// retreatTarget is the hoglin the piglin is avoiding, 0 when it avoids none.
func (m *Mob) retreatTarget() int32 {
	if m.barter.retreat > 0 {
		return m.barter.from
	}
	return 0
}

// retreatGoal is the avoid activity: walk away from the hoglin until the
// timer runs out or the piglins equal the hoglins in number.
// vanilla: PiglinAi.initRetreatActivity, wantsToStopFleeing
type retreatGoal struct{ m *Mob }

func (g *retreatGoal) Priority() int           { return priorityRetreat }
func (g *retreatGoal) Flags() GoalFlags        { return GoalMove }
func (g *retreatGoal) RequiresEveryTick() bool { return false }
func (g *retreatGoal) Tick(t *tickView)        { g.flee(t) }

func (g *retreatGoal) CanStart(t *tickView) bool {
	m := g.m
	m.babyAvoidNemesis(t)
	m.avoidZombified(t)
	m.dropTargetNearZombified(t)
	return m.barter.retreat > 0 && m.targetByID(t, m.barter.from) != nil
}

// ShouldContinue ends a hoglin retreat when the piglins stop being
// outnumbered; any other avoided body is fled until the timer runs out.
// vanilla: PiglinAi.wantsToStopFleeing
func (g *retreatGoal) ShouldContinue(t *tickView) bool {
	m := g.m
	m.avoidZombified(t)
	m.dropTargetNearZombified(t)
	if m.barter.retreat <= 0 || m.targetByID(t, m.barter.from) == nil {
		return false
	}
	if o := t.mobByID(m.barter.from); o != nil {
		switch {
		case o.Type == TypeHoglin:
			return m.hoglinsOutnumber(t)
		case isZombifiedMob(o):
			return m.nearestZombified(t, math.MaxFloat64) == o
		}
	}
	return true
}

// isZombifiedMob is PiglinAi.isZombified.
func isZombifiedMob(o *Mob) bool {
	return o.Type == TypeZombifiedPiglin || o.Type == TypeZoglin
}

// nearestZombified is the nearest seen zombified piglin or zoglin within
// distSq, the piglin's NEAREST_VISIBLE_ZOMBIFIED memory.
// vanilla: PiglinSpecificSensor.doTick
func (m *Mob) nearestZombified(t *tickView, distSq float64) *Mob {
	return m.nearestSeen(t, distSq, isZombifiedMob)
}

// zombifiedNear is the largest squared distance below 6 blocks, the reach of
// PiglinAi.isNearZombified (nearestSeen's bound is inclusive).
var zombifiedNear = math.Nextafter(36, 0)

// nearZombified is PiglinAi.isNearZombified: a piglin (not a brute) sees a
// zombified mob within 6 blocks.
func (m *Mob) nearZombified(t *tickView) bool {
	return m.Type == TypePiglin && m.nearestZombified(t, zombifiedNear) != nil
}

// dropTargetNearZombified erases the attack target while a zombified mob is
// within 6 blocks.
// vanilla: PiglinAi.initFightActivity (EraseMemoryIf isNearZombified)
func (m *Mob) dropTargetNearZombified(t *tickView) {
	if m.Type == TypePiglin && m.TargetID != 0 && m.nearZombified(t) {
		m.SetMetadataTargetID(0)
		m.stopSeeking()
	}
}

// avoidZombified avoids a zombified mob within 6 blocks for 5 to 7 seconds,
// renewed while it stays near.
// vanilla: PiglinAi.avoidZombified
func (m *Mob) avoidZombified(t *tickView) {
	if m.Type != TypePiglin || (m.Age+int(m.ID))%piglinSenseInterval != 0 {
		return
	}
	if z := m.nearestZombified(t, zombifiedNear); z != nil {
		m.barter.from, m.barter.retreat = z.ID, 100+m.Rand.Intn(41)
	}
}

// babyAvoidNemesis makes a baby avoid the nearest visible wither skeleton for
// 5 to 7 seconds, renewed while it stays in sight.
// vanilla: PiglinAi.babyAvoidNemesis
func (m *Mob) babyAvoidNemesis(t *tickView) {
	if !m.IsBaby() || (m.Age+int(m.ID))%piglinSenseInterval != 0 {
		return
	}
	var prey *Mob
	best := math.MaxFloat64
	for _, o := range t.others {
		if o.Type != TypeWitherSkeleton || !o.Alive() || !m.sees(t, o) {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); d < best {
			best, prey = d, o
		}
	}
	if prey != nil {
		m.barter.from, m.barter.retreat = prey.ID, 100+m.Rand.Intn(41)
	}
}

func (g *retreatGoal) Start(t *tickView) {
	m := g.m
	if o := m.targetByID(t, m.barter.from); o != nil &&
		Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) < piglinRetreatSoundRange*piglinRetreatSoundRange {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityPiglinRetreat, Volume: 1, Pitch: m.VoicePitch()})
	}
	g.flee(t)
}

func (g *retreatGoal) Stop(*tickView) {
	g.m.barter.retreat = 0
	g.m.stopSeeking()
}

// flee picks a cell away from the hoglin when the walk has run out.
// vanilla: SetWalkTargetAwayFrom.entity
func (g *retreatGoal) flee(t *tickView) {
	m := g.m
	if o := m.targetByID(t, m.barter.from); o != nil && len(m.navOf().nav.path) == 0 {
		m.walkAwayFrom(t, o.X, o.Z, 1.0)
	}
}

// piglinSounds plays the sounds a piglin makes on changing activity and runs
// the hunt and retreat countdowns.
// vanilla: PiglinAi.updateActivity, getSoundForActivity
func (m *Mob) piglinSounds(t *tickView) {
	h := &m.barter
	if h.huntRest > 0 {
		h.huntRest--
	}
	if h.retreat > 0 {
		h.retreat--
	}
	m.rideStep(t)
	angry := m.TargetID != 0
	act := m.piglinActivity()
	if act != h.activity || !h.activityKnown {
		h.activity, h.activityKnown = act, true
		if sound := m.piglinActivitySound(t, act); sound != registry.SoundNone {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: 1, Pitch: m.VoicePitch()})
		}
	}
	h.ambient = m.piglinActivitySound(t, act)
	if angry && m.Type == TypePiglinBrute && m.Rand.Float64() < piglinBruteGrowl {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityPiglinBruteAngry, Volume: 1, Pitch: m.VoicePitch()})
	}
}

// piglinActivity is the active activity in PiglinAi.updateActivity's ranking.
// vanilla: PiglinAi.updateActivity
func (m *Mob) piglinActivity() piglinActivity {
	b := &m.barter
	switch {
	case b.admiring > 0:
		return activityAdmire
	case m.TargetID != 0:
		return activityFight
	case b.retreat > 0:
		return activityAvoid
	case m.celebrating():
		return activityCelebrate
	case b.ride != 0:
		return activityRide
	}
	return activityIdle
}

// piglinActivitySound is the sound for an activity: the one played on
// entering it and the ambient while it holds. The brute makes only its angry
// sound, on entering the fight.
// vanilla: PiglinAi.getSoundForActivity, PiglinBruteAi.getSoundForActivity
func (m *Mob) piglinActivitySound(t *tickView, act piglinActivity) registry.Sound {
	if m.Type != TypePiglin {
		if m.Type == TypePiglinBrute && act == activityFight {
			return registry.SoundEntityPiglinBruteAngry
		}
		return registry.SoundNone
	}
	h := &m.barter
	switch {
	case act == activityFight:
		return registry.SoundEntityPiglinAngry
	case h.inOverworld > 0:
		return registry.SoundEntityPiglinRetreat
	case act == activityAvoid && m.nearAvoided(t):
		return registry.SoundEntityPiglinRetreat
	case act == activityAdmire:
		return registry.SoundEntityPiglinAdmiringItem
	case act == activityCelebrate:
		return registry.SoundEntityPiglinCelebrate
	case m.seesPlayerHoldingLoved(t):
		return registry.SoundEntityPiglinJealous
	case h.repelSeen:
		return registry.SoundEntityPiglinRetreat
	}
	return registry.SoundEntityPiglinAmbient
}

// nearAvoided reports whether the body the piglin avoids is within the
// retreat sound's range.
// vanilla: PiglinAi.isNearAvoidTarget
func (m *Mob) nearAvoided(t *tickView) bool {
	o := t.mobByID(m.barter.from)
	return o != nil && Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) < piglinRetreatSoundRange*piglinRetreatSoundRange
}

// seesPlayerHoldingLoved is NEAREST_PLAYER_HOLDING_WANTED_ITEM being set.
// vanilla: PiglinSpecificSensor.doTick
func (m *Mob) seesPlayerHoldingLoved(t *tickView) bool {
	r := m.followRange()
	for i := range t.players {
		p := &t.players[i]
		if !p.Spectator && isPlayerHoldingLovedItem(p) &&
			Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) <= r*r && m.seesTarget(t.w, p) {
			return true
		}
	}
	return false
}

// isPlayerHoldingLovedItem is PiglinAi.isPlayerHoldingLovedItem.
func isPlayerHoldingLovedItem(p *Player) bool {
	return isLoved(p.HeldItem) || isLoved(p.OffHandItem)
}
