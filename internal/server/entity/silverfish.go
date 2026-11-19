package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// silverfishKind is the silverfish: box 0.4 by 0.3, eyes 0.13, eight of
// health, 0.25 pace, attack damage 1.
// vanilla: EntityTypes.SILVERFISH's builder, Silverfish.createAttributes
var silverfishKind = &kind{
	name:         "Silverfish",
	width:        0.4,
	height:       0.3,
	eyeHeight:    0.13,
	maxHealth:    8,
	speed:        0.25,
	hostile:      true,
	followRange:  16,
	attackDamage: 1,
	sounds: MobSounds{
		Ambient: registry.SoundEntitySilverfishAmbient,
		Hurt:    registry.SoundEntitySilverfishHurt,
		Death:   registry.SoundEntitySilverfishDeath,
		Step:    registry.SoundEntitySilverfishStep,
	},
}

// Silverfish.registerGoals' priorities for the goals of its own.
const (
	silverfishPowderPriority  = 1
	silverfishFriendsPriority = 3
	silverfishMeleePriority   = 4
	silverfishMergePriority   = 5
)

// silverfishGoals is Silverfish.registerGoals: FloatGoal 1,
// ClimbOnTopOfPowderSnowGoal 1, the wake-up call 3, MeleeAttackGoal 4, the
// merge with stone 5, HurtByTargetGoal (setAlertOthers) 1 and
// NearestAttackableTargetGoal(Player) 2.
func silverfishGoals(m *Mob) []Goal {
	m.friends = &wakeFriendsGoal{m: m}
	return []Goal{
		newFloatGoal(m),
		&climbOnPowderSnowGoal{m: m, priority: silverfishPowderPriority},
		&hurtByGoal{m: m, priority: 1, alertOthers: true},
		&nearestTargetGoal{m: m, priority: 2},
		m.friends,
		&meleeAttackGoal{m: m, priority: silverfishMeleePriority},
		&mergeWithStoneGoal{m: m, priority: silverfishMergePriority, interval: 10},
	}
}

// noteSilverfishHurt arms the wake-up call of a hurt with a body behind it
// or of a type on always_triggers_silverfish.
// vanilla: Silverfish.hurtServer
func (m *Mob) noteSilverfishHurt(entityBehind bool, damageType registry.DamageType) {
	if m.friends == nil {
		return
	}
	if entityBehind || damageType.InTag(registry.DamageTypeTagIDAlwaysTriggersSilverfish) {
		m.friends.notifyHurt()
	}
}

// Reach of the wake-up call around the silverfish's block.
const (
	wakeReachXZ = 10
	wakeReachY  = 5
)

// wakeFriendsGoal is the call a hurt silverfish makes twenty ticks after the
// blow.
// vanilla: Silverfish.SilverfishWakeUpFriendsGoal
type wakeFriendsGoal struct {
	m              *Mob
	lookForFriends int
}

func (g *wakeFriendsGoal) Priority() int    { return silverfishFriendsPriority }
func (g *wakeFriendsGoal) Flags() GoalFlags { return 0 }

// notifyHurt starts the countdown unless one runs.
func (g *wakeFriendsGoal) notifyHurt() {
	if g.lookForFriends == 0 {
		g.lookForFriends = reducedTickDelay(20)
	}
}

func (g *wakeFriendsGoal) CanStart(*tickView) bool       { return g.lookForFriends > 0 }
func (g *wakeFriendsGoal) Start(*tickView)               {}
func (g *wakeFriendsGoal) ShouldContinue(*tickView) bool { return g.lookForFriends > 0 }
func (g *wakeFriendsGoal) Stop(*tickView)                {}
func (g *wakeFriendsGoal) RequiresEveryTick() bool       { return false }

