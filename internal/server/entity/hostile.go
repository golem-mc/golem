package entity

import (
	"cmp"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The hostile machinery every monster reuses: its goals, the daylight burn
// and the idleness despawn. The target goals (NearestAttackableTargetGoal
// for players, HurtByTargetGoal) hold TargetID and revalidate it each tick,
// dropping a target that died, changed mode, left the follow range or the
// dimension, as canContinueToUse does. The attack is ZombieAttackGoal
// (MeleeAttackGoal, priority 3); its swing is reported as a raw event that
// the server runs through the player-hit pipeline.

// GoalTarget is the target goals' control, one holder at a time, so the
// retaliation can take over the hunt's target and the hunt resumes after.
const GoalTarget GoalFlags = 1 << 3

// The zombie's goal priorities, vanilla's numbers: HurtByTargetGoal 1,
// NearestAttackableTargetGoal(Player) 2, ZombieAttackGoal 3,
// WaterAvoidingRandomStroll 7, the looks 8.
const (
	priorityHurtBy  = 1
	priorityNearest = 2
	// priorityGolemTarget is the priority of the skeleton's golem, turtle and piglin targets.
	priorityGolemTarget = 3
	priorityMelee       = 3
	hostileStroll       = 7
	hostileLookAt       = 8
	hostileLookAround   = 8

	// AbstractSkeleton.registerGoals: the zombie's target pair,
	// RestrictSunGoal 2, FleeSunGoal 3, ranged attack 4, stroll 5, looks 6.
	// The wolf avoidance is left out.
	priorityRestrictSun = 2
	priorityFleeSun     = 3
	priorityRanged      = 4
	skeletonStroll      = 5
	skeletonLookAt      = 6
	skeletonLookAround  = 6
)

// hostileGoals wires a hostile mob's goals at its own class's priorities.
// FloatGoal is registered only where vanilla's class does (the creeper and
// spider at 1); the zombie and skeleton have none, so they walk a pond's
// bottom rather than paddle.
func hostileGoals(m *Mob) []Goal {
	if m.Type == TypeEnderman {
		return endermanGoals(m)
	}
	if m.Type == TypeCreeper {
		// Creeper.registerGoals: FloatGoal 1, NearestAttackableTargetGoal 1,
		// HurtByTargetGoal 2, SwellGoal 2, AvoidEntityGoal 3, MeleeAttackGoal 4,
		// stroll 5, looks 6.
		return []Goal{
			newFloatGoal(m),
			&nearestTargetGoal{m: m, priority: 1},
			&hurtByGoal{m: m, priority: 2},
			&swellGoal{m: m},
			newCreeperAvoidGoal(m, 3),
			&meleeAttackGoal{m: m, priority: 4},
			&randomStroll{m: m, priority: 5},
			&lookAtPlayer{m: m, priority: 6},
			&randomLookAround{m: m, priority: 6},
		}
	}
	if m.is(TypeSpider) {
		// Spider.registerGoals: FloatGoal 1, HurtByTargetGoal 1,
		// Spider$SpiderTargetGoal 2, LeapAtTargetGoal 3,
		// Spider$SpiderAttackGoal 4, stroll 5, looks 6.
		return []Goal{
			newFloatGoal(m),
			&hurtByGoal{m: m, priority: 1},
			&spiderTargetGoal{nearestTargetGoal{m: m, priority: 2}},
			&avoidGoal{m: m, priority: 2, active: func() bool { return true }, maxDist: 6, walk: 1, sprint: 1.2, mobs: []MobType{TypeArmadillo}, mobOK: func(o *Mob) bool { return !o.ArmadilloScared() }},
			&spiderGolemTargetGoal{mobTargetGoal{m: m, priority: 3, victim: TypeIronGolem}},
			&leapAtTargetGoal{m: m, priority: 3},
			&spiderAttackGoal{meleeAttackGoal{m: m, priority: 4, climb: true, followUnseen: true}},
			&randomStroll{m: m, priority: 5},
			&lookAtPlayer{m: m, priority: 6},
			&randomLookAround{m: m, priority: 6},
		}
	}
	if m.Type == TypeSilverfish {
		return silverfishGoals(m)
	}
	if m.Type == TypeEndermite {
		// Endermite.registerGoals: FloatGoal 1,
		// HurtByTargetGoal(this).setAlertOthers 1, MeleeAttackGoal 2, stroll 3,
		// NearestAttackableTargetGoal(Player) 2, looks 7 and 8.
		return []Goal{
			newFloatGoal(m),
			&hurtByGoal{m: m, priority: 1, alertOthers: true},
			&nearestTargetGoal{m: m, priority: 2},
			&meleeAttackGoal{m: m, priority: 2},
			&randomStroll{m: m, priority: 3},
			&lookAtPlayer{m: m, priority: 7},
			&randomLookAround{m: m, priority: 8},
		}
	}
	if m.Type == TypeWitch {
		return witchGoals(m)
	}
	if m.Type == TypeDrowned {
		return drownedGoals(m)
	}
	if m.isCube() {
		return cubeGoals(m)
	}
	if m.is(TypeSkeleton) {
		// AbstractSkeleton.registerGoals: HurtByTargetGoal 1,
		// NearestAttackableTargetGoal(Player, mustSee) 2, RestrictSunGoal 2,
		// FleeSunGoal(this, 1.0) 3, AvoidEntityGoal(Wolf) 3,
		// the bow or melee attack 4, stroll 5, looks 6. The ranged interval
		// follows the difficulty; the world starts at easy.
		// reassessWeaponGoal registers only one attack, chosen by the held
		// weapon on every equipment change; here both are registered and each
		// refuses to run, or stops, while the hand holds the other's weapon.
		// vanilla: AbstractSkeleton.reassessWeaponGoal
		goals := []Goal{
			&hurtByGoal{m: m, priority: priorityHurtBy},
			&nearestTargetGoal{m: m, priority: priorityNearest},
			&restrictSunGoal{m: m},
			&fleeSunGoal{m: m},
			&avoidGoal{m: m, priority: 3, active: func() bool { return true }, maxDist: 6,
				walk: 1.0, sprint: 1.2, mobs: []MobType{TypeWolf}},
			&mobTargetGoal{m: m, priority: priorityGolemTarget, victim: TypeIronGolem},
			&mobTargetGoal{m: m, priority: priorityGolemTarget, kinds: babyTurtleKinds, matches: isBabyTurtleAshore},
			&rangedAttackGoal{m: m, priority: priorityRanged},
			&meleeAttackGoal{m: m, priority: priorityRanged, pace: skeletonMeleePace, refuse: m.holdsBow},
			&randomStroll{m: m, priority: skeletonStroll},
			&lookAtPlayer{m: m, priority: skeletonLookAt},
			&randomLookAround{m: m, priority: skeletonLookAround},
		}
		if m.Type == TypeWitherSkeleton {
			// vanilla: WitherSkeleton.registerGoals; slotted before the priority-4 attack
			piglins := &mobTargetGoal{m: m, priority: priorityGolemTarget, kinds: piglinKinds}
			i := slices.IndexFunc(goals, func(g Goal) bool { return g.Priority() > priorityGolemTarget })
			goals = slices.Insert(goals, i, Goal(piglins))
		}
		return goals
	}
	if m.isPiglin() {
		return piglinMindGoals(m)
	}
	if m.Type == TypeZombifiedPiglin {
		return piglinGoals(m)
	}
	// Zombie.registerGoals (also the husk's and zombie villager's):
	// HurtByTargetGoal(this).setAlertOthers 1,
	// NearestAttackableTargetGoal(Player) 2, SpearUseGoal 2, ZombieAttackGoal 3,
	// MoveThroughVillageGoal 6, stroll 7, looks 8. The alert sends zombies in follow range, except zombified
	// piglins, after the attacker.
	return withZombieTargets(m, []Goal{
		newZombieBreakDoorGoal(m),
		&spearUseGoal{m: m},
		&hurtByGoal{m: m, priority: priorityHurtBy, alertOthers: true},
		&nearestTargetGoal{m: m, priority: priorityNearest},
		&meleeAttackGoal{m: m, priority: priorityMelee},
		&moveThroughVillage{m: m},
		newZombieAttackTurtleEggGoal(m),
		&randomStroll{m: m, priority: hostileStroll},
		&lookAtPlayer{m: m, priority: hostileLookAt},
		&randomLookAround{m: m, priority: hostileLookAround},
	})
}

// withZombieTargets adds the zombie's mob targets to goals in priority order.
func withZombieTargets(m *Mob, goals []Goal, extra ...Goal) []Goal {
	goals = append(append(goals, extra...), zombieMobTargets(m)...)
	slices.SortStableFunc(goals, func(a, b Goal) int { return cmp.Compare(a.Priority(), b.Priority()) })
	return goals
}

// isPrey is the types zombieMobTargets and the drowned's axolotl goal match.
func isPrey(k MobType) bool {
	switch k {
	case TypeVillager, TypeWanderingTrader, TypeIronGolem, TypeTurtle, TypeAxolotl:
		return true
	}
	return false
}

// zombieMobTargets is Zombie.addBehaviourGoals' mob targets: villagers and
// wandering traders unsighted and iron golems at 3, baby turtles ashore at 5.
func zombieMobTargets(m *Mob) []Goal {
	return []Goal{
		&mobTargetGoal{m: m, priority: 3, fromPrey: true, unsighted: true, kinds: abstractVillagerKinds},
		&mobTargetGoal{m: m, priority: 3, fromPrey: true, kinds: ironGolemKinds},
		&mobTargetGoal{m: m, priority: 5, fromPrey: true, kinds: babyTurtleKinds, matches: isBabyTurtleAshore},
	}
}

// targetIn resolves TargetID from the tick's bodies: a still-huntable
// present player, or a live, non-invulnerable mob. A mob target comes back
// in the Player shape, so grudges against mobs reuse the player machinery.
func (m *Mob) targetIn(t *tickView) *Player {
	if m.TargetID == 0 {
		return nil
	}
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == m.TargetID {
			if !t.canAttackPlayer(p) {
				return nil
			}
			return p
		}
	}
	if o := t.mobByID(m.TargetID); o != nil && o.Alive() {
		p := o.asTarget()
		if !p.Huntable {
			return nil
		}
		return &p
	}
	return nil
}

