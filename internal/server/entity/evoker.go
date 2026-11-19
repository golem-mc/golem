package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The evoker: an illager that keeps its distance and casts in turn fang rows
// or rings at its target, up to three vexes at a time, and wololo on blue
// sheep. Not modelled: the vexes' scoreboard team and raid wave spawning.
// vanilla: Evoker, SpellcasterIllager

// TypeEvoker is the evoker.
const TypeEvoker = registry.EntityTypeEvoker

// The spells' ids in the spellcasting metadata byte.
// vanilla: SpellcasterIllager.IllagerSpell
const (
	SpellNone      uint8 = 0
	SpellSummonVex uint8 = 1
	SpellFangs     uint8 = 2
	SpellWololo    uint8 = 3
	SpellDisappear uint8 = 4
	SpellBlindness uint8 = 5
)

// The evoker's numbers.
// vanilla: Evoker, Evoker.EvokerSummonSpellGoal, Evoker.EvokerAttackSpellGoal
const (
	evokerAvoidDist = 8.0
	evokerAvoidWalk = 0.6
	evokerAvoidRun  = 1.0
	evokerMemory    = 300 // setUnseenMemoryTicks on the player and villager targets
	// evokerVexRange is the reach of the box the summon spell counts vexes in.
	evokerVexRange = 16.0
	evokerVexMax   = 8 // the summon rolls nextInt(8) + 1 against the count
	evokerVexCount = 3
	// evokerVexSpread is the horizontal nextInt bound of a summoned vex's
	// block around the evoker, centred on it.
	evokerVexSpread = 5
	// evokerVexLifeBase and evokerVexLifeSpread give a vex 30 to 119 seconds.
	evokerVexLifeBase   = 30
	evokerVexLifeSpread = 90
	// The fang rows: a line of 16 beyond three blocks, a ring of five at 1.5
	// and eight at 2.5 inside.
	evokerFangsLine    = 16
	evokerFangsStep    = 1.25
	evokerFangsNearSq  = 9.0
	evokerFangsInner   = 5
	evokerFangsOuter   = 8
	evokerFangsInnerR  = 1.5
	evokerFangsOuterR  = 2.5
	evokerFangsOuterAt = 3 // the outer ring's warmup
	// evokerWololoRange and evokerWololoRise are the sheep search's reach
	// around the evoker, flat and vertical.
	evokerWololoRange = 16.0
	evokerWololoRise  = 4.0
)

func init() {
	// vanilla: EntityTypes.EVOKER and Evoker.createAttributes
	kinds[TypeEvoker] = &kind{
		name: "Evoker", width: 0.6, height: 1.95, eyeHeight: 1.6575,
		maxHealth: 24, speed: 0.5, hostile: true, followRange: 12,
		sounds: MobSounds{
			Ambient: registry.SoundEntityEvokerAmbient,
			Hurt:    registry.SoundEntityEvokerHurt,
			Death:   registry.SoundEntityEvokerDeath,
		},
	}
	mobTracking[TypeEvoker] = tracking{rangeChunks: 8, updateInterval: 3}
}

// spellState is what a spellcaster keeps beyond the common mob.
type spellState struct {
	// current is the spell on the metadata byte, SpellNone between casts.
	current uint8
	// castTicks is spellCastingTickCount, counted down once a tick.
	castTicks int
	// wololo is the entity ID of the sheep the wololo spell is aimed at.
	wololo int32
}

// Spell is the spellcasting metadata byte.
func (m *Mob) Spell() uint8 { return m.spell.current }

// SpellTicks is spellCastingTickCount, what a saved evoker keeps.
func (m *Mob) SpellTicks() int { return m.spell.castTicks }

// SetSpellTicks restores spellCastingTickCount.
func (m *Mob) SetSpellTicks(ticks int) { m.spell.castTicks = ticks }

// spellCastSound is getCastingSoundEvent.
func spellCastSound(t MobType) registry.Sound {
	if t == TypeIllusioner {
		return registry.SoundEntityIllusionerCastSpell
	}
	return registry.SoundEntityEvokerCastSpell
}

