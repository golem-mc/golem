package entity

import (
	"math"
	"slices"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Tamable is the state a tameable mob keeps: whether it is tame, whose it
// is, and whether it was ordered to sit.
// vanilla: TamableAnimal
type Tamable struct {
	Tame  bool
	Owner uuid.UUID
	// OrderedToSit is the owner's order; SittingPose is the pose the sit goal
	// holds (by order or with the owner absent), which the client draws.
	OrderedToSit bool
	SittingPose  bool
}

// The tamed wolf's numbers.
const (
	// tamedWolfHealth is Wolf.applyTamingSideEffects' maximum and healed health.
	tamedWolfHealth = 40
	// tameOdds is the 1 in 3 of Wolf.tryToTame's nextInt(3) == 0.
	tameOdds = 3
	// teleportOwnerDistSq is TamableAnimal.TELEPORT_WHEN_DISTANCE_IS_SQ.
	teleportOwnerDistSq = 144
	// followStartDistance and followStopDistance are the 10 and 2 of
	// Wolf.registerGoals' FollowOwnerGoal.
	followStartDistance = 10.0
	followStopDistance  = 2.0
	followPace          = 1.0
	ownerFollowReplan   = 10
)

// The goal priorities of a wolf's tame side.
// vanilla: Wolf.registerGoals
const (
	wolfPrioritySit         = 2
	wolfPriorityFollowOwner = 6
	wolfPriorityOwnerHurtBy = 1
	wolfPriorityOwnerHurt   = 2
)

// TryToTame is the bone's 1-in-3 roll: the wolf becomes the player's, stops,
// drops its target and sits. It reports success.
// vanilla: Wolf.tryToTame
func (m *Mob) TryToTame(owner uuid.UUID) bool {
	if !m.RollTame() {
		return false
	}
	m.TameTo(owner)
	return true
}

// RollTame is the bone's 1-in-3 roll alone.
func (m *Mob) RollTame() bool { return m.Rand.Intn(tameOdds) == 0 }

// TameTo makes the mob the owner's, as a won roll does.
// vanilla: Wolf.tryToTame, TamableAnimal.tame
func (m *Mob) TameTo(owner uuid.UUID) {
	t := &m.Tamable
	m.SetMetadataTamableTame(true)
	m.SetMetadataTamableOwner(owner)
	if m.Type == TypeWolf {
		// vanilla: Wolf.applyTamingSideEffects
		m.MaxHealth = tamedWolfHealth
		m.SetMetadataHealth(tamedWolfHealth)
		m.syncMaxHealth()
	}
	m.stopSeeking()
	m.SetMetadataTargetID(0)
	t.OrderedToSit = true
}

// ToggleSit flips a tame wolf's sit order; it stops, forgets its jump and
// drops its target.
// vanilla: Wolf.mobInteract
func (m *Mob) ToggleSit() {
	m.Tamable.OrderedToSit = !m.Tamable.OrderedToSit
	m.jump = false
	m.stopSeeking()
	m.SetMetadataTargetID(0)
}

// IsOwnedBy reports whether the player is the owner of a tame mob.
func (m *Mob) IsOwnedBy(owner uuid.UUID) bool {
	return m.Tamable.Tame && m.Tamable.Owner != uuid.Nil && m.Tamable.Owner == owner
}

// CreditOwner is the player a tame wolf's blow credits with the hurt,
// uuid.Nil for any other striker.
// vanilla: LivingEntity.resolvePlayerResponsibleForDamage
func (m *Mob) CreditOwner() uuid.UUID {
	if m.Type != TypeWolf || !m.Tamable.Tame {
		return uuid.Nil
	}
	return m.Tamable.Owner
}

// ownerIn finds the owner among the world's players; nil when offline or in
// another dimension.
// vanilla: OwnableEntity.getOwner
func (m *Mob) ownerIn(t *tickView) *Player {
	if !m.Tamable.Tame || m.Tamable.Owner == uuid.Nil {
		return nil
	}
	for i := range t.players {
		if t.players[i].UUID == m.Tamable.Owner {
			return &t.players[i]
		}
	}
	return nil
}

// unableToMoveToOwner is true when ordered to sit, riding, leashed, or the
// owner is a spectator.
// vanilla: TamableAnimal.unableToMoveToOwner
func (m *Mob) unableToMoveToOwner(owner *Player) bool {
	return m.Tamable.OrderedToSit || m.RidesVehicle || m.LeashHolderID != 0 || owner.Spectator
}

// sitGoal keeps a tame wolf sitting while ordered to and while its owner is
// absent.
// vanilla: SitWhenOrderedToGoal
type sitGoal struct{ m *Mob }

func (g *sitGoal) Priority() int    { return wolfPrioritySit }
func (g *sitGoal) Flags() GoalFlags { return GoalMove | GoalJump }

func (g *sitGoal) CanStart(t *tickView) bool {
	m := g.m
	ordered := m.Tamable.OrderedToSit
	if !ordered && !m.Tamable.Tame {
		return false
	}
	if m.InWater() || !m.OnGround {
		return false
	}
	owner := m.ownerIn(t)
	if owner == nil {
		return true
	}
	if m.distanceToPlayer(owner) < followTeleportDist && owner.HurtBy != 0 {
		return false
	}
	return ordered
}

// followTeleportDist is the distance where the owner's trouble still reaches
// the wolf and following becomes a teleport: sqrt(teleportOwnerDistSq).
const followTeleportDist = 12.0

func (g *sitGoal) Start(*tickView) {
	g.m.stopSeeking()
	g.m.SetMetadataTamableSittingPose(true)
}

func (g *sitGoal) ShouldContinue(*tickView) bool { return g.m.Tamable.OrderedToSit }
func (g *sitGoal) Tick(*tickView)                {}
func (g *sitGoal) Stop(*tickView)                { g.m.SetMetadataTamableSittingPose(false) }
func (g *sitGoal) RequiresEveryTick() bool       { return false }

// followOwnerGoal walks a tame wolf to its owner once ten blocks away until
// within two, and teleports it from twelve.
// vanilla: FollowOwnerGoal
type followOwnerGoal struct {
	m      *Mob
	recalc int
	// stop replaces followStopDistance where nonzero: the cat's 5.
	stop float64
	// start replaces followStartDistance and priority wolfPriorityFollowOwner
	// where nonzero: the parrot's 5 and 2.
	start    float64
	priority int
}

func (g *followOwnerGoal) stopDistance() float64 {
	if g.stop != 0 {
		return g.stop
	}
	return followStopDistance
}

func (g *followOwnerGoal) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return wolfPriorityFollowOwner
}
func (g *followOwnerGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *followOwnerGoal) CanStart(t *tickView) bool {
	owner := g.m.ownerIn(t)
	if owner == nil || g.m.unableToMoveToOwner(owner) {
		return false
	}
	start := followStartDistance
	if g.start != 0 {
		start = g.start
	}
	return Dist2(g.m.X, g.m.Y, g.m.Z, owner.X, owner.Y, owner.Z) >= start*start
}