// canAttackPlayer reports whether a mob may target the player (one of
// t.players, not a mob in the Player shape): huntable, and not on Peaceful.
// vanilla: LivingEntity.canAttack
func (t *tickView) canAttackPlayer(p *Player) bool {
	return p.Huntable && t.w.Difficulty() != rules.DifficultyPeaceful
}

// mobByID finds the mob of the tick's herd with the entity ID; the caller
// checks it is alive.
func (t *tickView) mobByID(id int32) *Mob {
	if t.push != nil {
		return t.push.lookupByID(id)
	}
	if t.byID != nil {
		return t.byID[id]
	}
	return mobByID(t.others, id)
}

// mobByID finds the mob with the entity ID; the caller checks it is alive.
func mobByID(others []*Mob, id int32) *Mob {
	for _, o := range others {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// asTarget is another mob seen as a target: feet, entity ID, and huntable
// (an invulnerable mob never is). HeldItem stays empty.
// vanilla: LivingEntity.canBeSeenAsEnemy
func (o *Mob) asTarget() Player {
	return Player{X: o.X, Y: o.Y, Z: o.Z, Width: o.width(), Height: o.height(), EntityID: o.ID, Huntable: !o.Invulnerable && !o.PlayingDead(), Team: o.Team}
}

// nearestTargetGoal is NearestAttackableTargetGoal as the zombie registers
// it: about every ten ticks (randomInterval 10, halved by reducedTickDelay)
// the nearest huntable player in follow range and line of sight becomes the
// target.
type nearestTargetGoal struct {
	m *Mob
	// priority is the goal's own vanilla registration.
	priority int
	// unseen counts the selections the target has been out of sight, TargetGoal's
	// memory before it lets a hidden target go.
	unseen int
	// accept is the goal's own selector predicate, nil for none.
	accept func(t *tickView, p *Player) bool
	// recheck re-asks accept during the hunt, dropping a target that no longer
	// passes.
	recheck bool
	// interval replaces nearestScanInterval when nonzero; gate refuses a found
	// target while false; followShare scales the holding follow range when
	// nonzero.
	// vanilla: NearestAttackableTargetGoal.randomInterval, getFollowDistance
	interval int
	gate     func(t *tickView) bool
	// refuse is asked before the interval roll; true ends the check without
	// spending it.
	refuse      func() bool
	followShare float64
	// memory replaces unseenMemoryTicks where nonzero.
	// vanilla: TargetGoal.setUnseenMemoryTicks
	memory int
}

func (g *nearestTargetGoal) Priority() int    { return g.priority }
func (g *nearestTargetGoal) Flags() GoalFlags { return GoalTarget }

func (g *nearestTargetGoal) CanStart(t *tickView) bool {
	interval := nearestScanInterval
	if g.interval != 0 {
		interval = g.interval
	}
	if g.refuse != nil && g.refuse() {
		return false
	}
	if g.m.TargetID != 0 || g.m.Rand.Intn(reducedTickDelay(interval)) != 0 {
		return false
	}
	p := g.m.nearestHuntable(t, g.accept, true)
	if p == nil || g.gate != nil && !g.gate(t) {
		return false
	}
	g.m.SetMetadataTargetID(p.EntityID)
	return true
}

func (g *nearestTargetGoal) Start(*tickView) { g.unseen = 0 }

// ShouldContinue is TargetGoal.canContinueToUse: present, huntable, in
// follow range, and seen, with lost sight forgiven for the 60-tick unseen
// memory (halved by reducedTickDelay).
func (g *nearestTargetGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	// vanilla: PiglinAi.stopAttackingIfTargetInvalid
	if p == nil || g.m.alliedTo(p) || g.recheck && !g.accept(t, p) || !g.m.inFollowRange(p) || g.followShare != 0 && g.m.distanceToPlayer(p) > g.m.followRange()*g.followShare {
		return false
	}
	if g.m.seesTarget(t.w, p) {
		g.unseen = 0
		return true
	}
	g.unseen++
	return g.unseen <= reducedTickDelay(cmp.Or(g.memory, unseenMemoryTicks))
}

func (g *nearestTargetGoal) Tick(*tickView) {}

func (g *nearestTargetGoal) Stop(*tickView) {
	g.unseen = 0
	// The target goes with the goal: the only other writer is the
	// retaliation, whose start has already run when it pre-empts this.
	g.m.SetMetadataTargetID(0)
}

// RequiresEveryTick is false: revalidation waits for selections.
func (g *nearestTargetGoal) RequiresEveryTick() bool { return false }

// alliedTo reports whether the target shares the mob's scoreboard team.
// vanilla: TargetGoal.canContinueToUse, TargetingConditions.test
func (m *Mob) alliedTo(p *Player) bool { return m.Team != "" && m.Team == p.Team }

// alliedToMob reports whether o shares the mob's scoreboard team.
// vanilla: Entity.isAlliedTo
func (m *Mob) alliedToMob(o *Mob) bool { return m.Team != "" && m.Team == o.Team }

// nearestScanInterval is NearestAttackableTargetGoal's randomInterval of 10.
const nearestScanInterval = 10

// unseenMemoryTicks is TargetGoal's default 60-tick memory of a target out
// of sight.
const unseenMemoryTicks = 60

// hurtByGoal is HurtByTargetGoal: the last living attacker (a player's
// swing, a mob's melee or projectile) becomes the target ahead of any the
// nearest hunt could find. The hurt methods record the attacker's entity ID
// for it.
type hurtByGoal struct {
	m *Mob
	// priority is the goal's own vanilla registration.
	priority int
	// alertOthers is HurtByTargetGoal.setAlertOthers: on taking the grudge,
	// targetless mobs of this kind in follow range are sent after the attacker
	// too.
	alertOthers bool
	// alertTo narrows the mobs an alert reaches where set.
	alertTo func(*Mob) bool
	// ignore is HurtByTargetGoal's exclusion list: kinds whose blows go
	// unanswered. nil answers every kind.
	ignore func(MobType) bool
	// accept applies a kind's canAttack restriction to acquiring and keeping a grudge.
	accept func(*tickView, *Player) bool
	// cub marks PolarBearHurtByTargetGoal: a struck cub alerts the grown bears
	// and drops the grudge, and grown bears are alerted only by a cub.
	cub bool
	// sight makes the alert go out only when the struck mob sees the attacker.
	sight bool
	// drop is asked when the grudge is checked; true ends it.
	drop func() bool
	// brain marks a brain's wasHurtBy, which answers a player's blow even
	// under universal anger.
	// vanilla: PiglinAi.wasHurtBy, HoglinAi.wasHurtBy, Zoglin.hurtServer
	brain bool
}

func (g *hurtByGoal) Priority() int    { return g.priority }
func (g *hurtByGoal) Flags() GoalFlags { return GoalTarget }

// CanStart takes the grudge when the attacker is still huntable
// (HurtByTargetGoal applies the nearest scan's targeting conditions to
// lastHurtByMob). Unlike the nearest hunt it starts even with a target
// already set, so a mob struck by its current target still alerts its
// neighbours. A mob named by an alert adopts that target the same way, so
// the target has an owner that drops it when the attacker is gone. With
// universal anger a player's blow is dropped unanswered.
// vanilla: HurtByTargetGoal.canUse
func (g *hurtByGoal) CanStart(t *tickView) bool {
	if !g.brain && g.m.retaliate != 0 && t.playerByID(g.m.retaliate) != nil && t.w.BoolRule(world.RuleUniversalAnger) {
		g.m.retaliate = 0
	}
	if g.m.retaliate != 0 {
		return g.canHunt(t, g.m.retaliate)
	}
	if g.m.alerted != 0 {
		if !g.canHunt(t, g.m.alerted) {
			// The attacker left before this selection; clear the alert's target
			// too, or the nearest hunt would never name another.
			g.m.SetMetadataTargetID(0)
			g.m.alerted = 0
			return false
		}
		return true
	}
	return false
}

// canHunt reports whether the attacker is present, huntable, not an ally and
// not of an ignored kind.
// vanilla: HurtByTargetGoal.HURT_BY_TARGETING, TargetingConditions.test
func (g *hurtByGoal) canHunt(t *tickView, id int32) bool {
	p := g.m.targetByID(t, id)
	if p == nil || !p.Huntable || g.m.alliedTo(p) || g.m.IsOwnedBy(p.UUID) || g.accept != nil && !g.accept(t, p) {
		return false
	}
	if t.playerByID(id) != nil && !t.canAttackPlayer(p) {
		return false
	}
	if g.ignore != nil {
		if o := t.mobByID(id); o != nil && g.ignore(o.Type) {
			return false
		}
	}
	return true
}

// Start targets the attacker and, if this mob was struck, alerts the
// neighbours. A mob named by an alert just adopts the target without
// spreading it; vanilla alerts only from the struck mob.
func (g *hurtByGoal) Start(t *tickView) {
	attacker := g.m.retaliate
	if attacker == 0 {
		attacker = g.m.alerted
	} else if g.alertOthers || g.cub && g.m.IsBaby() {
		g.alertNearby(t, attacker)
	}
	if g.cub && g.m.IsBaby() {
		g.m.SetMetadataTargetID(0)
		g.m.retaliate = 0
		g.m.alerted = 0
		return
	}
	g.m.setTargetBy(attacker, TargetByHurt)
	g.m.retaliate = 0
	g.m.alerted = 0
}

// alertNearby is HurtByTargetGoal.alertOthers: every targetless mob of the
// struck mob's class or subclasses (getEntitiesOfClass(mob.getClass())) in
// the search box gets Mob.setTarget(attacker), which its own goal adopts on
// its next selection.
func (g *hurtByGoal) alertNearby(t *tickView, attacker int32) {
	for _, o := range t.others {
		// vanilla: HurtByTargetGoal.alertOthers answers a tamable's call only
		// for its own owner's mobs.
		if o == g.m || o.TargetID != 0 || !o.Alive() || !alertedBy(g.m.Type, o.Type) ||
			o.Tamable.Owner != g.m.Tamable.Owner {
			continue
		}
		if !alertReaches(g.m, o) || g.cub && o.IsBaby() || g.alertTo != nil && !g.alertTo(o) {
			continue
		}
		p := g.m.targetByID(t, attacker)
		if g.sight && (p == nil || !g.m.seesTarget(t.w, p)) || p != nil && o.alliedTo(p) {
			continue
		}
		o.setTargetBy(attacker, TargetByHurt)
		o.alerted = attacker
	}
}

// alertedBy is alertOthers' getEntitiesOfClass(this.mob.getClass()): the
// same class or a subclass. The family table flattens the class tree, so a
// struck family base (zombie) calls its whole family and a variant (husk)
// only its own kind.
func alertedBy(striker, other MobType) bool {
	if striker == other {
		return true
	}
	// vanilla: Zombie.addBehaviourGoals setAlertOthers(ZombifiedPiglin.class)
	if other == TypeZombifiedPiglin {
		return false
	}
	return Family(striker) == striker && Family(other) == striker
}

// alertRangeY is alertOthers' vertical reach:
// AABB.unitCubeFromLowerCorner(pos).inflate(follow, 10, follow).
const alertRangeY = 10.0

// alertReaches reports whether o's box meets m's foot cube inflated by the
// follow range sideways and alertRangeY vertically.
func alertReaches(m, o *Mob) bool {
	follow := m.followRange()
	return boxesOverlap(
		m.X+0.5, m.Y-alertRangeY, m.Z+0.5, 2*follow+1, 2*alertRangeY+1,
		o.X, o.Y, o.Z, o.width(), o.height())
}

func (g *hurtByGoal) ShouldContinue(t *tickView) bool {
	if g.drop != nil && g.drop() {
		return false
	}
	p := g.m.targetIn(t)
	return p != nil && !g.m.alliedTo(p) && g.m.inFollowRange(p) && (g.accept == nil || g.accept(t, p))
}

func (g *hurtByGoal) Tick(*tickView) {}

// Stop drops the target with the grudge, or the nearest scan would never
// name another.
func (g *hurtByGoal) Stop(*tickView) { g.m.SetMetadataTargetID(0) }

// RequiresEveryTick is false: the grudge waits for selections.
func (g *hurtByGoal) RequiresEveryTick() bool { return false }

// nearestHuntable is NearestAttackableTargetGoal.findTarget: of the players
// the targeting conditions accept (huntable, within the visibility range feet
// to feet, visible when mustSee), the one whose feet are nearest the mob's
// eyes, so a player in a pit can rank farther despite nearer feet; an exact
// tie goes to the first.
// vanilla: NearestAttackableTargetGoal.findTarget, TargetingConditions.test,
// ServerEntityGetter.getNearestEntity
func (m *Mob) nearestHuntable(t *tickView, accept func(*tickView, *Player) bool, mustSee bool) *Player {
	w, players := t.w, t.players
	var nearest *Player
	var best float64
	for i := range players {
		p := &players[i]
		if !t.canAttackPlayer(p) || m.alliedTo(p) {
			continue
		}
		// The cheap range test runs before the sight ray.
		if m.distanceToPlayer(p) > m.visibilityRange(p) {
			continue
		}
		if accept != nil && !accept(t, p) {
			continue
		}
		if mustSee && !m.seesTarget(w, p) {
			continue
		}
		rank := Dist2(p.X, p.Y, p.Z, m.X, m.Y+m.eyeHeight(), m.Z)
		if nearest == nil || rank < best {
			best, nearest = rank, p
		}
	}
	return nearest
}

// Visibility shares of the follow range: sneaking 0.8; invisibility 0.7
// scaled by armour cover, floored at 0.1. The range is
// floored at 2 blocks so the nearest invisible body still shows.
// vanilla: LivingEntity.getVisibilityPercent,
// TargetingConditions.MIN_VISIBILITY_DISTANCE_FOR_INVISIBLE_TARGET
const (
	sneakVisibility       = 0.8
	invisibilityShare     = 0.7
	invisibleCoverFloor   = float64(float32(0.1)) // vanilla's 0.1f
	minVisibilityDistance = 2.0
)

// visibilityRange is the follow range times the target's visibility
// percent, floored at 2: the reach a scan may name a target in. An opened
// hunt keeps its target over the whole follow range instead.
// vanilla: TargetingConditions.test — max(followRange * modifier, 2.0)
func (m *Mob) visibilityRange(p *Player) float64 {
	return max(m.followRange()*visibilityPercent(m, p), minVisibilityDistance)
}

// visibilityPercent is the share of the follow range the target is visible
// at, using the worn items' default mob_visibility and equippable components.
// Stack component patches are not applied here.
// vanilla: LivingEntity.getVisibilityPercent
func visibilityPercent(m *Mob, p *Player) float64 {
	percent := 1.0
	if p.Sneaking {
		percent *= sneakVisibility
	}
	if p.Invisible {
		percent *= invisibilityShare * max(armourCover(p.Worn), invisibleCoverFloor)
	}
	for slot := range p.Worn {
		stack := &p.Worn[slot]
		if stack.Empty() {
			continue
		}
		visibility, ok := registry.StackMobVisibility(*stack, m.Type.Name())
		if !ok {
			continue
		}
		equippable, ok := registry.StackEquippable(*stack)
		if ok && equippable.Slot == ArmorSlotNames[slot] {
			percent *= float64(visibility)
		}
	}
	return min(max(percent, 0), 10)
}

// armourCover is the share of the four armour slots worn.
// vanilla: LivingEntity.getArmorCoverPercentage
func armourCover(worn [4]protocol.ItemStack) float64 {
	wornCount := 0
	for i := range worn {
		if !worn[i].Empty() {
			wornCount++
		}
	}
	return float64(wornCount) / float64(len(worn))
}

// inFollowRange reports whether the player is within follow range, as
// TargetGoal.canContinueToUse requires.
func (m *Mob) inFollowRange(p *Player) bool {
	return m.distanceToPlayer(p) <= m.followRange()
}

// distanceToPlayer is the feet-to-feet distance (Entity.distanceToSqr).
func (m *Mob) distanceToPlayer(p *Player) float64 {
	return math.Sqrt(Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z))
}

// followRange is the type's follow range (35 for the zombie,
// Zombie.createAttributes; otherwise Mob.createMobAttributes' 16) with
// Mob.finalizeSpawn's bonus added to the base, times the zombie's random
// spawn bonus: the target goals' reach and a hunt path's range.
func (m *Mob) followRange() float64 {
	if m.attrs != nil {
		return m.attrs.Value(registry.AttributeFollowRange)
	}
	return m.BaseFollowRange() * (1 + m.followSpawnBonus) * (1 + m.zombie.FollowBonus)
}

// BaseFollowRange is the follow_range base of the body's type.
func (m *Mob) BaseFollowRange() float64 {
	if k := m.kind().followRange; k > 0 {
		return k
	}
	return 16
}

// attackDamage is one blow's damage: the kind's base (3 for the zombie,
// Zombie.createAttributes) plus the main-hand attack_damage modifier (an
// iron sword's 5).
// vanilla: Mob.doHurtTarget reading ATTACK_DAMAGE,
// LivingEntity.collectEquipmentChanges adding the item's modifiers
func (m *Mob) attackDamage() float64 {
	base := m.kind().attackDamage
	if m.attrs != nil && m.attrs.Has(registry.AttributeAttackDamage) {
		// The map holds the kind's base, the killer bunny's, a cube's size and
		// the main hand's modifier.
		base = m.attrs.Value(registry.AttributeAttackDamage)
		if m.hoglinKind() && m.IsBaby() {
			base += hoglinBabyAttack - m.kind().attackDamage
		}
		return base
	} else {
		if m.hoglinKind() && m.IsBaby() {
			// vanilla: Hoglin.ageBoundaryReached, Zoglin.setBaby
			base = hoglinBabyAttack
		}
		if m.KillerBunny() {
			base += rabbitEvilAttack
		}
	}
	return m.effectAttackDamage(rules.ApproximateAttributeWith(m.MainHand, registry.AttributeAttackDamage, "mainhand", base))
}

// BlowEnchantments applies the main-hand enchantments to a blow on
// victimType: the damage after damage effects, the knockback strength
// (already halved, like the player's extra). bind, when not nil, gives the
// context the blow's bodies for loot conditions.
// vanilla: Mob.doHurtTarget
func (m *Mob) BlowEnchantments(victimType int32, damage float64, bind func(*rules.EnchContext)) (dealt, knockback float64) {
	// vanilla: IronGolem.doHurtTarget bypasses Mob.doHurtTarget.
	if m.Type == TypeIronGolem && len(m.MainHand.Enchantments) == 0 {
		return damage, 0
	}
	base := m.kind().attackKnockback
	if m.attrs != nil {
		base = m.attrs.Value(registry.AttributeAttackKnockback)
	}
	if len(m.MainHand.Enchantments) == 0 {
		// vanilla: LivingEntity.getKnockback
		return damage, rules.AttackKnockback(base, false)
	}
	self := m.typeID()
	ctx := rules.EnchContext{
		DamageType: rules.DamageTypeMobAttack, Direct: true,
		Victim: victimType, Attacker: self, DirectAttacker: self,
	}
	if bind != nil {
		bind(&ctx)
	}
	if m.Type == TypeIronGolem {
		return damage, 0
	}
	dealt = rules.EnchantedDamage(m.MainHand, &ctx, damage)
	knockback = rules.AttackKnockback(rules.EnchantedKnockback(m.MainHand, &ctx, base), false)
	return dealt, knockback
}

// seesPlayer is Sensing.hasLineOfSight: whether the ray from the eyes to the
// point is clear of block collision shapes.
func (m *Mob) seesPlayer(w World, x, y, z float64) bool {
	return m.seesPoint(w, x, y+playerEyeHeight, z)
}

// seesTarget is seesPlayer for a target of its own eye height.
func (m *Mob) seesTarget(w World, p *Player) bool {
	return m.seesPoint(w, p.X, p.Y+p.eyeHeight(), p.Z)
}

// seesPoint is seesPlayer for a point given as the target's eye position.
func (m *Mob) seesPoint(w World, x, y, z float64) bool {
	// vanilla: Ravager.hasLineOfSight
	if m.ravager.stun > 0 || m.ravager.roar > 0 {
		return false
	}
	return rayClear(w, m.X, m.Y+m.eyeHeight(), m.Z, x, y, z)
}

// meleeAttackGoal is ZombieAttackGoal(this, 1.0, false) at priority 3: walk
// to the target at the mob's pace, look at it, and strike when in reach,
// seen and cooled down. The aggressive bit (a zombie's raised arms) goes up
// only after five ticks of hunting with the next swing under half the
// interval away.
type meleeAttackGoal struct {
	m *Mob
	// priority is the goal's own vanilla registration.
	priority int
	// climb marks WallClimberNavigation (the spider), whose walk falls back to a
	// straight drive when no path is found.
	climb bool
	// pace replaces meleePace where nonzero.
	pace float64
	// followUnseen is MeleeAttackGoal's followingTargetEvenIfNotSeen: re-path
	// toward an unseen target instead of waiting for sight.
	followUnseen bool
	// interval replaces attackInterval where nonzero.
	interval int
	// brain marks the kinds whose vanilla attack is a brain behaviour, not a
	// MeleeAttackGoal: the hunt outlasts a spent path.
	brain bool
	// refuse, where set, keeps the goal from running while it returns true.
	refuse func() bool
	// swingStatus is the entity event sent with each blow where nonzero.
	swingStatus int32
	// swingSound is played after each blow where set.
	swingSound  registry.Sound
	swingVolume float32
	swingPitch  func() float32
	// modifySwing applies a kind's damage roll to the pending blow.
	modifySwing func(*MobEvent)
	// holdSwing keeps movement running while a brain attack is cooling down.
	holdSwing func() bool
	// onSwing runs with each blow where set.
	onSwing func()
	// polar swaps the swing for PolarBearMeleeAttackGoal.checkAndPerformAttack,
	// which also stands the bear up before the blow.
	polar bool
	// ticksUntilAttack is MeleeAttackGoal's cooldown, 20 ticks per swing.
	ticksUntilAttack int
	// raiseArmTicks is ZombieAttackGoal's count of the ticks the hunt has
	// run.
	raiseArmTicks int
	// ticksUntilNextPathRecalculation is MeleeAttackGoal's countdown between
	// path searches, so an unreachable target is not searched every tick.
	ticksUntilNextPathRecalculation int
	// window is MeleeAttackGoal's lastCanUseCheck.
	window canUseWindow
	// pendingPath is MeleeAttackGoal.path, planned by canUse and handed to the
	// navigation by start. A refusing check leaves it nil, so a refused hunt
	// takes nothing from a running goal.
	pendingPath []Waypoint
	pendingEnd  Waypoint
	// pathedTargetX, pathedTargetY and pathedTargetZ are where the last search
	// aimed; the next waits for the target to move a block from there.
	pathedTargetX, pathedTargetY, pathedTargetZ float64
}

// speed is the pace the hunt walks at.
func (g *meleeAttackGoal) speed() float64 {
	if g.pace != 0 {
		return g.pace
	}
	return meleePace
}

// raiseArmsAfter is ZombieAttackGoal.tick's five hunting ticks before the
// arms may rise.
const raiseArmsAfter = 5

func (g *meleeAttackGoal) Priority() int    { return g.priority }
func (g *meleeAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *meleeAttackGoal) CanStart(t *tickView) bool {
	if g.refuse != nil && g.refuse() {
		return false
	}
	return g.canUse(t, g.m.targetIn(t))
}

// canUse is MeleeAttackGoal.canUse with the target already resolved, so the
// drowned's override can spend the same 20-tick window before its okTarget.
// vanilla: MeleeAttackGoal.canUse
func (g *meleeAttackGoal) canUse(t *tickView, p *Player) bool {
	// vanilla: MeleeAttackGoal.canUse's lastCanUseCheck
	if !g.window.open(g.m.Age) {
		return false
	}
	if p == nil {
		return false
	}
	g.pendingPath, g.pendingEnd = nil, Waypoint{}
	// The opening search plans to the target's cell without a sight check.
	// A body that may plan always gets a path (at worst the empty one), so
	// only a null createPath falls through to the reach test.
	if path, end, ok := g.m.createPath(t.w, p.X, p.Y, p.Z); ok {
		g.pendingPath, g.pendingEnd = path, end
		return true
	}
	return g.m.inMeleeReach(p)
}

func (g *meleeAttackGoal) Start(*tickView) {
	// vanilla: MeleeAttackGoal.start — hand canUse's path to the navigation
	// and set the re-path clock and cooldown ready.
	g.m.SetMetadataAggressive(true)
	g.raiseArmTicks = 0
	g.ticksUntilNextPathRecalculation = 0
	g.ticksUntilAttack = 0
	g.m.moveTo(g.pendingPath, g.pendingEnd, g.speed())
}

// ShouldContinue is MeleeAttackGoal.canContinueToUse: without
// followingTargetEvenIfNotSeen the hunt ends with its path, so canUse's own
// search (to the target's cell, not next to it) walks the body into contact
// again within its 20-tick window.
// vanilla: MeleeAttackGoal.canContinueToUse
func (g *meleeAttackGoal) ShouldContinue(t *tickView) bool {
	if g.m.targetIn(t) == nil || g.refuse != nil && g.refuse() {
		return false
	}
	return g.followUnseen || g.brain || !g.m.navDone()
}

// planTo runs one hunt path search and resets the recalculation clock:
// a base countdown, more for a distant target, and 15 more when no path was
// found. The spider's straight drive counts as a path, so it never pays the
// 15.
// vanilla: MeleeAttackGoal.tick
func (g *meleeAttackGoal) planTo(t *tickView, p *Player) {
	g.pathedTargetX, g.pathedTargetY, g.pathedTargetZ = p.X, p.Y, p.Z
	g.ticksUntilNextPathRecalculation = recalcBase + g.m.Rand.Intn(recalcVary)
	if d := Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z); d > recalcFar*recalcFar {
		g.ticksUntilNextPathRecalculation += 10
	} else if d > recalcMid*recalcMid {
		g.ticksUntilNextPathRecalculation += 5
	}
	reach := int32(navReach)
	if g.climb {
		// vanilla: WallClimberNavigation.moveTo plans to createPath(target, 0)
		reach = 0
	}
	if !g.m.seekTo(t.w, p.X, p.Y, p.Z, g.speed(), reach) {
		g.ticksUntilNextPathRecalculation += recalcNoWay
	}
}

