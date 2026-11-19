package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypeSniffer is minecraft:sniffer.
const TypeSniffer = registry.EntityTypeSniffer

// SnifferState is Sniffer.State's id, the synched state.
type SnifferState int

// vanilla: Sniffer.State
const (
	SnifferIdling SnifferState = iota
	SnifferFeelingHappy
	SnifferScenting
	SnifferSniffing
	SnifferSearching
	SnifferDigging
	SnifferRising
)

// The sniffer's numbers.
// vanilla: SnifferAi, Sniffer
const (
	snifferSniffCooldown = 9600 // SNIFFING_COOLDOWN_TICKS
	snifferSeedDelay     = 120  // DIGGING_DROP_SEED_OFFSET_TICKS
	snifferDigPace       = 1.25 // SPEED_MULTIPLIER_WHEN_SNIFFING
	snifferBabyAge       = -48000
	snifferDigHeight     = float64(float32(1.75) - float32(0.4)) // vanilla: DIGGING_DIMENSIONS
	snifferDigEye        = float64(float32(0.81))
	snifferExploredMax   = 20
	snifferHeadReach     = 2.25
	snifferSearchTicks   = 600
	// MobSnifferDug's entity event, sent when a dig starts.
	snifferDigStatus = 63
)

var snifferDiggable = registry.BlockTagOf("minecraft:sniffer_diggable_block")

// snifferActivity is Brain.setActiveActivityToFirstValid(DIG, SNIFF, IDLE).
type snifferActivity uint8

const (
	snifferNoActivity snifferActivity = iota
	snifferIdle
	snifferSniff
	snifferDig
)

// snifferSpot is a GlobalPos: an explored cell with its dimension.
type snifferSpot struct {
	dim string
	pos Waypoint
}

// snifferBrain holds the sniffer's synched state and the brain memories its
// behaviors share.
type snifferBrain struct {
	state SnifferState
	// dropSeedAt is DATA_DROP_SEED_AT_TICK, a tick of Mob.Age.
	dropSeedAt int
	// digging, happy, hasTarget and hasWalk are the SNIFFER_DIGGING, SNIFFER_HAPPY,
	// SNIFFER_SNIFFING_TARGET and WALK_TARGET memories (target and walk are cells).
	digging, happy     bool
	hasTarget, hasWalk bool
	target, walk       Waypoint
	// sniffCooldown and temptCooldown count SNIFF_COOLDOWN and
	// TEMPTATION_COOLDOWN_TICKS down; zero is absent.
	sniffCooldown, temptCooldown int
	// explored is SNIFFER_EXPLORED_POSITIONS, newest first.
	explored []snifferSpot
	// w is the world the last goal call saw, for the behaviors' conditions.
	w World
	// pending holds sounds and events a transition queued, sent by snifferStep.
	pending []MobEvent
}

func init() {
	// vanilla: EntityTypes.SNIFFER, Sniffer.createAttributes
	kinds[TypeSniffer] = &kind{
		maxHeadYRot: 50,
		name:        "Sniffer", width: float64(float32(1.9)), height: float64(float32(1.75)), eyeHeight: float64(float32(1.05)),
		babyWidth: float64(float32(1.9) * float32(0.5)), babyHeight: float64(float32(1.75) * float32(0.5)), babyEye: float64(float32(1.05) * float32(0.5)),
		babyable: true, maxHealth: 14, speed: float64(float32(0.1)),
		temptTag: &registry.ItemTagIDSnifferFood, temptPace: 1.25, strollPace: 1,
		ambientInterval: 120, fireAverse: true,
		sounds: MobSounds{Ambient: registry.SoundEntitySnifferIdle, Hurt: registry.SoundEntitySnifferHurt, Death: registry.SoundEntitySnifferDeath, Eat: registry.SoundEntitySnifferEat, Step: registry.SoundEntitySnifferStep},
	}
	mobTracking[TypeSniffer] = tracking{rangeChunks: 10, updateInterval: 3}
}

// SnifferStateOf is the synched Sniffer.DATA_STATE.
func (m *Mob) SnifferStateOf() SnifferState { return m.sniffer.state }

// SnifferDropSeedAt is the synched DATA_DROP_SEED_AT_TICK.
func (m *Mob) SnifferDropSeedAt() int { return m.sniffer.dropSeedAt }

