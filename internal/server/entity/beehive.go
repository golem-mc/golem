package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The bee's hive and flower numbers.
// vanilla: Bee, Bee.BeePollinateGoal
const (
	// beeFarAway is the distance in blocks past which a hive is forgotten.
	beeFarAway = 48
	// beeEnterRange is how close to the hive's centre a bee goes in.
	beeEnterRange = 2.0
	// beeTiredAfter is the ticks without nectar after which a bee goes home.
	beeTiredAfter = 3600
	// beeLocateRange is the radius of the hive search.
	beeLocateRange = 20
	// beeLocateCooldown is the pause after a hive search or a dropped hive.
	beeLocateCooldown = 200
	// beeMaxTravel is the ticks a bee flies to its hive before dropping it.
	beeMaxTravel = 2400
	// beeMaxCrops is the crops one pollination grows.
	beeMaxCrops = 10
	// beePollinateTicks is the time over a flower that earns nectar.
	beePollinateTicks = 400
	// beePollinateGiveUp is the ticks after which a flower is dropped.
	beePollinateGiveUp = 600
	// beeFlowerRadius is the Manhattan radius of the flower search.
	beeFlowerRadius = 5
	// beeKnownFlowerAfter is the ticks without nectar before a bee goes back
	// to the flower it remembers.
	beeKnownFlowerAfter = 600
	// beePollinateSoundGap is the ticks of pollination between two buzzes.
	beePollinateSoundGap = 60
	// beeHoverPace is the speed factor while hovering over a flower.
	beeHoverPace = 0.35
	// beeHoverHeight lifts the hover point above the flower's cell floor.
	beeHoverHeight = float64(float32(0.6))
	// beeHoverJitter is the width of the hover point's shifts.
	beeHoverJitter = float64(float32(0.33333334))
)

// beeMemory is the hive and flower a bee remembers and its countdowns.
type beeMemory struct {
	hive, flower       [3]int32
	hasHive, hasFlower bool
	blacklist          [][3]int32
	// stayOut keeps the bee out of any hive; hiveCooldown and flowerCooldown
	// pace the searches.
	stayOut, hiveCooldown, flowerCooldown int
	// noNectar counts the ticks since the bee last had nectar or left a hive.
	noNectar int
	crops    int
	// pollinating is set while the pollinate goal runs.
	pollinating bool
}

// BeeMemory is what a bee keeps of its hive, its flower and its errands
// between saves. Hive and Flower are nil when it knows none.
type BeeMemory struct {
	Hive, Flower                  *[3]int32
	StayOut, NoNectar, CropsGrown int
}

// BeeMemory reads the saved form of the bee's memory.
func (m *Mob) BeeMemory() BeeMemory {
	b := &m.bee.mem
	out := BeeMemory{StayOut: b.stayOut, NoNectar: b.noNectar, CropsGrown: b.crops}
	if b.hasHive {
		h := b.hive
		out.Hive = &h
	}
	if b.hasFlower {
		f := b.flower
		out.Flower = &f
	}
	return out
}

// SetBeeMemory restores the saved memory.
func (m *Mob) SetBeeMemory(in BeeMemory) {
	b := &m.bee.mem
	b.stayOut, b.noNectar, b.crops = in.StayOut, in.NoNectar, in.CropsGrown
	b.hasHive, b.hasFlower = in.Hive != nil, in.Flower != nil
	if in.Hive != nil {
		b.hive = *in.Hive
	}
	if in.Flower != nil {
		b.flower = *in.Flower
	}
}

// SetBeeHive points the bee at a hive, the one a hive released it from.
// vanilla: Bee.setHivePos
func (m *Mob) SetBeeHive(x, y, z int32) {
	m.bee.mem.hive, m.bee.mem.hasHive = [3]int32{x, y, z}, true
}

// SetBeeStayOut keeps the bee out of hives for the ticks given.
// vanilla: Bee.setStayOutOfHiveCountdown
func (m *Mob) SetBeeStayOut(ticks int) { m.bee.mem.stayOut = ticks }

// beeCloserThan is Vec3i.closerThan against the bee's cell: strictly inside d.
// vanilla: Vec3i.closerThan
func (m *Mob) beeCloserThan(p [3]int32, d float64) bool {
	c := cellAt(m.X, m.Y, m.Z)
	dx, dy, dz := float64(c.X-p[0]), float64(c.Y-p[1]), float64(c.Z-p[2])
	return dx*dx+dy*dy+dz*dz < d*d
}