func (g *meleeAttackGoal) Tick(t *tickView) {
	p := g.m.targetIn(t)
	if p == nil {
		return
	}
	// The head is held on the target — vanilla's LookControl.setLookAt.
	dx, dz := p.X-g.m.X, p.Z-g.m.Z
	g.m.HeadYaw = turnToward(g.m.HeadYaw, float32(yawToward(dx, dz)), headTurnSpeed)
	g.m.Pitch = turnToward(g.m.Pitch,
		float32(pitchToward(dx, p.Y+p.eyeHeight()-(g.m.Y+g.m.eyeHeight()), dz)), headTurnSpeed)
	// MeleeAttackGoal's recalculation: wait out the countdown, then search
	// only for a target seen (or followUnseen) that moved a block since the
	// last search, or on vanilla's 1-in-20 re-ask for a still target.
	// vanilla: MeleeAttackGoal.tick
	g.ticksUntilNextPathRecalculation = max(g.ticksUntilNextPathRecalculation-1, 0)
	if g.ticksUntilNextPathRecalculation <= 0 &&
		(g.followUnseen || g.m.seesTarget(t.w, p)) &&
		(Dist2(g.pathedTargetX, g.pathedTargetY, g.pathedTargetZ, p.X, p.Y, p.Z) >= 1.0 ||
			g.m.Rand.Float64() < recalcUnmoved) {
		g.planTo(t, p)
	}

	// The cooldown counts down a tick.
	if g.ticksUntilAttack > 0 {
		g.ticksUntilAttack--
	}
	if g.polar {
		g.polarSwing(t, p)
		return
	}
	// And the swing itself: in reach, seen, cooled down.
	if g.ticksUntilAttack == 0 && (g.holdSwing == nil || !g.holdSwing()) && g.m.inMeleeReach(p) && g.m.seesTarget(t.w, p) {
		g.ticksUntilAttack = attackInterval
		if g.interval != 0 {
			g.ticksUntilAttack = g.interval
		}
		ev := MobEvent{Mob: g.m, Kind: MobAttackedPlayer, Damage: g.m.attackDamage(), Player: *p}
		if g.modifySwing != nil {
			g.modifySwing(&ev)
		}
		if g.m.hoglinKind() {
			g.ticksUntilAttack = g.m.hoglinInterval()
			g.m.hoglinBlow(&ev)
			g.m.hoglinOnHit(t, p.EntityID)
		}
		t.events = append(t.events, ev)
		if g.swingStatus != 0 {
			t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobStatus, Status: g.swingStatus})
		}
		if g.swingSound != registry.SoundNone {
			volume, pitch := g.swingVolume, float32(1)
			if volume == 0 {
				volume = 1
			}
			if g.swingPitch != nil {
				pitch = g.swingPitch()
			}
			t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobSounded, SoundID: g.swingSound, Volume: volume, Pitch: pitch})
		}
		if g.onSwing != nil {
			g.onSwing()
		}
	}
	g.raiseArmTicks++
	if !g.m.is(TypeZombie) {
		// Only ZombieAttackGoal runs the arm clock; every other melee goal
		// raises the arms in Start and lowers them in Stop.
		// vanilla: ZombieAttackGoal.tick, MeleeAttackGoal.start
		return
	}
	g.m.SetMetadataAggressive(g.raiseArmTicks >= raiseArmsAfter && g.ticksUntilAttack < attackInterval/2)
}

