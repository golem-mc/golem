package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TypeWanderingTrader is the wandering trader, an AbstractVillager with no
// villager data, levels or restocks.
const TypeWanderingTrader = registry.EntityTypeWanderingTrader

// WanderingTraderTradeSets are the trade sets a wandering trader draws, in
// order. vanilla: WanderingTrader.updateTrades
var WanderingTraderTradeSets = [...]string{
	"wandering_trader/buying", "wandering_trader/uncommon", "wandering_trader/common",
}

// The wandering trader's speed modifiers over its 0.7 movement speed.
// vanilla: WanderingTrader.registerGoals
const (
	traderWalkPace  = 0.35
	traderPanicPace = 0.5
)

// traderZombies are the Zombie subclasses its AvoidEntityGoal<Zombie> flees.
var traderZombies = []MobType{TypeZombie, TypeHusk, TypeDrowned, TypeZombieVillager, TypeZombifiedPiglin}

// traderGoals wires float, the potion at night and the milk at dawn, the
// flights from zombies and illagers, panic, the walk to its wander target and
// home, stroll and the glances.
// vanilla: WanderingTrader.registerGoals
func traderGoals(m *Mob) []Goal {
	avoid := func(dist float64, mobs ...MobType) Goal {
		return &avoidGoal{m: m, priority: 1, active: func() bool { return true }, maxDist: dist,
			walk: traderPanicPace, sprint: traderPanicPace, mobs: mobs}
	}
	return []Goal{
		newFloatGoal(m),
		&traderDrinkGoal{m: m, item: traderPotion, done: registry.SoundEntityWanderingTraderDisappeared},
		&traderDrinkGoal{m: m, item: traderMilk, done: registry.SoundEntityWanderingTraderReappeared},
		&tradingPlayerGoal{m: m, trader: true},
		avoid(8, traderZombies...),
		avoid(12, TypeEvoker),
		avoid(12, TypeIllusioner),
		avoid(8, TypeVindicator),
		avoid(8, TypeVex),
		avoid(15, TypePillager),
		avoid(10, TypeZoglin),
		&panicGoal{m: m, pace: traderPanicPace},
		&traderWanderGoal{m: m},
		&homeGoal{m: m, priority: 4, pace: traderWalkPace},
		&randomStroll{m: m, priority: 8, pace: traderWalkPace},
		// vanilla: InteractGoal(Player, 3, 1.0), LookAtPlayerGoal(Mob, 8)
		&lookAtPlayer{m: m, priority: 9, reach: 3, chance: 1},
		&lookAtPlayer{m: m, priority: 10},
	}
}

// traderDespawnStep counts DespawnDelay down while no one trades and reports
// when it runs out; a delay of zero never runs out.
// vanilla: WanderingTrader.maybeDespawn
func (m *Mob) traderDespawnStep() bool {
	if m.Type == TypeTraderLlama {
		return m.llamaDespawnStep()
	}
	if m.Type != TypeWanderingTrader || m.DespawnDelay <= 0 || m.Trading {
		return false
	}
	m.DespawnDelay--
	return m.DespawnDelay == 0
}

// llamaDespawnStep counts a trader llama's delay down, or follows its
// trader's, and reports when it runs out. A lead held by anything but the
// trader, a tame body, a rider, an age lock or persistence stops the count.
// The lead goes without an item.
// vanilla: TraderLlama.maybeDespawn
func (m *Mob) llamaDespawnStep() bool {
	h := &m.Horse
	if h.Tame || m.LeashHolderID != 0 && !h.traderLead || len(m.Passengers) > 0 || h.Rider != uuid.Nil ||
		m.AgeLocked || m.Persistence {
		return false
	}
	m.DespawnDelay--
	if h.traderLead {
		m.DespawnDelay = h.traderDelay - 1
	}
	if m.DespawnDelay > 0 {
		return false
	}
	m.RemoveLeash()
	return true
}

// The wandering trader's drinks: an invisibility potion and a milk bucket.
var (
	traderPotion = traderPotionStack()
	traderMilk   = protocol.ItemStack{ItemID: int32(registry.ItemMilkBucket), Count: 1}
)

const mobEffectInvisibility = registry.MobEffectInvisibility

func traderPotionStack() protocol.ItemStack {
	stack := protocol.ItemStack{ItemID: int32(registry.ItemPotion), Count: 1}
	stack.SetPotion(registry.MustPotion("minecraft:invisibility"))
	return stack
}

// traderDrinkGoal is UseItemGoal: the potion once it is dark outside and the
// trader is visible, the milk once it is bright and it is not. It holds the
// item until traderUseStep finishes it, then empties the hand and plays its
// sound. It takes no controls, so it runs beside the others.
// vanilla: UseItemGoal
type traderDrinkGoal struct {
	m    *Mob
	item protocol.ItemStack
	done registry.Sound
}

func (g *traderDrinkGoal) Priority() int           { return 0 }
func (g *traderDrinkGoal) Flags() GoalFlags        { return 0 }
func (g *traderDrinkGoal) RequiresEveryTick() bool { return false }
func (g *traderDrinkGoal) Tick(*tickView)          {}

func (g *traderDrinkGoal) CanStart(t *tickView) bool {
	invisible := g.m.hasEffect(mobEffectInvisibility)
	if g.item.ItemID == int32(registry.ItemPotion) {
		return t.w.DarkOutside() && !invisible
	}
	return t.w.BrightOutside() && invisible
}

