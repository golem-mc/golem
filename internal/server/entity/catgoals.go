package entity

import (
	"math"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The tame cat's bed and seat goals.
// vanilla: Cat.registerGoals
const (
	catPriorityRelax    = 3
	catPriorityLieOnBed = 5
	catPrioritySitOn    = 7
	catPriorityLeap     = 8
	catPriorityAttack   = 9

	catBedPace     = 1.1 // CatLieOnBedGoal and CatRelaxOnOwnerGoal's walk
	catSeatPace    = 0.8 // CatSitOnBlockGoal
	catSeatRange   = 8   // search range of both block goals
	catBedRise     = 6   // CatLieOnBedGoal's verticalSearchRange
	catBedFall     = -2  // CatLieOnBedGoal.verticalSearchStart
	catBedDelay    = 40  // CatLieOnBedGoal.nextStartTick
	catRelaxNearSq = 2.5 // CatRelaxOnOwnerGoal.tick's lie-down distance
	catRelaxLieAt  = 8   // goal ticks beside the owner before the cat lies; vanilla: adjustedTickDelay(16)
	catRelaxSeeSq  = 100 // CatRelaxOnOwnerGoal.canUse's range
	// catLeapUp is LeapAtTargetGoal's yd in Cat and Ocelot.registerGoals.
	catLeapUp = 0.3
)

// catGiftSleep is the sleep counter a morning gift needs.
// vanilla: CatRelaxOnOwnerGoal.stop
const catGiftSleep = 100

var (
	catsCanLieOn = registry.BlockTagOf("minecraft:cats_can_lie_on")
	catBeds      = registry.BlockTagOf("minecraft:beds")
)

// catCanLieOn reports whether a cat lies on the state's block.
func catCanLieOn(state uint16) bool { return registry.BlockOf(state).InTag(catsCanLieOn) }

// catLieOnBedGoal walks a tame cat to a bed with air over it and lies there.
// vanilla: CatLieOnBedGoal
type catLieOnBedGoal struct{ *moveToBlock }

func newCatLieOnBedGoal(m *Mob) *catLieOnBedGoal {
	g := &catLieOnBedGoal{newMoveToBlock(m, catBedPace, catSeatRange, func(w World, x, y, z int32) bool {
		return w.IsAirAt(x, y+1, z) && catCanLieOn(w.BlockAt(x, y, z))
	})}
	g.vertStart, g.vertRange, g.startDelay = catBedFall, catBedRise, catBedDelay
	return g
}

func (g *catLieOnBedGoal) Priority() int { return catPriorityLieOnBed }

func (g *catLieOnBedGoal) CanStart(t *tickView) bool {
	m := g.m
	return m.Tamable.Tame && !m.Tamable.OrderedToSit && !m.Lying && g.canUse(t)
}

func (g *catLieOnBedGoal) Start(t *tickView) {
	g.start(t)
	g.m.SetMetadataTamableSittingPose(false)
}

func (g *catLieOnBedGoal) ShouldContinue(t *tickView) bool { return g.cont(t) }

func (g *catLieOnBedGoal) Tick(t *tickView) {
	g.tick(t)
	g.m.SetMetadataTamableSittingPose(false)
	g.m.SetMetadataLying(g.reached)
}

func (g *catLieOnBedGoal) Stop(*tickView) { g.m.SetMetadataLying(false) }

// catSitOnBlockGoal walks a tame cat to a free chest, a lit furnace or a
// bed's foot and sits on it.
// vanilla: CatSitOnBlockGoal
type catSitOnBlockGoal struct{ *moveToBlock }

func newCatSitOnBlockGoal(m *Mob) *catSitOnBlockGoal {
	g := &catSitOnBlockGoal{}
	g.moveToBlock = newMoveToBlock(m, catSeatPace, catSeatRange, g.seat)
	return g
}

// seat is CatSitOnBlockGoal.isValidTarget.
func (g *catSitOnBlockGoal) seat(w World, x, y, z int32) bool {
	if !w.IsAirAt(x, y+1, z) {
		return false
	}
	state := w.BlockAt(x, y, z)
	switch registry.BlockOf(state) {
	case registry.BlockChest:
		// vanilla: ChestBlockEntity.getOpenCount
		return g.m.ChestOpen == nil || !g.m.ChestOpen(x, y, z)
	case registry.BlockFurnace:
		return registry.StateBool(state, registry.PropLit)
	}
	return registry.BlockOf(state).InTag(catBeds) && registry.StateValue(state, registry.PropPart) != "head"
}

func (g *catSitOnBlockGoal) Priority() int { return catPrioritySitOn }

func (g *catSitOnBlockGoal) CanStart(t *tickView) bool {
	return g.m.Tamable.Tame && !g.m.Tamable.OrderedToSit && g.canUse(t)
}

func (g *catSitOnBlockGoal) Start(t *tickView) {
	g.start(t)
	g.m.SetMetadataTamableSittingPose(false)
}

func (g *catSitOnBlockGoal) ShouldContinue(t *tickView) bool {
	return g.cont(t)
}

func (g *catSitOnBlockGoal) Tick(t *tickView) {
	g.tick(t)
	g.m.SetMetadataTamableSittingPose(g.reached)
}

func (g *catSitOnBlockGoal) Stop(*tickView) { g.m.SetMetadataTamableSittingPose(false) }

// catRelaxOnOwnerGoal brings a tame cat to the foot of its sleeping owner's
// bed to settle and lie, and may leave a gift when the owner wakes.
// vanilla: Cat.CatRelaxOnOwnerGoal
type catRelaxOnOwnerGoal struct {
	m        *Mob
	owner    uuid.UUID
	goal     Waypoint
	hasGoal  bool
	onBedFor int
}

func (g *catRelaxOnOwnerGoal) Priority() int    { return catPriorityRelax }
func (g *catRelaxOnOwnerGoal) Flags() GoalFlags { return GoalMove | GoalJump }

// sleepingOwner is the owner while asleep, nil otherwise.
func (g *catRelaxOnOwnerGoal) sleepingOwner(t *tickView) *Player {
	if o := g.m.ownerIn(t); o != nil && o.Sleeping {
		return o
	}
	return nil
}

func (g *catRelaxOnOwnerGoal) CanStart(t *tickView) bool {
	m := g.m
	if !m.Tamable.Tame || m.Tamable.OrderedToSit {
		return false
	}
	o := g.sleepingOwner(t)
	if o == nil || Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) > catRelaxSeeSq {
		return false
	}
	at := Waypoint{int32(math.Floor(o.X)), int32(math.Floor(o.Y)), int32(math.Floor(o.Z))}
	state := t.w.BlockAt(at.X, at.Y, at.Z)
	if !catCanLieOn(state) {
		return false
	}
	g.owner, g.goal, g.hasGoal = o.UUID, at, true
	dx, dz := rules.HiveFacingStep(registry.StateValue(state, registry.PropFacing))
	g.goal.X, g.goal.Z = at.X-dx, at.Z-dz
	return !g.crowded(t)
}