func (g *meleeAttackGoal) Stop(*tickView) {
	g.m.SetMetadataAggressive(false)
	g.m.SetMetadataStanding(false)
	g.m.stopSeeking()
}

// RequiresEveryTick is MeleeAttackGoal's override: the cooldown and arm
// count are per tick.
func (g *meleeAttackGoal) RequiresEveryTick() bool { return true }

// attackInterval is resetAttackCooldown's adjustedTickDelay(20).
const attackInterval = 20

// canUseCheckInterval is MeleeAttackGoal.canUse's 20 game ticks between
// opening searches (lastCanUseCheck).
const canUseCheckInterval = 20

// canUseWindow is MeleeAttackGoal.canUse's lastCanUseCheck: a check passes the
// window once per canUseCheckInterval ticks, whatever it decides afterwards.
type canUseWindow struct {
	last    int
	checked bool
}

// open reports whether a check at age may run, and stamps age when it may, so a
// fresh goal's first check passes.
func (w *canUseWindow) open(age int) bool {
	if w.checked && age-w.last < canUseCheckInterval {
		return false
	}
	w.checked, w.last = true, age
	return true
}

// MeleeAttackGoal.tick's path cadence: 4 plus up to 7 ticks per search, 5
// more beyond 16 blocks and 10 beyond 32, 15 more when no path is found,
// and a 1-in-20 re-search for a target that has not moved.
const (
	recalcBase    = 4
	recalcVary    = 7
	recalcMid     = 16
	recalcFar     = 32
	recalcNoWay   = 15
	recalcUnmoved = 0.05
)