// Tick counts down, then walks the surrounding cells in vanilla's offset
// order 0, 1, -1, 2, -2, ..., waking infested blocks until a coin flip
// stops it.
// vanilla: SilverfishWakeUpFriendsGoal.tick
func (g *wakeFriendsGoal) Tick(t *tickView) {
	g.lookForFriends--
	if g.lookForFriends > 0 {
		return
	}
	base := g.m.cell()
	for y := int32(0); y <= wakeReachY && y >= -wakeReachY; y = alternate(y) {
		for x := int32(0); x <= wakeReachXZ && x >= -wakeReachXZ; x = alternate(x) {
			for z := int32(0); z <= wakeReachXZ && z >= -wakeReachXZ; z = alternate(z) {
				at := Waypoint{base.X + x, base.Y + y, base.Z + z}
				if !infestedBlocks[registry.BlockOf(t.w.BlockAt(at.X, at.Y, at.Z))] {
					continue
				}
				t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobWokeInfested,
					X: float64(at.X), Y: float64(at.Y), Z: float64(at.Z)})
				if g.m.Rand.Intn(2) == 0 {
					return
				}
			}
		}
	}
}

// alternate steps an offset through 0, 1, -1, 2, -2, ...
func alternate(v int32) int32 {
	if v <= 0 {
		return 1 - v
	}
	return -v
}

// mergeDirections are Direction.VALUES in vanilla's order, which
// Direction.getRandom indexes.
var mergeDirections = [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// mergeWithStoneGoal is the stroll that, one try in ten, enters an adjacent
// host block instead of walking.
// vanilla: Silverfish.SilverfishMergeWithStoneGoal
type mergeWithStoneGoal struct {
	randomStroll
	cell    Waypoint
	doMerge bool
}

func (g *mergeWithStoneGoal) CanStart(t *tickView) bool {
	if g.m.TargetID != 0 || len(g.m.navOf().nav.path) != 0 {
		return false
	}
	if t.w.MobGriefing() && g.m.Rand.Intn(reducedTickDelay(10)) == 0 {
		d := mergeDirections[g.m.Rand.Intn(len(mergeDirections))]
		g.cell = Waypoint{
			int32(math.Floor(g.m.X)) + d[0],
			int32(math.Floor(g.m.Y+0.5)) + d[1],
			int32(math.Floor(g.m.Z)) + d[2],
		}
		if rules.IsInfestableHost(t.w.BlockAt(g.cell.X, g.cell.Y, g.cell.Z)) {
			g.doMerge = true
			return true
		}
	}
	g.doMerge = false
	return g.randomStroll.CanStart(t)
}

func (g *mergeWithStoneGoal) ShouldContinue(t *tickView) bool {
	return !g.doMerge && g.randomStroll.ShouldContinue(t)
}

// Start infests the block and takes the silverfish out of the world, the
// server telling the poof.
func (g *mergeWithStoneGoal) Start(t *tickView) {
	if !g.doMerge {
		g.randomStroll.Start(t)
		return
	}
	if !rules.IsInfestableHost(t.w.BlockAt(g.cell.X, g.cell.Y, g.cell.Z)) {
		return
	}
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobMergedWithStone,
		X: float64(g.cell.X), Y: float64(g.cell.Y), Z: float64(g.cell.Z)})
	g.m.Discard()
}

// climbOnPowderSnowGoal keeps a walker of the powder snow jumping while it
// sinks into the snow, so it surfaces.
// vanilla: ClimbOnTopOfPowderSnowGoal
type climbOnPowderSnowGoal struct {
	m        *Mob
	priority int
}

func (g *climbOnPowderSnowGoal) Priority() int    { return g.priority }
func (g *climbOnPowderSnowGoal) Flags() GoalFlags { return GoalJump }

// CanStart needs the body in powder snow (lastInPowderSnow, vanilla's
// flag from the last move) and the cell above open or powder snow.
func (g *climbOnPowderSnowGoal) CanStart(t *tickView) bool {
	if !g.m.lastInPowderSnow {
		return false
	}
	if !g.m.Type.InTag(registry.EntityTypeTagIDPowderSnowWalkableMobs) {
		return false
	}
	c := g.m.cell()
	above := t.w.BlockAt(c.X, c.Y+1, c.Z)
	return rules.IsPowderSnow(above) || len(world.CollisionBoxes(above)) == 0
}

func (g *climbOnPowderSnowGoal) Start(*tickView)                 {}
func (g *climbOnPowderSnowGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *climbOnPowderSnowGoal) Stop(*tickView)                  {}
func (g *climbOnPowderSnowGoal) RequiresEveryTick() bool         { return true }

func (g *climbOnPowderSnowGoal) Tick(*tickView) { g.m.jump = true }
