package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The illagers: AbstractIllager, the base of the vindicator, pillager,
// evoker and illusioner. They hunt players, villagers and iron golems,
// spare each other and baby villagers.
// vanilla: AbstractIllager, Raider, Vindicator

const TypeVindicator = registry.EntityTypeVindicator

func init() {
	// vanilla: EntityTypes.VINDICATOR and Vindicator.createAttributes
	kinds[TypeVindicator] = &kind{
		name: "Vindicator", width: 0.6, height: 1.95, eyeHeight: 1.6575,
		maxHealth: 24, speed: float64(float32(0.35)), hostile: true, followRange: 12, attackDamage: 5,
		sounds: MobSounds{
			Ambient: registry.SoundEntityVindicatorAmbient,
			Hurt:    registry.SoundEntityVindicatorHurt,
			Death:   registry.SoundEntityVindicatorDeath,
		},
	}
	mobTracking[TypeVindicator] = tracking{rangeChunks: 8, updateInterval: 3}
}

var (
	illagerKinds = mobKindsMatching(IsIllagerType)
	raiderKinds  = mobKindsMatching(isRaiderType)
)

// IsIllagerType reports whether the type is in the minecraft:illager entity
// type tag, which minecraft:illager_friends lists.
func IsIllagerType(t MobType) bool {
	return t == TypeVindicator || t == TypePillager || t == TypeEvoker || t == TypeIllusioner
}

// isRaiderType is the Raider subclasses present here: the illagers, the
// witch and the ravager.
func isRaiderType(t MobType) bool { return IsIllagerType(t) || t == TypeWitch || t == TypeRavager }

// IsRaiderType reports whether the type is a Raider that can join a raid.
func IsRaiderType(t MobType) bool { return isRaiderType(t) }

// WearOminousBanner puts the raid captain's banner on the head, a drop
// guaranteed whole.
// vanilla: Raid.setLeader
func (m *Mob) WearOminousBanner(banner protocol.ItemStack) {
	m.LeadWithBanner(banner)
	m.Patrolling = true
}

// patrolLeaderOdds is the chance a freshly finalized raider is a patrol leader.
// vanilla: PatrollingMonster.finalizeSpawn
const patrolLeaderOdds = 0.06

// LeadWithBanner makes the mob a patrol leader wearing the banner, a drop
// guaranteed whole.
// vanilla: PatrollingMonster.finalizeSpawn
func (m *Mob) LeadWithBanner(banner protocol.ItemStack) {
	m.SetEquipment(protocol.EquipFeet+wornHead, banner)
	m.PatrolLeader = true
	m.DropChances[2+wornHead] = guaranteedDropChance
}

// DropSpawnLead clears the leader roll: a raid or patrol spawn takes none.
// vanilla: PatrollingMonster.finalizeSpawn
func (m *Mob) DropSpawnLead() { m.PatrolLeader = false }

// DressSpawnLeader gives a rolled leader without a helmet the banner.
// vanilla: PatrollingMonster.finalizeSpawn
func (m *Mob) DressSpawnLeader(banner protocol.ItemStack) {
	if m.PatrolLeader && m.Worn[wornHead].Empty() {
		m.LeadWithBanner(banner)
	}
}

// Captain is Raider.isCaptain: a patrol leader with a banner on its head. The
// banner is judged by its item, not its patterns.
func (m *Mob) Captain() bool {
	return m.PatrolLeader && m.Worn[wornHead].ItemID == int32(registry.ItemWhiteBanner)
}

// MarkJohnny turns the vindicator against every living body, for good.
// vanilla: Vindicator.setCustomName
func (m *Mob) MarkJohnny() { m.Johnny = true }

// finalizeVindicator is Vindicator.finalizeSpawn without a raid, after the
// Mob step: an iron axe in the main hand and the weapon enchantment roll on
// dice, no armour.
// vanilla: Vindicator.finalizeSpawn
func (m *Mob) finalizeVindicator(dice Dice, local rules.LocalDifficulty) {
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemIronAxe), Count: 1})
	m.rollSpawnEnchantments(dice, local)
}

var (
	abstractVillagerKinds = mobKindsMatching(isAbstractVillager)
	ironGolemKinds        = []MobType{TypeIronGolem}
)

// isAbstractVillager reports whether t is an AbstractVillager: a villager or
// a wandering trader.
func isAbstractVillager(t MobType) bool { return t == TypeVillager || t == TypeWanderingTrader }

// illagerTargetable is AbstractIllager.canAttack: a baby villager or trader is
// spared.
func illagerTargetable(o *Mob) bool { return !isAbstractVillager(o.Type) || !o.IsBaby() }