// meleePace is ZombieAttackGoal's speed modifier, 1.0.
const meleePace = 1.0

// skeletonMeleePace is AbstractSkeleton.meleeGoal's speed modifier.
const skeletonMeleePace = 1.2

// inMeleeReach is Mob.isWithinMeleeAttackRange: the mob's box inflated
// sideways by the reach meets the target's, and, for a main hand with an
// attack_range, the box inflated by its minimum reach does not. Both reaches
// are the component's times its mob factor; with none in hand, or one the
// stack removes, the reach is Mob.DEFAULT_ATTACK_REACH, built from float
// literals.
// vanilla: Mob.isWithinMeleeAttackRange, AttackRange.effectiveMaxRange
func (m *Mob) inMeleeReach(p *Player) bool {
	w, h, _ := p.box()
	if m.Y >= p.Y+h || p.Y >= m.Y+m.height() {
		return false
	}
	maxReach, minReach := defaultAttackReach, 0.0
	if r, ok := rules.StackAttackRange(m.MainHand); ok {
		maxReach, minReach = float64(r.MaxReach*r.MobFactor), float64(r.MinReach*r.MobFactor)
	}
	within := func(reach float64) bool {
		edge := m.width()/2 + reach + w/2
		return math.Abs(m.X-p.X) < edge && math.Abs(m.Z-p.Z) < edge
	}
	return within(maxReach) && (minReach <= 0 || !within(minReach))
}