// StartSnifferSearch sends the sniffer walking to a block, to dig there when
// it arrives, as a finished sniffing would.
func (m *Mob) StartSnifferSearch(x, y, z int32) {
	b := &m.sniffer
	b.target, b.walk, b.hasTarget, b.hasWalk = Waypoint{x, y, z}, Waypoint{x, y, z}, true, true
}

func snifferGoals(m *Mob) []Goal {
	b := &m.sniffer
	reset := m.snifferReset
	// vanilla: SnifferAi.initCoreActivity, initIdleActivity, initSniffingActivity, initDigActivity
	scenting := m.snifferBehavior(snifferIdle, 40, 80, nil)
	scenting.ok = func() bool {
		return !b.digging && !b.hasTarget && !b.happy && !m.snifferBreeding() && !m.snifferTempted() && !m.panicking()
	}
	scenting.begin = func() { m.snifferTransition(SnifferScenting) }
	scenting.end = func(bool) { m.snifferTransition(SnifferIdling) }
	sniffing := m.snifferBehavior(snifferIdle, 40, 80, nil)
	sniffing.ok = func() bool {
		return !b.hasWalk && !b.hasTarget && b.sniffCooldown == 0 && !m.IsBaby() && m.snifferCanSniff()
	}
	sniffing.keep = m.snifferCanSniff
	sniffing.begin = func() { m.snifferTransition(SnifferSniffing) }
	sniffing.end = func(finished bool) {
		m.snifferTransition(SnifferIdling)
		if finished {
			m.snifferPickTarget()
		}
	}
	happy := m.snifferBehavior(snifferIdle, 40, 100, nil)
	happy.priority, happy.flags = 7, 0
	happy.ok = func() bool { return b.happy }
	happy.begin = func() { m.snifferTransition(SnifferFeelingHappy) }
	happy.end = func(bool) { m.snifferTransition(SnifferIdling); b.happy = false }
	idle := &runOneGoal{m: m, priority: 8, choices: []Goal{
		scenting, sniffing, &lookAtPlayer{m: m, reach: 6, chance: 1}, &randomStroll{m: m, pace: 1, interval: 1},
		m.snifferBehavior(snifferIdle, 5, 20, nil), m.snifferBehavior(snifferIdle, 5, 20, nil),
	}}
	search := m.snifferBehavior(snifferSniff, snifferSearchTicks, snifferSearchTicks, nil)
	search.priority, search.flags = 4, 0
	search.ok = m.snifferCanSniff
	search.keep = func() bool {
		if !m.snifferCanSniff() {
			m.snifferTransition(SnifferIdling)
			return false
		}
		return b.hasWalk && b.hasTarget && b.walk == b.target
	}
	search.begin = func() { m.snifferTransition(SnifferSearching) }
	search.end = func(bool) {
		if m.snifferCanDig() && m.snifferCanSniff() {
			b.digging = true
		}
		b.hasWalk, b.hasTarget = false, false
	}
	dig := m.snifferBehavior(snifferDig, 160, 180, nil)
	dig.priority = 3
	dig.ok = func() bool { return b.sniffCooldown == 0 && m.snifferCanSniff() }
	dig.keep = func() bool { return b.digging && m.snifferCanDig() && !m.IsInLove() }
	dig.begin = func() { m.snifferTransition(SnifferDigging) }
	dig.end = func(finished bool) {
		if finished {
			b.sniffCooldown = snifferSniffCooldown
		} else {
			m.snifferReset()
		}
	}
	finish := m.snifferBehavior(snifferDig, 40, 40, nil)
	finish.priority = 3
	finish.ok = func() bool { return b.sniffCooldown > 0 }
	finish.keep = func() bool { return b.digging }
	finish.begin = func() { m.snifferTransition(SnifferRising) }
	finish.end = func(finished bool) {
		m.snifferTransition(SnifferIdling)
		if finished {
			if b.w != nil {
				b.explored = append([]snifferSpot{{b.w.DimensionName(), m.onPos()}}, b.explored[:min(len(b.explored), snifferExploredMax)]...)
			}
		}
		b.digging, b.happy = false, true
	}
	return []Goal{newFloatGoal(m), &panicGoal{m: m, brain: true, pace: 2, onStart: reset}, &snifferWalkGoal{m: m},
		dig, finish, search,
		&breedGoal{m: m, priority: 5, onStart: reset}, &temptGoal{m: m, priority: 6, cooldownTicks: &b.temptCooldown, onStart: reset},
		happy, idle}
}