func (g *followOwnerGoal) Start(*tickView) { g.recalc, g.m.nav.waterFree = 0, true }

func (g *followOwnerGoal) ShouldContinue(t *tickView) bool {
	owner := g.m.ownerIn(t)
	if owner == nil || len(g.m.navOf().nav.path) == 0 || g.m.unableToMoveToOwner(owner) {
		return false
	}
	return Dist2(g.m.X, g.m.Y, g.m.Z, owner.X, owner.Y, owner.Z) > g.stopDistance()*g.stopDistance()
}

func (g *followOwnerGoal) Tick(t *tickView) {
	owner := g.m.ownerIn(t)
	if owner == nil {
		return
	}
	far := Dist2(g.m.X, g.m.Y, g.m.Z, owner.X, owner.Y, owner.Z) >= teleportOwnerDistSq
	if !far {
		dx, dz := owner.X-g.m.X, owner.Z-g.m.Z
		g.m.HeadYaw = float32(yawToward(dx, dz))
		g.m.Pitch = float32(pitchToward(dx, owner.Y+owner.eyeHeight()-(g.m.Y+g.m.height()*0.85), dz))
	}
	if g.recalc--; g.recalc > 0 {
		return
	}
	g.recalc = reducedTickDelay(ownerFollowReplan)
	if far {
		g.m.teleportAround(t.w, owner)
		return
	}
	g.m.seekTo(t.w, owner.X, owner.Y, owner.Z, followPace, navReach)
}

