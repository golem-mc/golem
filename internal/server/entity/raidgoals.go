package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The goals Raider adds to every raider: walking to the raid, wandering the
// village's beds and celebrating a lost raid.
// vanilla: Raider.registerGoals, PathfindToRaidGoal, Raider.RaiderMoveThroughVillageGoal, Raider.RaiderCelebration

// RaidPos is a block position in a raider's view of its raid.
type RaidPos struct{ X, Y, Z int }

// RaidView is what a raider's goals read of its raid, set once a tick.
type RaidView struct {
	// Has is a raid at all (Raider.getCurrentRaid != null); Active is
	// Raid.isActive, Over isOver and Loss isLoss.
	Has, Active, Over, Loss bool
	// Preparing is before the first wave or during the countdown between waves.
	Preparing bool
	// Center is the raid's centre block.
	Center RaidPos
	// InVillage is ServerLevel.isVillage at the raider's block.
	InVillage bool
	// Homes are the beds near the raider; nil keeps the last list.
	Homes []RaidPos
}

// raidWalk is a raider's raid view with the goals' own memory.
type raidWalk struct {
	view RaidView
	// recruit asks the server to enrol the raiders around the mob.
	recruit bool
}

// SetRaidView tells a raider about its raid, once a tick.
func (m *Mob) SetRaidView(v RaidView) {
	if v.Homes == nil {
		v.Homes = m.raidWalk.view.Homes
	}
	if !v.Active {
		v.Homes = nil
	}
	m.raidWalk.view = v
}

// TakeRaidRecruit reports, once, that the raider's walk to the raid is due to
// recruit raiders within 16 blocks.
// vanilla: PathfindToRaidGoal.recruitNearby
func (m *Mob) TakeRaidRecruit() bool {
	r := m.raidWalk.recruit
	m.raidWalk.recruit = false
	return r
}

// withRaidGoals adds the patrol and Raider's three goals to a raider's goals,
// kept in priority order.
func withRaidGoals(m *Mob, goals []Goal) []Goal {
	goals = append(goals, &patrolGoal{m: m}, &pathfindToRaidGoal{m: m}, &raidVillageGoal{m: m}, &raidCelebrationGoal{m: m})
	slices.SortStableFunc(goals, func(a, b Goal) int { return a.Priority() - b.Priority() })
	return goals
}

// pathfindToRaidGoal walks a raider that stands outside the village toward the
// raid's centre, recruiting the raiders it passes.
type pathfindToRaidGoal struct {
	m           *Mob
	recruitTick int
}

func (g *pathfindToRaidGoal) Priority() int           { return 3 }
func (g *pathfindToRaidGoal) Flags() GoalFlags        { return GoalMove }
func (g *pathfindToRaidGoal) RequiresEveryTick() bool { return false }

func (g *pathfindToRaidGoal) CanStart(*tickView) bool {
	v := &g.m.raidWalk.view
	return g.m.TargetID == 0 && len(g.m.Passengers) == 0 && v.Active && !v.Over && !v.InVillage
}

func (g *pathfindToRaidGoal) ShouldContinue(*tickView) bool {
	v := &g.m.raidWalk.view
	return v.Active && !v.Over && !v.InVillage
}

func (g *pathfindToRaidGoal) Start(*tickView) {}
func (g *pathfindToRaidGoal) Stop(*tickView)  {}

func (g *pathfindToRaidGoal) Tick(t *tickView) {
	m := g.m
	v := &m.raidWalk.view
	if !v.Active {
		return
	}
	if m.Age > g.recruitTick {
		g.recruitTick = m.Age + raidRecruitInterval
		m.raidWalk.recruit = true
	}
	if !m.navDone() {
		return
	}
	center := [2]float64{float64(v.Center.X) + 0.5, float64(v.Center.Z) + 0.5}
	if cell, ok := posTowardsWalkable(t.w, m, 15, 4, center, math.Pi/2); ok {
		m.seekTo(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, 1, navReach)
	}
}

// RaidHomeRadius is how far a raider looks for a bed, in blocks.
const RaidHomeRadius = 48

const (
	// raidRecruitInterval is the ticks between PathfindToRaidGoal's recruiting.
	raidRecruitInterval = 20
	// raidVisitedKept is how many visited beds a raider remembers.
	raidVisitedKept = 3
	raidVillagePace = 1.05
)

// raidVillageGoal walks a raider in a raid from bed to bed, skipping the last
// few it reached.
type raidVillageGoal struct {
	m       *Mob
	poi     RaidPos
	visited []RaidPos
	stuck   bool
}