// evokerStep is Mob.customServerAiStep, behind the goals.
// vanilla: SpellcasterIllager.customServerAiStep
func (m *Mob) evokerStep() {
	if m.spell.castTicks > 0 {
		m.spell.castTicks--
	}
}

// evokerGoals wires Evoker.registerGoals in priority order across both
// selectors.
func evokerGoals(m *Mob) []Goal {
	return withRaidGoals(m, []Goal{
		newFloatGoal(m),
		&hurtByGoal{m: m, priority: 1, alertOthers: true, ignore: isRaiderType},
		&castingSpellGoal{m: m},
		newObtainBannerGoal(m),
		&nearestTargetGoal{m: m, priority: 2, memory: evokerMemory},
		&avoidGoal{m: m, priority: 2, active: func() bool { return true },
			maxDist: evokerAvoidDist, walk: evokerAvoidWalk, sprint: evokerAvoidRun},
		newCreakingAvoidGoal(m, 3, evokerAvoidWalk, evokerAvoidRun),
		&mobTargetGoal{m: m, priority: 3, unsighted: true, memory: evokerMemory, matches: illagerVillagerTarget},
		&mobTargetGoal{m: m, priority: 3, unsighted: true, matches: func(o *Mob) bool { return o.Type == TypeIronGolem }},
		&spellGoal{m: m, priority: 4, spell: summonSpell{}},
		&spellGoal{m: m, priority: 5, spell: fangsSpell{}},
		&spellGoal{m: m, priority: 6, spell: wololoSpell{}},
		&randomStroll{m: m, priority: 8, pace: 0.6},
		&lookAtPlayer{m: m, priority: 9, reach: 3, chance: 1},
		&lookAtMob{m: m, priority: 10},
	})
}

// castingSpellGoal is Evoker.EvokerCastingSpellGoal: stand still and face the
// target for as long as the spell lasts.
type castingSpellGoal struct{ m *Mob }

func (g *castingSpellGoal) Priority() int    { return 1 }
func (g *castingSpellGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *castingSpellGoal) CanStart(*tickView) bool       { return g.m.spell.castTicks > 0 }
func (g *castingSpellGoal) ShouldContinue(*tickView) bool { return g.m.spell.castTicks > 0 }
func (g *castingSpellGoal) RequiresEveryTick() bool       { return false }

func (g *castingSpellGoal) Start(*tickView) { g.m.stopSeeking() }

func (g *castingSpellGoal) Stop(*tickView) { g.m.SetMetadataSpellCurrent(SpellNone) }

func (g *castingSpellGoal) Tick(t *tickView) {
	m := g.m
	if p := m.targetIn(t); p != nil {
		m.lookAtTarget(p)
	} else if o := t.mobByID(m.spell.wololo); o != nil {
		p := o.asTarget()
		m.lookAtTarget(&p)
	}
}

// spellKind is what one spell decides inside the shared use-spell goal.
type spellKind interface {
	id() uint8
	warmup() int
	castingTime() int
	interval() int
	prepareSound() registry.Sound
	canUse(g *spellGoal, t *tickView) bool
	keeps(g *spellGoal, t *tickView) bool
	perform(g *spellGoal, t *tickView)
	stop(g *spellGoal)
}

// spellGoal is SpellcasterIllager.SpellcasterUseSpellGoal.
type spellGoal struct {
	m        *Mob
	priority int
	spell    spellKind
	// warmupDelay counts the goal's ticks down to the cast; nextAttack is the
	// age the next cast is allowed from.
	warmupDelay int
	nextAttack  int
}

func (g *spellGoal) Priority() int    { return g.priority }
func (g *spellGoal) Flags() GoalFlags { return 0 }

func (g *spellGoal) RequiresEveryTick() bool { return false }

// baseCanUse is the use-spell goal's own canUse: a live target, no spell in
// the air and the interval run out.
func (g *spellGoal) baseCanUse(t *tickView) bool {
	return g.m.targetIn(t) != nil && g.m.spell.castTicks <= 0 && g.m.Age >= g.nextAttack
}