// crowded reports whether another cat already lies or settles near the
// goal.
// vanilla: CatRelaxOnOwnerGoal.spaceIsOccupied
func (g *catRelaxOnOwnerGoal) crowded(t *tickView) bool {
	const inflate = 2
	for _, o := range t.others {
		if o == g.m || o.Type != TypeCat || !o.Lying && !o.RelaxStateOne {
			continue
		}
		hw := o.width() / 2
		if o.X+hw > float64(g.goal.X-inflate) && o.X-hw < float64(g.goal.X+1+inflate) &&
			o.Y+o.height() > float64(g.goal.Y-inflate) && o.Y < float64(g.goal.Y+1+inflate) &&
			o.Z+hw > float64(g.goal.Z-inflate) && o.Z-hw < float64(g.goal.Z+1+inflate) {
			return true
		}
	}
	return false
}

func (g *catRelaxOnOwnerGoal) ShouldContinue(t *tickView) bool {
	return g.m.Tamable.Tame && !g.m.Tamable.OrderedToSit && g.sleepingOwner(t) != nil && g.hasGoal && !g.crowded(t)
}

func (g *catRelaxOnOwnerGoal) walk(t *tickView) {
	g.m.seekTo(t.w, float64(g.goal.X)+0.5, float64(g.goal.Y), float64(g.goal.Z)+0.5, catBedPace, navReach)
}

func (g *catRelaxOnOwnerGoal) Start(t *tickView) {
	g.m.SetMetadataTamableSittingPose(false)
	g.walk(t)
}