// defaultAttackReach is Mob.DEFAULT_ATTACK_REACH, Math.sqrt(2.04f) -
// (double)0.6f: both literals are floats widened to double.
var defaultAttackReach = math.Sqrt(float64(float32(2.04))) - float64(float32(0.6))

// playerWidth and playerHeight are an adult player's 0.6 by 1.8 box.
const (
	playerWidth  = 0.6
	playerHeight = 1.8
)

// burnStep is Mob.isSunBurnTick and burnUndead: where monsters burn, out of
// water and rain, with the eyes under open sky and the light magic value
// past 0.5, the die (random.nextFloat() * 30 against twice the brightness
// past 0.4) may light the mob for igniteForSeconds(8). It rolls every tick,
// so a mob in the sun never burns out. A worn head piece takes the wear
// instead, breaking at its limit.
func (m *Mob) burnStep(w World, events []MobEvent) []MobEvent {
	// vanilla: Mob.isSunBurnTick
	if m.inPowderSnow(w) || m.wasInPowderSnow || m.inRain(w) || rules.IsWater(w.BlockAt(m.cell().X, m.cell().Y, m.cell().Z)) {
		return events
	}
	if !w.MonstersBurn() {
		return events
	}
	x, y, z := m.cell().X, int32(math.Floor(m.Y+m.eyeHeight())), m.cell().Z
	brightness := lightMagicValue(w.MaxLocalRawLightAt(x, y, z))
	if brightness <= 0.5 || float32(m.Rand.Float64())*30 >= (brightness-0.4)*2 {
		return events
	}
	if !canSeeSkyAt(w, x, y, z) {
		return events
	}
	// vanilla: Mob.burnUndead's sun blocker — a damageable head piece takes
	// a point or two of wear per roll and breaks at its limit, letting the
	// fire through from the next roll.
	// vanilla: ZombieHorse.sunProtectionSlot, ZombieNautilus.sunProtectionSlot
	slot, brokeKind := protocol.EquipHead, MobArmourBroke
	if m.Type == TypeZombieHorse || m.Type == TypeZombieNautilus {
		slot, brokeKind = 6, MobBodyBroke
	}
	if stack := m.EquipmentSlot(slot); !stack.Empty() {
		worn, broke := rules.Wear(stack, int32(m.Rand.Intn(2)))
		if broke {
			m.SetEquipment(slot, protocol.ItemStack{})
			events = append(events, MobEvent{Mob: m, Kind: brokeKind})
		} else {
			m.SetEquipment(slot, worn)
		}
		return events
	}
	// The daylight thaws the body. vanilla: Entity.igniteForTicks
	m.IgniteForTicks(burnSeconds * 20)
	return events
}