// illagerVillagerTarget is the NearestAttackableTargetGoal<AbstractVillager>
// filter the illagers and the ravager share.
func illagerVillagerTarget(o *Mob) bool { return isAbstractVillager(o.Type) && illagerTargetable(o) }

// illagerTargets is the target selection the vindicator, the pillager and the
// ravager share: HurtByTargetGoal(Raider) 1, the player 2, villagers, wandering
// traders and iron golems 3. villagerUnsighted keeps a villager already
// targeted without sight.
func illagerTargets(m *Mob, villagerUnsighted bool) []Goal {
	return []Goal{
		&hurtByGoal{m: m, priority: 1, alertOthers: true, ignore: isRaiderType},
		&nearestTargetGoal{m: m, priority: 2},
		&mobTargetGoal{m: m, priority: 3, unsighted: villagerUnsighted, kinds: abstractVillagerKinds, matches: illagerVillagerTarget},
		&mobTargetGoal{m: m, priority: 3, kinds: ironGolemKinds},
	}
}

// The pace the pillager and the vindicator flee a creaking at.
// vanilla: Pillager.registerGoals, Vindicator.registerGoals
const (
	illagerCreakingWalk   = 1.0
	illagerCreakingSprint = 1.2
)

// vindicatorGoals wires Vindicator.registerGoals: FloatGoal 0, creaking
// avoidance 1, hold ground 4, MeleeAttackGoal 5, the illager targets, Johnny 4, stroll 8 at
// 0.6, looks 9 and 10.
func vindicatorGoals(m *Mob) []Goal {
	goals := append([]Goal{newFloatGoal(m), newCreakingAvoidGoal(m, 1, illagerCreakingWalk, illagerCreakingSprint),
		newObtainBannerGoal(m)}, illagerTargets(m, false)...)
	goals = append(goals,
		newVindicatorBreakDoorGoal(m),
		newOpenDoorGoal(m, 3, false, func() bool { return m.InRaid }),
		&mobTargetGoal{m: m, priority: 4, gate: func() bool { return m.Johnny }, eager: true, matches: func(o *Mob) bool {
			return !IsIllagerType(o.Type) && illagerTargetable(o)
		}},
		&holdGroundGoal{m: m, priority: 4},
		&meleeAttackGoal{m: m, priority: 5},
		&randomStroll{m: m, priority: 8, pace: 0.6},
		&lookAtPlayer{m: m, priority: 9, reach: 3},
		&lookAtMob{m: m, priority: 10},
	)
	return withRaidGoals(m, goals)
}

// lookAtMob is LookAtPlayerGoal(this, Mob.class, 8.0f): a glance at the
// nearest other mob within eight blocks, three vertically.
type lookAtMob struct {
	m        *Mob
	priority int
	// reach replaces lookAtMobRange where non-zero.
	reach   float64
	remains int
	target  int32
}

const (
	lookAtMobRange  = 8.0
	lookAtMobRangeY = 3.0 // the goal's bounding box inflate in y
)

func (g *lookAtMob) rangeSq() float64 {
	if g.reach != 0 {
		return g.reach * g.reach
	}
	return lookAtMobRange * lookAtMobRange
}

func (g *lookAtMob) Priority() int    { return g.priority }
func (g *lookAtMob) Flags() GoalFlags { return GoalLook }

func (g *lookAtMob) CanStart(t *tickView) bool {
	m := g.m
	if m.Rand.Float64() >= lookChance {
		return false
	}
	var nearest *Mob
	best := g.rangeSq()
	for _, o := range t.others {
		if o == m || !o.Alive() || math.Abs(o.Y-m.Y) > m.height()+lookAtMobRangeY {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); d <= best {
			best, nearest = d, o
		}
	}
	if nearest == nil {
		return false
	}
	g.target = nearest.ID
	return true
}

func (g *lookAtMob) Start(*tickView) {
	g.remains = reducedTickDelay(lookForTicks + g.m.Rand.Intn(lookForVaries))
}

func (g *lookAtMob) other(t *tickView) *Mob {
	o := t.mobByID(g.target)
	if o == nil || !o.Alive() || Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z) > g.rangeSq() {
		return nil
	}
	return o
}

func (g *lookAtMob) ShouldContinue(t *tickView) bool {
	return g.remains > 0 && g.other(t) != nil
}

func (g *lookAtMob) Tick(t *tickView) {
	g.remains--
	if o := g.other(t); o != nil {
		m := g.m
		dx, dz := o.X-m.X, o.Z-m.Z
		m.HeadYaw = float32(yawToward(dx, dz))
		m.Pitch = float32(pitchToward(dx, o.Y+o.eyeHeight()-(m.Y+m.eyeHeight()), dz))
	}
}

func (g *lookAtMob) Stop(*tickView)          {}
func (g *lookAtMob) RequiresEveryTick() bool { return false }