func (g *followOwnerGoal) Stop(*tickView) {
	g.m.nav.waterFree = false
	g.m.stopSeeking()
}
func (g *followOwnerGoal) RequiresEveryTick() bool { return false }

// teleportAround moves the mob to a free spot two to three blocks from the
// owner, in ten tries.
// vanilla: TamableAnimal.teleportToAroundBlockPos, maybeTeleportTo, canTeleportTo
func (m *Mob) teleportAround(w World, owner *Player) {
	ox, oy, oz := int32(math.Floor(owner.X)), int32(math.Floor(owner.Y)), int32(math.Floor(owner.Z))
	foot := footprintOf(m.width(), m.height())
	for range 10 {
		xd := m.Rand.Intn(7) - 3
		zd := m.Rand.Intn(7) - 3
		if xd > -2 && xd < 2 && zd > -2 && zd < 2 {
			continue
		}
		yd := m.Rand.Intn(3) - 1
		x, y, z := ox+int32(xd), oy+int32(yd), oz+int32(zd)
		if !walkableAt(w, foot, x, y, z) || !safeToTeleportTo(w, x, y, z) {
			continue
		}
		m.SetPos(float64(x)+0.5, float64(y), float64(z)+0.5)
		m.stopSeeking()
		return
	}
}

// ownerTargetGoal targets whoever struck the owner (byAttacker) or whoever
// the owner last struck, once per blow.
// vanilla: OwnerHurtByTargetGoal, OwnerHurtTargetGoal
type ownerTargetGoal struct {
	m          *Mob
	priority   int
	byAttacker bool
	// stamp is the owner's age at the blow last taken up.
	stamp  int
	target int32
}

func (g *ownerTargetGoal) Priority() int    { return g.priority }
func (g *ownerTargetGoal) Flags() GoalFlags { return GoalTarget }

// blow is the entity and the age of the owner's blow the goal answers.
func (g *ownerTargetGoal) blow(owner *Player) (int32, int) {
	if g.byAttacker {
		if owner.HurtNoWolfRetaliation {
			return 0, 0
		}
		return owner.HurtBy, owner.HurtByStamp
	}
	return owner.HurtMob, owner.HurtMobStamp
}

func (g *ownerTargetGoal) CanStart(t *tickView) bool {
	m := g.m
	if !m.Tamable.Tame || m.Tamable.OrderedToSit {
		return false
	}
	owner := m.ownerIn(t)
	if owner == nil {
		return false
	}
	id, stamp := g.blow(owner)
	if stamp == g.stamp || id == 0 || !g.canAttack(t, id, owner) {
		return false
	}
	g.target = id
	return true
}

// canAttack is TargetingConditions.DEFAULT (a visible living body the wolf
// may attack) with Wolf.wantsToAttack: a player only off peaceful with pvp
// on.
// vanilla: TargetingConditions.test, Mob.canAttack, Wolf.wantsToAttack
func (g *ownerTargetGoal) canAttack(t *tickView, id int32, owner *Player) bool {
	m := g.m
	target := m.targetByID(t, id)
	if id == owner.EntityID || target == nil || !target.Huntable || target.Spectator || m.alliedTo(target) {
		return false
	}
	if mob := t.mobByID(id); mob != nil {
		if !m.wantsToAttackMob(mob, owner) {
			return false
		}
	} else if t.w.Difficulty() == rules.DifficultyPeaceful || !t.w.BoolRule(world.RulePvp) {
		return false
	}
	return m.seesTarget(t.w, target)
}

// wantsToAttackMob is Wolf.wantsToAttack for a mob target.
// vanilla: Wolf.wantsToAttack
func (m *Mob) wantsToAttackMob(o *Mob, owner *Player) bool {
	switch o.Type {
	case TypeCreeper, TypeGhast:
		return false // vanilla also spares the armor stand, not simulated here
	case TypeWolf:
		return !o.Tamable.Tame || o.Tamable.Owner != owner.UUID
	case TypeCat, TypeParrot, TypeNautilus, TypeZombieNautilus:
		return !o.Tamable.Tame
	}
	if IsEquine(o.Type) {
		return !o.Horse.Tame
	}
	return true
}