// burnSeconds is Mob's igniteForSeconds(8).
const burnSeconds = 8

// removeWhenFarAway is Mob.removeWhenFarAway: monsters (not the shulker, a
// golem), water creatures, ambient mobs (bats) and a jockey's chicken leave
// when no player is near; other animals do not (Animal.removeWhenFarAway), so the creature category
// never despawns.
// vanilla: Mob.checkDespawn, Animal.removeWhenFarAway,
// Chicken.removeWhenFarAway
func (m *Mob) removeWhenFarAway() bool {
	if m.Type == TypeWarden {
		return false
	}
	// vanilla: AbstractGolem.removeWhenFarAway
	if m.Type == TypeShulker {
		return false
	}
	if m.Type == TypeZombieVillager {
		return !m.Curing() && m.VillagerXp == 0
	}
	if m.IsFish() || m.Type == TypeAxolotl {
		// vanilla: AbstractFish.removeWhenFarAway, Axolotl.removeWhenFarAway
		return !m.FromBucket && !m.Named
	}
	return IsNautilus(m.Type) || m.kind().hostile || m.kind().water || m.kind().ambient || m.ChickenJockey || m.felineDespawns()
}

// despawnDistance is the MobCategory despawn distance: 64 for water_ambient
// fish, 128 otherwise.
// vanilla: MobCategory.WATER_AMBIENT
func (m *Mob) despawnDistance() float64 {
	if m.IsFish() {
		return fishDespawnDistance
	}
	return monsterDespawnDistance
}