func (g *traderDrinkGoal) ShouldContinue(*tickView) bool { return g.m.UsingItem }

// Start is LivingEntity.startUsingItem on the main hand.
func (g *traderDrinkGoal) Start(*tickView) {
	m := g.m
	m.SetEquipment(protocol.EquipMainHand, g.item)
	if m.UsingItem {
		return
	}
	cons, _ := registry.ConsumableOf(g.item.ItemID)
	m.SetMetadataUsingItem(true)
	m.useLeft = rules.ConsumeTicks(cons.Seconds)
}

func (g *traderDrinkGoal) Stop(t *tickView) {
	m := g.m
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: g.done, Volume: 1,
		Pitch: m.Rand.Float32()*0.2 + 0.9})
}

// traderUseStep counts the held drink down: the drinking sound every fourth
// tick past the first fifth, then the drink's effects and its remainder in
// hand. A hand that no longer holds the drink stops the use.
// vanilla: LivingEntity.updatingUsingItem, ItemStack.onUseTick,
// LivingEntity.completeUsingItem
func (m *Mob) traderUseStep(events []MobEvent) []MobEvent {
	if !m.UsingItem {
		return events
	}
	item := m.MainHand.ItemID
	if item != int32(registry.ItemPotion) && item != traderMilk.ItemID {
		m.SetMetadataUsingItem(false)
		return events
	}
	cons, _ := registry.ConsumableOf(item)
	if rules.EatBiteTicks(rules.ConsumeTicks(cons.Seconds), m.useLeft) {
		events = m.traderDrinkSound(events, item)
	}
	if m.useLeft--; m.useLeft != 0 {
		return events
	}
	events = m.traderDrinkSound(events, item)
	stack := m.MainHand
	events = m.drinkPotion(stack, events)
	for _, change := range rules.ConsumeOn(m, cons.Effects, m.Rand).Changes {
		events = append(events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: change})
	}
	m.SetEquipment(protocol.EquipMainHand, cons.Remainder)
	m.SetMetadataUsingItem(false)
	return events
}

// traderDrinkSound is the trader's own consume sound at a drink's volume and
// pitch. vanilla: WanderingTrader.getConsumeSound,
// Consumable.emitParticlesAndSounds
func (m *Mob) traderDrinkSound(events []MobEvent, item int32) []MobEvent {
	sound := registry.SoundEntityWanderingTraderDrinkPotion
	if item == traderMilk.ItemID {
		sound = registry.SoundEntityWanderingTraderDrinkMilk
	}
	return append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: 0.5,
		Pitch: 0.9 + 0.1*m.Rand.Float32()})
}

// The wander target's reaches: the goal starts beyond traderWanderStop
// blocks of the target's centre, and a target beyond traderWanderLeg is
// walked toward in legs of that length.
// vanilla: WanderingTrader.registerGoals, WanderToPositionGoal.tick
const (
	traderWanderPriority = 2
	traderWanderStop     = 2.0
	traderWanderLeg      = 10.0
)

// traderWanderGoal walks the trader to its wander target and forgets the
// target when it stops.
// vanilla: WanderingTrader.WanderToPositionGoal
type traderWanderGoal struct{ m *Mob }

func (g *traderWanderGoal) Priority() int           { return traderWanderPriority }
func (g *traderWanderGoal) Flags() GoalFlags        { return GoalMove }
func (g *traderWanderGoal) RequiresEveryTick() bool { return false }
func (g *traderWanderGoal) Start(*tickView)         {}

func (g *traderWanderGoal) CanStart(*tickView) bool {
	return g.m.WanderTarget != nil && g.farFrom(*g.m.WanderTarget, traderWanderStop)
}

func (g *traderWanderGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *traderWanderGoal) Tick(t *tickView) {
	m := g.m
	if m.WanderTarget == nil || !m.navDone() {
		return
	}
	p := *m.WanderTarget
	if g.farFrom(p, traderWanderLeg) {
		// The leg points at the block's corner, not its centre.
		dx, dy, dz := float64(p.X)-m.X, float64(p.Y)-m.Y, float64(p.Z)-m.Z
		if n := math.Sqrt(dx*dx + dy*dy + dz*dz); n >= 1e-5 {
			dx, dy, dz = dx/n, dy/n, dz/n
		} else {
			dx, dy, dz = 0, 0, 0
		}
		m.seekTo(t.w, m.X+dx*traderWanderLeg, m.Y+dy*traderWanderLeg, m.Z+dz*traderWanderLeg, traderWalkPace, navReach)
		return
	}
	m.seekTo(t.w, float64(p.X), float64(p.Y), float64(p.Z), traderWalkPace, navReach)
}

func (g *traderWanderGoal) Stop(*tickView) {
	g.m.WanderTarget = nil
	g.m.stopSeeking()
}

// farFrom is !BlockPos.closerToCenterThan: the trader is at least dist from
// the block's centre.
func (g *traderWanderGoal) farFrom(p Waypoint, dist float64) bool {
	m := g.m
	dx, dy, dz := float64(p.X)+0.5-m.X, float64(p.Y)+0.5-m.Y, float64(p.Z)+0.5-m.Z
	return dx*dx+dy*dy+dz*dz >= dist*dist
}