func (g *ownerTargetGoal) Start(t *tickView) {
	why := TargetByOwnerAttack
	if g.byAttacker {
		why = TargetByOwnerHurt
	}
	g.m.setTargetBy(g.target, why)
	if owner := g.m.ownerIn(t); owner != nil {
		_, g.stamp = g.blow(owner)
	}
}

func (g *ownerTargetGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	return p != nil && g.m.inFollowRange(p) && !g.m.IsOwnedBy(p.UUID)
}

func (g *ownerTargetGoal) Tick(*tickView)          {}
func (g *ownerTargetGoal) Stop(*tickView)          { g.m.SetMetadataTargetID(0) }
func (g *ownerTargetGoal) RequiresEveryTick() bool { return false }

// safeToTeleportTo is canTeleportTo's PathType.WALKABLE test: no leaves,
// trapdoor-like, honey, powder-snow or damaging floor; nothing
// positionPathType classifies in the feet cell (fire, lava, rail, cocoa,
// powder snow, dripstone), no fluid or damaging block there; and no fire,
// lava, water or damaging block in the surrounding 3x3x3 outside its column.
// vanilla: TamableAnimal.canTeleportTo, WalkNodeEvaluator.getPathTypeStatic
func safeToTeleportTo(w World, x, y, z int32) bool {
	if leavesAt(w, x, y-1, z) || positionPathType(w, x, y, z) != pathClear {
		return false
	}
	if hazardousFloor(w.BlockAt(x, y-1, z)) || hazardInFeet(w.BlockAt(x, y, z)) {
		return false
	}
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				if hazardInFeet(w.BlockAt(x+dx, y+dy, z+dz)) {
					return false
				}
			}
		}
	}
	return true
}

// hazardInFeet reports whether a block is water, fire, lava or damaging to
// stand in or beside.
// vanilla: WalkNodeEvaluator.checkNeighbourBlocks
func hazardInFeet(state uint16) bool {
	return rules.IsWater(state) || rules.IsLava(state) || rules.IsFire(state) ||
		rules.IsCactus(state) || rules.IsSweetBerryBush(state) || rules.IsWitherRose(state)
}

// hazardousFloor reports floors a wolf is never teleported onto: trapdoor,
// lily pad, big dripleaf, powder snow, honey, cactus, berry bush, fluid.
// vanilla: WalkNodeEvaluator.getPathTypeStatic
func hazardousFloor(state uint16) bool {
	return isTrapdoorPathType(state) || rules.IsPowderSnow(state) ||
		rules.IsHoneyBlock(state) || hazardInFeet(state)
}

// leavesAt reports a leaf block, which a wolf is never teleported onto.
// vanilla: TamableAnimal.canTeleportTo
func leavesAt(w World, x, y, z int32) bool {
	return registry.BlockOf(w.BlockAt(x, y, z)).InTag(leavesBlocks)
}

// feedHeal applies TamableAnimal.feed's nutrition multiplier or fallback.
func (m *Mob) feedHeal(stack protocol.ItemStack, factor, fallback float64) {
	amount := fallback
	if food, ok := registry.StackFood(stack); ok {
		amount = factor * float64(food.Nutrition)
	}
	m.heal(amount)
}

// TeamWithOwner resolves a tame animal's team through its root owner when it
// has no scoreboard team of its own. lookup returns a mob and its own team,
// or nil and a player's team; an absent owner returns nil and an empty team.
// vanilla: TamableAnimal.getTeam, OwnableEntity.getRootOwner
func (m *Mob) TeamWithOwner(team string, lookup func(uuid.UUID) (*Mob, string)) string {
	if team != "" || !m.Tamable.Tame {
		return team
	}
	seen := []*Mob{m}
	owner := m.Tamable.Owner
	for owner != uuid.Nil {
		var next *Mob
		next, team = lookup(owner)
		if next == nil {
			return team
		}
		if slices.Contains(seen, next) {
			return ""
		}
		seen = append(seen, next)
		switch {
		case next.Type == TypeWolf || next.Type == TypeCat || next.Type == TypeParrot || IsNautilus(next.Type):
			owner = next.Tamable.Owner
		case IsEquine(next.Type):
			owner = next.Horse.Owner
		default:
			return team
		}
	}
	return ""
}