func (g *raidVillageGoal) Priority() int           { return 4 }
func (g *raidVillageGoal) Flags() GoalFlags        { return GoalMove }
func (g *raidVillageGoal) RequiresEveryTick() bool { return false }

// reached is BlockPos.closerToCenterThan the raider.
func (g *raidVillageGoal) reached(dist float64) bool {
	m := g.m
	dx, dy, dz := float64(g.poi.X)+0.5-m.X, float64(g.poi.Y)+0.5-m.Y, float64(g.poi.Z)+0.5-m.Z
	return dx*dx+dy*dy+dz*dz < dist*dist
}

func (g *raidVillageGoal) CanStart(*tickView) bool {
	if len(g.visited) > raidVisitedKept-1 {
		g.visited = g.visited[1:]
	}
	m := g.m
	v := &m.raidWalk.view
	if !v.Active || v.Over || m.TargetID != 0 {
		return false
	}
	var fresh []RaidPos
	for _, h := range v.Homes {
		dx, dy, dz := float64(h.X-int(rules.BlockOf(m.X))), float64(h.Y-int(rules.BlockOf(m.Y))), float64(h.Z-int(rules.BlockOf(m.Z)))
		if dx*dx+dy*dy+dz*dz <= RaidHomeRadius*RaidHomeRadius && !slices.Contains(g.visited, h) {
			fresh = append(fresh, h)
		}
	}
	if len(fresh) == 0 {
		return false
	}
	g.poi = fresh[m.Rand.Intn(len(fresh))]
	return true
}

func (g *raidVillageGoal) Start(t *tickView) {
	g.m.NoActionTicks = 0
	g.m.seekTo(t.w, float64(g.poi.X), float64(g.poi.Y), float64(g.poi.Z), raidVillagePace, navReach)
	g.stuck = false
}

func (g *raidVillageGoal) ShouldContinue(*tickView) bool {
	m := g.m
	if m.navDone() {
		return false
	}
	return m.TargetID == 0 && !g.reached(m.width()+1) && !g.stuck
}

func (g *raidVillageGoal) Stop(*tickView) {
	if g.reached(1) {
		g.visited = append(g.visited, g.poi)
	}
}

func (g *raidVillageGoal) Tick(t *tickView) {
	m := g.m
	if !m.navDone() {
		return
	}
	poi := [2]float64{float64(g.poi.X) + 0.5, float64(g.poi.Z) + 0.5}
	cell, ok := posTowardsWalkable(t.w, m, 16, 7, poi, 0.3141592741012573)
	if !ok {
		cell, ok = posTowardsWalkable(t.w, m, 8, 7, poi, math.Pi/2)
	}
	if !ok {
		g.stuck = true
		return
	}
	m.seekTo(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, raidVillagePace, navReach)
}

// raidCelebrationGoal makes a raider with no target jump and cheer once its
// raid is lost.
type raidCelebrationGoal struct{ m *Mob }

func (g *raidCelebrationGoal) Priority() int           { return 5 }
func (g *raidCelebrationGoal) Flags() GoalFlags        { return GoalMove }
func (g *raidCelebrationGoal) RequiresEveryTick() bool { return false }

func (g *raidCelebrationGoal) CanStart(*tickView) bool {
	return g.m.Alive() && g.m.TargetID == 0 && g.m.raidWalk.view.Has && g.m.raidWalk.view.Loss
}

func (g *raidCelebrationGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *raidCelebrationGoal) Start(*tickView)                 { g.m.SetMetadataCelebrating(true) }
func (g *raidCelebrationGoal) Stop(*tickView)                  { g.m.SetMetadataCelebrating(false) }

func (g *raidCelebrationGoal) Tick(t *tickView) {
	m := g.m
	if !m.Silent && m.Rand.Intn(reducedTickDelay(100)) == 0 {
		ev := MobEvent{Mob: m, Kind: MobSounded, Volume: 1, Pitch: m.VoicePitch(), SoundID: raidCelebrationSounds[m.Type]}
		t.events = append(t.events, ev)
	}
	if !m.RidesVehicle && m.Rand.Intn(reducedTickDelay(50)) == 0 {
		m.jump = true
	}
}

// vanilla: Raider.getCelebrateSound, Illusioner.getCelebrateSound
var raidCelebrationSounds = [registry.EntityTypeCount]registry.Sound{
	TypeEvoker:     registry.SoundEntityEvokerCelebrate,
	TypeIllusioner: registry.SoundEntityIllusionerAmbient,
	TypePillager:   registry.SoundEntityPillagerCelebrate,
	TypeRavager:    registry.SoundEntityRavagerCelebrate,
	TypeVindicator: registry.SoundEntityVindicatorCelebrate,
	TypeWitch:      registry.SoundEntityWitchCelebrate,
}