// beeHiveValid is Bee.getBeehiveBlockEntity != null.
func (m *Mob) beeHiveValid(w World) bool {
	b := &m.bee.mem
	if !b.hasHive || !m.beeCloserThan(b.hive, beeFarAway) {
		return false
	}
	_, ok := w.BeehiveOccupants(b.hive[0], b.hive[1], b.hive[2])
	return ok
}

// beeHiveNearFire is BeehiveBlockEntity.isFireNearby for the bee's hive.
func (m *Mob) beeHiveNearFire(w World) bool {
	if !m.beeHiveValid(w) {
		return false
	}
	h := m.bee.mem.hive
	return rules.HiveFireNearby(w, h[0], h[1], h[2])
}

// beeWantsHive is Bee.wantsToEnterHive.
func (m *Mob) beeWantsHive(w World) bool {
	b := &m.bee
	if b.mem.stayOut > 0 || b.mem.pollinating || b.stung || m.TargetID != 0 {
		return false
	}
	return (b.nectar || b.mem.noNectar > beeTiredAfter || w.BeesStayInHive()) && !m.beeHiveNearFire(w)
}

// dropHive is Bee.dropHive.
func (m *Mob) dropHive() {
	m.bee.mem.hasHive = false
	m.bee.mem.hiveCooldown = beeLocateCooldown
}

// dropFlower is Bee.dropFlower.
func (m *Mob) dropFlower() {
	m.bee.mem.hasFlower = false
	m.bee.mem.flowerCooldown = 20 + m.Rand.Intn(41)
}

// setNectar is Bee.setHasNectar.
func (m *Mob) setNectar(v bool) {
	if v {
		m.bee.mem.noNectar = 0
	}
	m.SetMetadataBeeNectar(v)
}

// beeAIStep is the part of Bee.aiStep that runs with NoAI too: the hive and
// flower cooldowns and the every-20-ticks hive validity drop.
// vanilla: Bee.aiStep
func (m *Mob) beeAIStep(t *tickView) {
	b := &m.bee.mem
	b.stayOut = max(0, b.stayOut-1)
	b.hiveCooldown = max(0, b.hiveCooldown-1)
	b.flowerCooldown = max(0, b.flowerCooldown-1)
	if m.Age%20 == 0 && b.hasHive && !m.beeHiveValid(t.w) {
		b.hasHive = false
	}
}

// beeMemoryStep is the AI-gated part of the bee's memory: counting ticks
// without nectar and dropping an invalid remembered flower.
// vanilla: Bee.customServerAiStep, Bee.ValidateFlowerGoal
func (m *Mob) beeMemoryStep(t *tickView) {
	b := &m.bee.mem
	if !m.bee.nectar {
		b.noNectar++
	}
	// vanilla: ValidateFlowerGoal is a BaseBeeGoal, so anger skips it.
	if m.Age%20 != 0 || m.Angry() {
		return
	}
	if b.hasFlower && !rules.BeeAttracts(t.w.BlockAt(b.flower[0], b.flower[1], b.flower[2])) {
		m.dropFlower()
	}
}

// beeEnterHiveGoal is BeeEnterHiveGoal.
type beeEnterHiveGoal struct{ m *Mob }

func (g *beeEnterHiveGoal) Priority() int    { return 1 }
func (g *beeEnterHiveGoal) Flags() GoalFlags { return 0 }

func (g *beeEnterHiveGoal) CanStart(t *tickView) bool {
	b := &g.m.bee.mem
	if !b.hasHive || !g.m.beeWantsHive(t.w) {
		return false
	}
	dx, dy, dz := g.m.X-float64(b.hive[0])-0.5, g.m.Y-float64(b.hive[1])-0.5, g.m.Z-float64(b.hive[2])-0.5
	if dx*dx+dy*dy+dz*dz >= beeEnterRange*beeEnterRange {
		return false
	}
	n, ok := t.w.BeehiveOccupants(b.hive[0], b.hive[1], b.hive[2])
	if !ok {
		return false
	}
	if n >= rules.HiveMaxOccupants {
		b.hasHive = false
		return false
	}
	return !g.m.Angry()
}

func (g *beeEnterHiveGoal) ShouldContinue(*tickView) bool { return false }

func (g *beeEnterHiveGoal) Start(t *tickView) {
	h := g.m.bee.mem.hive
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobEnteredHive, X: float64(h[0]), Y: float64(h[1]), Z: float64(h[2])})
}