func (g *spellGoal) CanStart(t *tickView) bool { return g.spell.canUse(g, t) }

func (g *spellGoal) ShouldContinue(t *tickView) bool { return g.spell.keeps(g, t) }

func (g *spellGoal) Start(t *tickView) {
	m := g.m
	g.warmupDelay = reducedTickDelay(g.spell.warmup())
	m.spell.castTicks = g.spell.castingTime()
	g.nextAttack = m.Age + g.spell.interval()
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: g.spell.prepareSound(), Volume: 1, Pitch: 1})
	m.SetMetadataSpellCurrent(g.spell.id())
	if s, ok := g.spell.(interface{ start(*spellGoal) }); ok {
		s.start(g)
	}
}

func (g *spellGoal) Tick(t *tickView) {
	g.warmupDelay--
	if g.warmupDelay == 0 {
		g.spell.perform(g, t)
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobSounded, SoundID: spellCastSound(g.m.Type), Volume: 1, Pitch: 1})
	}
}

func (g *spellGoal) Stop(*tickView) { g.spell.stop(g) }

// keepsOnTarget is the use-spell goal's canContinueToUse.
func (g *spellGoal) keepsOnTarget(t *tickView) bool {
	return g.m.targetIn(t) != nil && g.warmupDelay > 0
}

// summonSpell is Evoker.EvokerSummonSpellGoal.
type summonSpell struct{}

func (summonSpell) id() uint8                            { return SpellSummonVex }
func (summonSpell) warmup() int                          { return 20 }
func (summonSpell) castingTime() int                     { return 100 }
func (summonSpell) interval() int                        { return 340 }
func (summonSpell) prepareSound() registry.Sound         { return registry.SoundEntityEvokerPrepareSummon }
func (summonSpell) stop(*spellGoal)                      {}
func (summonSpell) keeps(g *spellGoal, t *tickView) bool { return g.keepsOnTarget(t) }

func (summonSpell) canUse(g *spellGoal, t *tickView) bool {
	if !g.baseCanUse(t) {
		return false
	}
	m := g.m
	vexes := 0
	for _, o := range t.others {
		if o != m && o.Type == TypeVex && o.Alive() && overlapsInflated(m, o, evokerVexRange, evokerVexRange) {
			vexes++
		}
	}
	return m.Rand.Intn(evokerVexMax)+1 > vexes
}

// perform asks for three vexes, each around a block just above the evoker.
func (summonSpell) perform(g *spellGoal, t *tickView) {
	m := g.m
	at := m.cell()
	for range evokerVexCount {
		x := at.X - 2 + int32(m.Rand.Intn(evokerVexSpread))
		z := at.Z - 2 + int32(m.Rand.Intn(evokerVexSpread))
		life := 20 * (evokerVexLifeBase + m.Rand.Intn(evokerVexLifeSpread))
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSummonedVex,
			X: float64(x), Y: float64(at.Y + 1), Z: float64(z), Amount: life})
	}
}

// fangsSpell is Evoker.EvokerAttackSpellGoal.
type fangsSpell struct{}

func (fangsSpell) id() uint8                             { return SpellFangs }
func (fangsSpell) warmup() int                           { return 20 }
func (fangsSpell) castingTime() int                      { return 40 }
func (fangsSpell) interval() int                         { return 100 }
func (fangsSpell) prepareSound() registry.Sound          { return registry.SoundEntityEvokerPrepareAttack }
func (fangsSpell) stop(*spellGoal)                       {}
func (fangsSpell) canUse(g *spellGoal, t *tickView) bool { return g.baseCanUse(t) }
func (fangsSpell) keeps(g *spellGoal, t *tickView) bool  { return g.keepsOnTarget(t) }