// snifferWalkGoal is MoveToTargetSink for the WALK_TARGET memory: it walks to
// the cell and forgets the target when the path is done.
type snifferWalkGoal struct{ m *Mob }

func (g *snifferWalkGoal) Priority() int    { return 2 }
func (g *snifferWalkGoal) Flags() GoalFlags { return GoalMove }
func (g *snifferWalkGoal) CanStart(t *tickView) bool {
	b := &g.m.sniffer
	if !b.hasWalk {
		return false
	}
	if g.m.cell() == b.walk || !g.m.seekTo(t.w, float64(b.walk.X)+0.5, float64(b.walk.Y), float64(b.walk.Z)+0.5, snifferDigPace, 0) {
		b.hasWalk = false
		return false
	}
	return true
}
func (g *snifferWalkGoal) Start(*tickView) {}
func (g *snifferWalkGoal) ShouldContinue(*tickView) bool {
	return g.m.sniffer.hasWalk && len(g.m.navOf().nav.path) > 0
}
func (g *snifferWalkGoal) Tick(*tickView) {}
func (g *snifferWalkGoal) Stop(*tickView) {
	g.m.stopSeeking()
	g.m.sniffer.hasWalk = false
}
func (g *snifferWalkGoal) RequiresEveryTick() bool { return true }

// snifferBehavior is a brain Behavior: it starts in its activity while ok
// holds and runs for min..max ticks while keep holds; end learns whether it
// timed out.
type snifferBehavior struct {
	m         *Mob
	priority  int
	flags     GoalFlags
	activity  snifferActivity
	min, max  int
	ok, keep  func() bool
	begin     func()
	end       func(finished bool)
	ran, span int
}

func (m *Mob) snifferBehavior(a snifferActivity, min, max int, ok func() bool) *snifferBehavior {
	return &snifferBehavior{m: m, activity: a, min: min, max: max, flags: GoalMove | GoalLook, ok: ok,
		keep: func() bool { return true }, begin: func() {}, end: func(bool) {}}
}

func (g *snifferBehavior) Priority() int    { return g.priority }
func (g *snifferBehavior) Flags() GoalFlags { return g.flags }
func (g *snifferBehavior) CanStart(t *tickView) bool {
	g.m.sniffer.w = t.w
	return g.m.snifferActivityNow() == g.activity && (g.ok == nil || g.ok())
}
func (g *snifferBehavior) Start(t *tickView) {
	g.m.sniffer.w = t.w
	g.ran, g.span = 0, g.min+g.m.Rand.Intn(g.max-g.min+1)
	g.begin()
}

// ShouldContinue is Behavior.tickOrStop: a behavior runs span+1 ticks.
func (g *snifferBehavior) ShouldContinue(t *tickView) bool {
	g.m.sniffer.w = t.w
	return g.ran <= g.span && g.keep()
}
func (g *snifferBehavior) Tick(*tickView) { g.ran++ }
func (g *snifferBehavior) Stop(t *tickView) {
	g.m.sniffer.w = t.w
	g.end(g.ran > g.span)
}
func (g *snifferBehavior) RequiresEveryTick() bool { return true }

// snifferActivityNow is the active activity.
// vanilla: SnifferAi.updateActivity
func (m *Mob) snifferActivityNow() snifferActivity {
	b := &m.sniffer
	switch {
	case !m.panicking() && !b.hasWalk && b.digging:
		return snifferDig
	case !m.panicking() && b.hasTarget && b.hasWalk:
		return snifferSniff
	case !b.digging:
		return snifferIdle
	}
	return snifferNoActivity
}

func (m *Mob) snifferBreeding() bool {
	for _, g := range m.ai.running {
		if _, ok := g.(*breedGoal); ok {
			return true
		}
	}
	return false
}

func (m *Mob) snifferTempted() bool {
	for _, g := range m.ai.running {
		if _, ok := g.(*temptGoal); ok {
			return true
		}
	}
	return false
}