func (g *beeEnterHiveGoal) Tick(*tickView)          {}
func (g *beeEnterHiveGoal) Stop(*tickView)          {}
func (g *beeEnterHiveGoal) RequiresEveryTick() bool { return false }

// beeLocateHiveGoal is BeeLocateHiveGoal.
type beeLocateHiveGoal struct{ m *Mob }

func (g *beeLocateHiveGoal) Priority() int    { return 5 }
func (g *beeLocateHiveGoal) Flags() GoalFlags { return 0 }

func (g *beeLocateHiveGoal) CanStart(t *tickView) bool {
	b := &g.m.bee.mem
	return b.hiveCooldown == 0 && !b.hasHive && g.m.beeWantsHive(t.w) && !g.m.Angry()
}

func (g *beeLocateHiveGoal) ShouldContinue(*tickView) bool { return false }

func (g *beeLocateHiveGoal) Start(t *tickView) {
	b := &g.m.bee.mem
	b.hiveCooldown = beeLocateCooldown
	here := cellAt(g.m.X, g.m.Y, g.m.Z)
	var open [][3]int32
	for _, c := range t.w.BeehiveCells(here.X, here.Y, here.Z, beeLocateRange) {
		if n, ok := t.w.BeehiveOccupants(c[0], c[1], c[2]); ok && n < rules.HiveMaxOccupants {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return
	}
	d2 := func(c [3]int32) int32 {
		dx, dy, dz := c[0]-here.X, c[1]-here.Y, c[2]-here.Z
		return dx*dx + dy*dy + dz*dz
	}
	slices.SortStableFunc(open, func(a, c [3]int32) int {
		return int(d2(a) - d2(c))
	})
	for _, c := range open {
		if !slices.Contains(b.blacklist, c) {
			b.hive, b.hasHive = c, true
			return
		}
	}
	b.blacklist = nil
	b.hive, b.hasHive = open[0], true
}

func (g *beeLocateHiveGoal) Tick(*tickView)          {}
func (g *beeLocateHiveGoal) Stop(*tickView)          {}
func (g *beeLocateHiveGoal) RequiresEveryTick() bool { return false }

// beeFlyTowards starts a flight to the cell given. A hive within 16 blocks is
// pathed to directly, with a tenfold node budget, and reports whether the path
// ends beside it. Otherwise, and for a flower, the flight is a leg of up to 12
// blocks toward it, pathed on half the budget, and reports false when no leg
// is free.
// vanilla: Bee.BeeGoToHiveGoal.pathfindDirectlyTowards, Bee.pathfindRandomlyTowards
func (m *Mob) beeFlyTowards(t *tickView, p [3]int32, direct bool) bool {
	hx, hy, hz := float64(p[0])+0.5, float64(p[1])+0.5, float64(p[2])+0.5
	if direct && m.beeCloserThan(p, 16) {
		reach := int32(2)
		if m.beeCloserThan(p, 3) {
			reach = 1
		}
		_, reached := m.seekToFlyPath(t.w, hx, hy, hz, 1.0, reach, 10)
		return reached
	}
	dx, dy, dz := hx-m.X, hy-m.Y, hz-m.Z
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	for _, leg := range []float64{12, 8, 4} {
		f := min(1, leg/dist)
		cell := cellAt(m.X+dx*f, m.Y+dy*f, m.Z+dz*f)
		if flyCellPassable(t.w, cell.X, cell.Y, cell.Z, false, false, true) {
			m.seekToFlyPath(t.w, float64(cell.X)+0.5, float64(cell.Y)+0.5, float64(cell.Z)+0.5, 1.0, navReach, 0.5)
			return true
		}
	}
	return false
}

// beeGoToHiveGoal is BeeGoToHiveGoal.
type beeGoToHiveGoal struct {
	m          *Mob
	travelling int
}

func (g *beeGoToHiveGoal) Priority() int    { return 5 }
func (g *beeGoToHiveGoal) Flags() GoalFlags { return GoalMove }

func (g *beeGoToHiveGoal) CanStart(t *tickView) bool {
	b := &g.m.bee.mem
	if !b.hasHive || !g.m.beeCloserThan(b.hive, beeFarAway) || g.m.home.set || !g.m.beeWantsHive(t.w) || g.m.beeCloserThan(b.hive, 2) {
		return false
	}
	return registry.BlockOf(t.w.BlockAt(b.hive[0], b.hive[1], b.hive[2])).InTag(registry.TagIDBeehives) && !g.m.Angry()
}

func (g *beeGoToHiveGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *beeGoToHiveGoal) Start(*tickView)                 { g.travelling = 0 }
func (g *beeGoToHiveGoal) RequiresEveryTick() bool         { return false }

func (g *beeGoToHiveGoal) Stop(*tickView) {
	g.travelling = 0
	g.m.stopSeeking()
}

func (g *beeGoToHiveGoal) Tick(t *tickView) {
	m, b := g.m, &g.m.bee.mem
	if !b.hasHive {
		return
	}
	g.travelling++
	if g.travelling > beeMaxTravel {
		g.drop()
		return
	}
	if m.flightOn() {
		return
	}
	if !m.beeFlyTowards(t, b.hive, true) {
		g.drop()
	}
}

func (g *beeGoToHiveGoal) drop() {
	b := &g.m.bee.mem
	b.blacklist = append(b.blacklist, b.hive)
	if len(b.blacklist) > 3 {
		b.blacklist = b.blacklist[1:]
	}
	g.m.dropHive()
}

// beeGoToKnownFlowerGoal is BeeGoToKnownFlowerGoal: a bee 600 ticks without
// nectar flies to its remembered flower unless home-restricted.
type beeGoToKnownFlowerGoal struct {
	m          *Mob
	travelling int
}

func (g *beeGoToKnownFlowerGoal) Priority() int    { return 6 }
func (g *beeGoToKnownFlowerGoal) Flags() GoalFlags { return GoalMove }

func (g *beeGoToKnownFlowerGoal) CanStart(*tickView) bool {
	b := &g.m.bee.mem
	return b.hasFlower && !g.m.home.set && b.noNectar > beeKnownFlowerAfter && !g.m.beeCloserThan(b.flower, 2) && !g.m.Angry()
}

func (g *beeGoToKnownFlowerGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *beeGoToKnownFlowerGoal) Start(*tickView)                 { g.travelling = 0 }
func (g *beeGoToKnownFlowerGoal) RequiresEveryTick() bool         { return false }

func (g *beeGoToKnownFlowerGoal) Stop(*tickView) {
	g.travelling = 0
	g.m.stopSeeking()
}

func (g *beeGoToKnownFlowerGoal) Tick(t *tickView) {
	m, b := g.m, &g.m.bee.mem
	if !b.hasFlower {
		return
	}
	g.travelling++
	if g.travelling > beeMaxTravel {
		m.dropFlower()
		return
	}
	if m.flightOn() {
		return
	}
	if !m.beeCloserThan(b.flower, beeFarAway) || !m.beeFlyTowards(t, b.flower, false) {
		m.dropFlower()
	}
}

// beePollinateGoal is BeePollinateGoal.
type beePollinateGoal struct {
	m                *Mob
	successful, tick int
	lastSound        int
	hover            [3]float64
	hasHover         bool
}

func (g *beePollinateGoal) Priority() int    { return 4 }
func (g *beePollinateGoal) Flags() GoalFlags { return GoalMove }

func (g *beePollinateGoal) CanStart(t *tickView) bool {
	m, b := g.m, &g.m.bee.mem
	if b.flowerCooldown > 0 || m.bee.nectar || t.w.IsRaining() {
		return false
	}
	// vanilla: BlockPos.withinManhattan visits the nearest cells first
	here := cellAt(m.X, m.Y, m.Z)
	best, bestDist := Waypoint{}, beeFlowerRadius+1
	for dx := int32(-beeFlowerRadius); dx <= beeFlowerRadius; dx++ {
		for dy := int32(-beeFlowerRadius); dy <= beeFlowerRadius; dy++ {
			for dz := int32(-beeFlowerRadius); dz <= beeFlowerRadius; dz++ {
				d := int(abs32(dx) + abs32(dy) + abs32(dz))
				if d >= bestDist {
					continue
				}
				x, y, z := here.X+dx, here.Y+dy, here.Z+dz
				if rules.BeeAttracts(t.w.BlockAt(x, y, z)) && flyCellPassable(t.w, x, y+1, z, false, false, true) {
					best, bestDist = Waypoint{X: x, Y: y, Z: z}, d
				}
			}
		}
	}
	if bestDist <= beeFlowerRadius {
		b.flower, b.hasFlower = [3]int32{best.X, best.Y, best.Z}, true
		m.seekToFly(float64(best.X)+0.5, float64(best.Y)+0.5, float64(best.Z)+0.5, 1.2)
		// vanilla: BaseBeeGoal.canUse tests anger after canBeeUse's side effects
		return !m.Angry()
	}
	b.flowerCooldown = 20 + m.Rand.Intn(41)
	return false
}

func (g *beePollinateGoal) longEnough() bool { return g.successful > beePollinateTicks }

func (g *beePollinateGoal) ShouldContinue(t *tickView) bool {
	b := &g.m.bee.mem
	if !b.pollinating || !b.hasFlower || t.w.IsRaining() || g.m.Angry() {
		return false
	}
	if g.longEnough() {
		return g.m.Rand.Float32() < 0.2
	}
	return true
}

func (g *beePollinateGoal) Start(*tickView) {
	g.successful, g.tick, g.lastSound, g.hasHover = 0, 0, 0, false
	g.m.bee.mem.pollinating = true
	g.m.bee.mem.noNectar = 0
}

func (g *beePollinateGoal) Stop(*tickView) {
	if g.longEnough() {
		g.m.setNectar(true)
	}
	g.m.bee.mem.pollinating = false
	g.m.stopSeeking()
	g.m.bee.mem.flowerCooldown = 200
}

func (g *beePollinateGoal) RequiresEveryTick() bool { return true }

func (g *beePollinateGoal) Tick(t *tickView) {
	m, b := g.m, &g.m.bee.mem
	if !b.hasFlower {
		return
	}
	g.tick++
	if g.tick > beePollinateGiveUp {
		m.dropFlower()
		b.pollinating = false
		b.flowerCooldown = 200
		return
	}
	over := [3]float64{float64(b.flower[0]) + 0.5, float64(b.flower[1]) + beeHoverHeight, float64(b.flower[2]) + 0.5}
	if Dist2(m.X, m.Y, m.Z, over[0], over[1], over[2]) > 1 {
		g.hover, g.hasHover = over, true
		m.seekToFly(g.hover[0], g.hover[1], g.hover[2], beeHoverPace)
		return
	}
	if !g.hasHover {
		g.hover, g.hasHover = over, true
	}
	arrived := Dist2(m.X, m.Y, m.Z, g.hover[0], g.hover[1], g.hover[2]) <= 0.01
	set := true
	if arrived {
		if m.Rand.Intn(25) == 0 {
			g.hover = [3]float64{over[0] + g.offset(), over[1], over[2] + g.offset()}
			m.stopSeeking()
		} else {
			set = false
		}
	}
	if set {
		m.seekToFly(g.hover[0], g.hover[1], g.hover[2], beeHoverPace)
	}
	g.successful++
	if m.Rand.Float32() < 0.05 && g.successful > g.lastSound+beePollinateSoundGap {
		g.lastSound = g.successful
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityBeePollinate, Volume: 1, Pitch: 1})
	}
}