// perform lays a ring around a target in reach and a line toward a far one.
func (fangsSpell) perform(g *spellGoal, t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	minY := math.Min(p.Y, m.Y)
	maxY := math.Max(p.Y, m.Y) + 1
	toward := float32(math.Atan2(p.Z-m.Z, p.X-m.X))
	if Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < evokerFangsNearSq {
		for i := range evokerFangsInner {
			a := toward + float32(i)*float32(math.Pi)*0.4
			g.fangs(t, m.X+cosf(a)*evokerFangsInnerR, m.Z+sinf(a)*evokerFangsInnerR, minY, maxY, a, 0)
		}
		for i := range evokerFangsOuter {
			a := toward + float32(i)*float32(math.Pi)*2/8 + 1.2566371
			g.fangs(t, m.X+cosf(a)*evokerFangsOuterR, m.Z+sinf(a)*evokerFangsOuterR, minY, maxY, a, evokerFangsOuterAt)
		}
		return
	}
	for i := range evokerFangsLine {
		reach := evokerFangsStep * float64(i+1)
		g.fangs(t, m.X+cosf(toward)*reach, m.Z+sinf(toward)*reach, minY, maxY, toward, i)
	}
}

func cosf(a float32) float64 { return float64(float32(math.Cos(float64(a)))) }
func sinf(a float32) float64 { return float64(float32(math.Sin(float64(a)))) }

// fangs is createSpellEntity: walk down from maxY to the first block with a
// full top face and put a fang on it; none if no floor is found down to
// minY.
func (g *spellGoal) fangs(t *tickView, x, z, minY, maxY float64, angle float32, delay int) {
	cx, cz := int32(math.Floor(x)), int32(math.Floor(z))
	y := int32(math.Floor(maxY))
	floor := int32(math.Floor(minY)) - 1
	for ; y >= floor; y-- {
		if !registry.FaceSturdy(t.w.BlockAt(cx, y-1, cz), 1, registry.SupportFull) {
			continue
		}
		top := 0.0
		if state := t.w.BlockAt(cx, y, cz); !world.IsAirState(state) {
			top = blockExtent(state)
		}
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobCastFangs,
			X: x, Y: float64(y) + top, Z: z, Radius: float64(angle), Amount: delay})
		return
	}
}

// wololoSpell is Evoker.EvokerWololoSpellGoal.
type wololoSpell struct{}

func (wololoSpell) id() uint8                    { return SpellWololo }
func (wololoSpell) warmup() int                  { return 40 }
func (wololoSpell) castingTime() int             { return 60 }
func (wololoSpell) interval() int                { return 140 }
func (wololoSpell) prepareSound() registry.Sound { return registry.SoundEntityEvokerPrepareWololo }

func (wololoSpell) stop(g *spellGoal) { g.m.spell.wololo = 0 }

func (wololoSpell) keeps(g *spellGoal, _ *tickView) bool {
	return g.m.spell.wololo != 0 && g.warmupDelay > 0
}

// canUse picks a random blue sheep near the evoker, only while it hunts
// nothing and mob_griefing allows it.
func (wololoSpell) canUse(g *spellGoal, t *tickView) bool {
	m := g.m
	if m.TargetID != 0 || m.spell.castTicks > 0 || m.Age < g.nextAttack || !t.w.MobGriefing() {
		return false
	}
	var sheep []*Mob
	for _, o := range t.others {
		if o.Type == TypeSheep && o.Alive() && o.Colour == "blue" &&
			overlapsInflated(m, o, evokerWololoRange, evokerWololoRise) {
			sheep = append(sheep, o)
		}
	}
	if len(sheep) == 0 {
		return false
	}
	m.spell.wololo = sheep[m.Rand.Intn(len(sheep))].ID
	return true
}

// perform turns the sheep red, whether or not it is sheared.
func (wololoSpell) perform(g *spellGoal, t *tickView) {
	if o := t.mobByID(g.m.spell.wololo); o != nil && o.Alive() {
		o.SetMetadataColour("red")
	}
}

// overlapsInflated reports whether o's box meets m's box grown by rx on each
// horizontal side and ry above and below, the search box of getNearbyEntities.
func overlapsInflated(m, o *Mob, rx, ry float64) bool {
	reach := (m.width()+o.width())/2 + rx
	return math.Abs(o.X-m.X) < reach && math.Abs(o.Z-m.Z) < reach &&
		o.Y < m.Y+m.height()+ry && o.Y+o.height() > m.Y-ry
}