// snifferCanSniff is Sniffer.canSniff.
func (m *Mob) snifferCanSniff() bool {
	return !m.snifferTempted() && !m.panicking() && !m.InWater() && !m.IsInLove() && m.OnGround && !m.isPassenger() && m.LeashHolderID == 0
}

// isPassenger reports a seat in a boat, minecart or on a mob mount.
// vanilla: Entity.isPassenger
func (m *Mob) isPassenger() bool { return m.RidesVehicle || m.Vehicle != nil }

// snifferReset is SnifferAi.resetSniffing.
func (m *Mob) snifferReset() {
	b := &m.sniffer
	b.digging, b.hasTarget, b.hasWalk = false, false, false
	m.snifferTransition(SnifferIdling)
}

// snifferTransition is Sniffer.transitionTo; the sounds ride events.
func (m *Mob) snifferTransition(s SnifferState) {
	m.SetMetadataSnifferState(s)
	switch s {
	case SnifferScenting:
		pitch := float32(1)
		if m.IsBaby() {
			pitch = 1.3
		}
		m.snifferSound(registry.SoundEntitySnifferScenting, pitch)
	case SnifferSniffing:
		m.snifferSound(registry.SoundEntitySnifferSniffing, 1)
	case SnifferDigging:
		m.SetMetadataSnifferDropSeedAt(m.Age + snifferSeedDelay)
		m.sniffer.pending = append(m.sniffer.pending, MobEvent{Mob: m, Kind: MobStatus, Status: snifferDigStatus})
	case SnifferRising:
		m.snifferSound(registry.SoundEntitySnifferDiggingStop, 1)
	case SnifferFeelingHappy:
		m.snifferSound(registry.SoundEntitySnifferHappy, 1)
	}
}

func (m *Mob) snifferSound(sound registry.Sound, pitch float32) {
	m.sniffer.pending = append(m.sniffer.pending, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: 1, Pitch: pitch})
}

// snifferCanMate is the state half of Sniffer.canMate.
func (m *Mob) snifferCanMate() bool {
	s := m.sniffer.state
	return s == SnifferIdling || s == SnifferScenting || s == SnifferFeelingHappy
}

// snifferHead is Sniffer.getHeadBlock.
func (m *Mob) snifferHead() Waypoint {
	yaw, pitch := float64(m.Yaw)*math.Pi/180, float64(m.Pitch)*math.Pi/180
	x := m.X + -math.Sin(yaw)*math.Cos(pitch)*snifferHeadReach
	z := m.Z + math.Cos(yaw)*math.Cos(pitch)*snifferHeadReach
	return Waypoint{int32(math.Floor(x)), int32(math.Floor(m.Y + float64(float32(0.2)))), int32(math.Floor(z))}
}

// snifferCanDig is Sniffer.canDig().
func (m *Mob) snifferCanDig() bool {
	w := m.sniffer.w
	if w == nil {
		return false
	}
	head := m.snifferHead()
	return !m.panicking() && !m.snifferTempted() && !m.IsBaby() && !m.InWater() && m.OnGround && !m.isPassenger() &&
		m.snifferDiggableAt(w, Waypoint{head.X, head.Y - 1, head.Z})
}

// snifferDiggableAt is Sniffer.canDig(BlockPos): a diggable block not yet
// explored, with a path that reaches it.
func (m *Mob) snifferDiggableAt(w World, p Waypoint) bool {
	if !registry.BlockOf(w.BlockAt(p.X, p.Y, p.Z)).InTag(snifferDiggable) {
		return false
	}
	if slices.Contains(m.sniffer.explored, snifferSpot{w.DimensionName(), p}) {
		return false
	}
	return m.pathReaches(w, p, 1)
}

// snifferPickTarget is Sniffer.calculateDigPosition and the memories
// Sniffing.stop sets from it.
func (m *Mob) snifferPickTarget() {
	w := m.sniffer.w
	if w == nil {
		return
	}
	for i := range 5 {
		spot, ok := landRandomPos(w, m, 10+2*i, 3)
		if !ok {
			continue
		}
		below := Waypoint{spot.X, spot.Y - 1, spot.Z}
		if m.snifferDiggableAt(w, below) {
			m.StartSnifferSearch(below.X, below.Y, below.Z)
			return
		}
	}
}