// requiresCustomPersistence is Mob.requiresCustomPersistence: never
// despawned while riding, leashed, an enderman carrying a block, or a raider
// in a raid.
// vanilla: Mob.requiresCustomPersistence, EnderMan.requiresCustomPersistence,
// AbstractFish.requiresCustomPersistence, Raider.requiresCustomPersistence
func (m *Mob) requiresCustomPersistence() bool {
	return m.Type == TypeTadpole || IsNautilus(m.Type) && m.Tamable.Tame || m.RidesVehicle || m.LeashHolderID != 0 || m.CarriedBlock() != 0 || m.SulfurCubeFilled() || (m.IsFish() || m.Type == TypeSulfurCube || m.Type == TypeAxolotl) && m.FromBucket || m.InRaid
}

// CountsForSpawnCap reports whether the mob counts toward its category's
// cap; persistent mobs do not.
// vanilla: NaturalSpawner.createState
func (m *Mob) CountsForSpawnCap() bool {
	return !m.Persistence && !m.requiresCustomPersistence()
}

// despawnStep is Mob.checkDespawn: on peaceful a hostile mob that may not
// live there is discarded; otherwise it is measured against the nearest
// player in the dimension (getNearestPlayer, no distance limit): beyond 128 a
// mob that is not persistent is removed, beyond 32 it is removed on a 1-in-800 roll once idle 600 ticks, and within 32 the
// idleness resets. It reports whether the mob leaves (no corpse, sound or
// drops).
func (m *Mob) despawnStep(w World, players []Player) bool {
	// Only monsters go in peaceful (shouldDespawnInPeaceful); friendly water
	// creatures stay, their categories spawning there.
	if m.goneInPeaceful() && w.Difficulty() == rules.DifficultyPeaceful {
		return true
	}
	// vanilla: Endermite.aiStep
	if m.Type == TypeEndermite && !m.Persistence {
		m.Life++
		if m.Life >= endermiteMaxLife {
			return true
		}
	}
	if m.Type == TypeWither {
		// vanilla: WitherBoss.checkDespawn
		m.NoActionTicks = 0
		return false
	}
	var nearest *Player
	best := math.MaxFloat64
	for i := range players {
		p := &players[i]
		if p.Spectator {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z); d < best {
			best = d
			nearest = p
		}
	}
	if nearest == nil {
		// Nobody here to measure against; the mob stands.
		return false
	}
	// Persistent mobs and animals never leave (removeWhenFarAway), but a near
	// player still resets their idleness.
	leaves := !m.Persistence && !m.requiresCustomPersistence() && m.removeWhenFarAway()
	// vanilla: PatrollingMonster.removeWhenFarAway
	if isRaiderType(m.Type) && m.Patrolling && best <= 16384 {
		leaves = false
	}
	if d := m.despawnDistance(); leaves && best > d*d {
		return true
	}
	// Inside the no-despawn distance the idleness is forgiven.
	if best < monsterNoDespawnDistance*monsterNoDespawnDistance {
		m.NoActionTicks = 0
		return false
	}
	return leaves && m.NoActionTicks > despawnNoActionTicks && m.Rand.Intn(despawnDie) == 0
}

// idleStep winds the idleness counter: +1 a tick (Mob.serverAiStep), +2
// more in bright light (Monster.updateNoActionTime's magic value: raw light
// / 15 through f/(4−3f), lerped from the dimension's ambient light); a hurt
// resets it.
func (m *Mob) idleStep(w World) {
	// The base count is serverAiStep's, skipped by NoAI; the bright-light
	// bonus is Monster.updateNoActionTime's, which friendly kinds, cubes and
	// the phantom do not get.
	if !m.NoAI {
		m.NoActionTicks++
	}
	if !m.monsterClass() || m.isCube() {
		return
	}
	if IsIllagerType(m.Type) {
		m.NoActionTicks += 2 // vanilla: Raider.updateNoActionTime, any light
		return
	}
	g := lightMagicValue(w.MaxLocalRawLightAt(m.cell().X, int32(math.Floor(m.Y+m.eyeHeight())), m.cell().Z))
	if magic := g + w.AmbientLight()*(1-g); magic > 0.5 {
		m.NoActionTicks += 2
	}
}

// Despawn numbers: the MONSTER category's despawn and no-despawn
// distances, 600 idle ticks before the dice, and the 1-in-800 roll.
const (
	endermiteMaxLife         = 2400
	monsterDespawnDistance   = 128
	fishDespawnDistance      = 64
	monsterNoDespawnDistance = 32
	despawnNoActionTicks     = 600
	despawnDie               = 800
)