func (g *beePollinateGoal) offset() float64 {
	return (float64(g.m.Rand.Float32())*2 - 1) * beeHoverJitter
}

// beeGrowChance is the one-in-N roll a grow-crop tick grows on, before the
// goal's tick delay halves it. vanilla: BeeGrowCropGoal.GROW_CHANCE
const beeGrowChance = 30

// beeGrowCropGoal is BeeGrowCropGoal.
type beeGrowCropGoal struct{ m *Mob }

func (g *beeGrowCropGoal) Priority() int    { return 7 }
func (g *beeGrowCropGoal) Flags() GoalFlags { return 0 }

func (g *beeGrowCropGoal) CanStart(t *tickView) bool {
	if g.m.bee.mem.crops >= beeMaxCrops || g.m.Rand.Float32() < 0.3 {
		return false
	}
	return g.m.bee.nectar && g.m.beeHiveValid(t.w) && !g.m.Angry()
}

func (g *beeGrowCropGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *beeGrowCropGoal) Start(*tickView)                 {}
func (g *beeGrowCropGoal) Stop(*tickView)                  {}
func (g *beeGrowCropGoal) RequiresEveryTick() bool         { return false }

func (g *beeGrowCropGoal) Tick(t *tickView) {
	// vanilla: adjustedTickDelay, halved for a goal ticked every other tick
	if g.m.Rand.Intn(reducedTickDelay(beeGrowChance)) != 0 {
		return
	}
	here := cellAt(g.m.X, g.m.Y, g.m.Z)
	for i := int32(1); i <= 2; i++ {
		next, ok := rules.BeeGrownCrop(t.w.BlockAt(here.X, here.Y-i, here.Z))
		if !ok {
			continue
		}
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobGrewCrop,
			X: float64(here.X), Y: float64(here.Y - i), Z: float64(here.Z), BlockState: next})
		g.m.bee.mem.crops++
	}
}