// onPos is Entity.getOnPos: the block under the feet.
func (m *Mob) onPos() Waypoint {
	return Waypoint{int32(math.Floor(m.X)), int32(math.Floor(m.Y - float64(float32(0.2)))), int32(math.Floor(m.Z))}
}

// snifferStep is Sniffer.tick and customServerAiStep's memory upkeep.
func (m *Mob) snifferStep(t *tickView) {
	b := &m.sniffer
	m.sniffer.w = t.w
	if !m.Alive() {
		// vanilla: Sniffer.die
		m.SetMetadataSnifferState(SnifferIdling)
		return
	}
	if b.sniffCooldown > 0 {
		b.sniffCooldown--
	}
	if b.temptCooldown > 0 {
		b.temptCooldown--
	}
	if b.state == SnifferDigging && b.dropSeedAt == m.Age {
		head := m.snifferHead()
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSnifferDug, X: float64(head.X), Y: float64(head.Y), Z: float64(head.Z)})
	}
	if b.state == SnifferDigging && m.Age%10 == 0 {
		// vanilla: Sniffer.emitDiggingParticles posts at the head block, not the body
		head := m.snifferHead()
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobActed, X: float64(head.X) + 0.5, Y: float64(head.Y) + 0.5, Z: float64(head.Z) + 0.5})
	}
	t.events = append(t.events, m.sniffer.pending...)
	m.sniffer.pending = m.sniffer.pending[:0]
}

// RollSnifferDigging rolls the gift table a dig drops from, in level.
// vanilla: Sniffer.dropSeed
func (m *Mob) RollSnifferDigging(level rules.LootLevel) []protocol.ItemStack {
	ctx := rules.LootContext{This: m.LootEntity(), Level: level}
	return rules.RollLoot("minecraft:gameplay/sniffer_digging", &ctx, m.Rand)
}

// snifferDigging reports the digging pose, whose box is lower.
func (m *Mob) snifferDigging() bool {
	return m.Type == TypeSniffer && m.sniffer.state == SnifferDigging
}

// snifferFields saves or loads the brain memories sniff_cooldown and
// sniffer_explored_positions.
// vanilla: LivingEntity.addAdditionalSaveData, Brain.codec
func (m *Mob) snifferFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	b := &m.sniffer
	if !load {
		memories := m.newMemoryMap()
		if b.sniffCooldown > 0 {
			memories.Set("minecraft:sniff_cooldown", memoryTag(nbt.NewOrderedCompound(0), int64(b.sniffCooldown)))
		}
		if len(b.explored) > 0 {
			items := make([]nbt.Tag, len(b.explored))
			for i, e := range b.explored {
				items[i] = globalPosTag(GlobalPos{e.dim, e.pos.X, e.pos.Y, e.pos.Z})
			}
			memories.Set("minecraft:sniffer_explored_positions", memoryTag(nbt.NewList(items)))
		}
		out.Set("Brain", brainTag(memories))
		return
	}
	b.sniffCooldown, b.explored = 0, nil
	brain, _ := nbt.Get[nbt.Compound](tag, "Brain")
	memories, _ := nbt.Get[nbt.Compound](brain, "memories")
	if cooldown, ok := nbt.Get[nbt.Compound](memories, "minecraft:sniff_cooldown"); ok {
		if _, unit := nbt.CompoundView(cooldown["value"]); unit {
			ttl, _ := nbt.LongValue(cooldown["ttl"])
			b.sniffCooldown = int(max(ttl, 0))
		}
	}
	explored, _ := nbt.Get[nbt.Compound](memories, "minecraft:sniffer_explored_positions")
	list, _ := nbt.Get[nbt.List](explored, "value")
	for _, item := range list.Items {
		entry, _ := nbt.CompoundView(item)
		dim, _ := nbt.Get[nbt.String](entry, "dimension")
		pos, _ := nbt.Get[nbt.IntArray](entry, "pos")
		if len(pos) == 3 {
			b.explored = append(b.explored, snifferSpot{string(dim), Waypoint{pos[0], pos[1], pos[2]}})
		}
	}
}