func (g *catRelaxOnOwnerGoal) Tick(t *tickView) {
	o := g.sleepingOwner(t)
	if o == nil {
		return
	}
	m := g.m
	m.SetMetadataTamableSittingPose(false)
	if m.navDone() {
		g.walk(t)
	}
	if Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) >= catRelaxNearSq {
		m.SetMetadataLying(false)
		return
	}
	if g.onBedFor++; g.onBedFor > catRelaxLieAt {
		m.SetMetadataLying(true)
		m.SetMetadataRelaxStateOne(false)
		return
	}
	// vanilla: Mob.lookAt(owner, 45, 45)
	m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(o.X-m.X, o.Z-m.Z)), 45)
	m.SetMetadataRelaxStateOne(true)
}

func (g *catRelaxOnOwnerGoal) Stop(t *tickView) {
	m := g.m
	m.SetMetadataLying(false)
	// The owner's counter is 100 after a wake that finished the sleep.
	if o := m.ownerIn(t); o != nil && o.SleepTimer >= catGiftSleep && m.Rand.Float32() < t.w.CatGiftChance() {
		g.giveGift(t)
	}
	g.onBedFor = 0
	m.SetMetadataRelaxStateOne(false)
	m.stopSeeking()
}

// giveGift moves the cat within five blocks sideways and two vertically, and
// has it drop one item from the cat_morning_gift table behind it. A leashed
// cat does not start from its holder's position.
// vanilla: CatRelaxOnOwnerGoal.giveMorningGift
func (g *catRelaxOnOwnerGoal) giveGift(t *tickView) {
	m := g.m
	const reach = 5
	cell := m.cell()
	m.teleportTo(t.w, float64(cell.X+int32(m.Rand.Intn(2*reach+1)-reach)), float64(cell.Y+int32(m.Rand.Intn(5)-2)),
		float64(cell.Z+int32(m.Rand.Intn(2*reach+1)-reach)), registry.TagIDCatDoesNotTeleportTo, nil)
	cell = m.cell()
	yaw := float64(m.Yaw) * math.Pi / 180
	for _, stack := range rules.RollLoot("minecraft:gameplay/cat_morning_gift", m.giftContext(t.w), m.Rand) {
		vx, vy, vz := PopVelocity(m.Rand)
		t.events = append(t.events, MobEvent{
			Mob: m, Kind: MobGaveGift, Stack: stack,
			X: float64(cell.X) - math.Sin(yaw), Y: float64(cell.Y), Z: float64(cell.Z) + math.Cos(yaw),
			VX: vx, VY: vy, VZ: vz,
		})
	}
}

func (g *catRelaxOnOwnerGoal) RequiresEveryTick() bool { return false }

// felineAttackGoal walks a cat or ocelot at its target, crouching from far,
// sprinting from close, and bites within two body widths every second.
// vanilla: OcelotAttackGoal
type felineAttackGoal struct {
	m        *Mob
	priority int
	cooldown int
}

func (g *felineAttackGoal) Priority() int    { return g.priority }
func (g *felineAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *felineAttackGoal) CanStart(t *tickView) bool { return g.m.targetIn(t) != nil }

func (g *felineAttackGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil || Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) > felineChaseSq {
		return false
	}
	return !g.m.navDone() || g.CanStart(t)
}

func (g *felineAttackGoal) Start(*tickView) {}
func (g *felineAttackGoal) Stop(*tickView)  { g.m.stopSeeking() }

func (g *felineAttackGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(p.X-m.X, p.Z-m.Z)), 30)
	reach := m.width() * 2 * (m.width() * 2)
	d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	pace := felineWalkPace
	switch {
	case d > reach && d < felineRushSq:
		pace = felineSprintPace
	case d < felineChaseSq:
		pace = felineTemptPace
	}
	m.seekTo(t.w, p.X, p.Y, p.Z, pace, navReach)
	g.cooldown = max(g.cooldown-1, 0)
	if d > reach || g.cooldown > 0 {
		return
	}
	g.cooldown = attackInterval
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.attackDamage(), Player: *p})
}

func (g *felineAttackGoal) RequiresEveryTick() bool { return true }

// The attack goal's distances (squared) and the walk pace beyond them.
// vanilla: OcelotAttackGoal.tick
const (
	felineChaseSq = 225
	felineRushSq  = 16
)
