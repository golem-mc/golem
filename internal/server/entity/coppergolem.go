package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TypeCopperGolem is minecraft:copper_golem.
const TypeCopperGolem = registry.EntityTypeCopperGolem

// Copper golem weathering stages, in WeatheringCopper.WeatherState order.
const (
	CopperUnaffected = iota
	CopperExposed
	CopperWeathered
	CopperOxidized
)

var copperStageNames = [...]string{"unaffected", "exposed", "weathered", "oxidized"}

// copperNextUnset and copperWaxed are nextWeatheringTick's sentinels.
// vanilla: CopperGolem.UNSET_WEATHERING_TICK, IGNORE_WEATHERING_TICK
const (
	copperNextUnset = -1
	copperWaxed     = -2
)

// CopperScrape is what an axe did to a copper golem.
type CopperScrape int

// The results of CopperGolemScrape.
const (
	CopperScrapeNone  CopperScrape = iota
	CopperScrapeWax                // removed the wax (level event 3004)
	CopperScrapeStage              // removed one stage of oxidation (3005)
)

// copperGolemState is the copper golem's own fields.
type copperGolemState struct {
	weather        int
	nextWeathering int64
	lastBolt       uuid.UUID
	// state is COPPER_GOLEM_STATE; cooldown is TRANSPORT_ITEMS_COOLDOWN_TICKS.
	state, cooldown int
	// opened is whether the golem counts as an opener of the chest at
	// openedAt, vanilla's openedChestPos.
	opened   bool
	openedAt Waypoint
}

func init() {
	// vanilla: EntityTypes.COPPER_GOLEM, CopperGolem.createAttributes. Eye
	// height 0.8125; getHurtSound and the like follow the weathering stage
	// (copperSounds).
	kinds[TypeCopperGolem] = &kind{
		name: "Copper Golem", width: float64(float32(0.49)), height: float64(float32(0.98)), eyeHeight: float64(float32(0.8125)),
		maxHealth: 12, speed: float64(float32(0.2)), stepHeight: 1, pathLength: 48, fireAverse: true,
		sounds: MobSounds{Hurt: registry.SoundEntityCopperGolemHurt, Death: registry.SoundEntityCopperGolemDeath,
			Step: registry.SoundEntityCopperGolemStep, StepVolume: 1},
	}
	mobTracking[TypeCopperGolem] = tracking{rangeChunks: 10, updateInterval: 3}
}

func copperGolemGoals(m *Mob) []Goal {
	// vanilla: CopperGolemAi.initCoreActivity, initIdleActivity. The idle
	// RunOne runs only while the transport rests in its cooldown.
	// Neither choice turns the head, so the glances run alongside.
	idle := &runOneGoal{m: m, priority: priorityStroll, flags: GoalMove, choices: []Goal{
		// vanilla: RandomStroll.stroll(1.0f, 2, 2)
		&randomStroll{m: m, interval: 1, pace: 1, ignoreIdle: true, pick: func(t *tickView) (Waypoint, bool) { return landRandomPos(t.w, m, 2, 2) }},
		&doNothingGoal{m: m, min: 30, max: 60},
	}}
	return []Goal{newFloatGoal(m),
		// vanilla: CopperGolemAi.initCoreActivity AnimalPanic(1.5f)
		&panicGoal{m: m, pace: 1.5, brain: true}, &copperTransport{m: m}, newInteractWithDoor(m),
		&gatedGoal{Goal: idle, enabled: func() bool { return m.copper.cooldown > 0 }},
		&lookAtPlayer{m: m, reach: 6}, &randomLookAround{m: m}}
}

// CopperWeather is the synched weathering stage, 0 unaffected to 3 oxidized.
func (m *Mob) CopperWeather() int { return m.copper.weather }

// SetCopperWeather sets the stage; an out of range value is clamped.
func (m *Mob) SetCopperWeather(stage int) {
	m.SetMetadataCopperWeather(max(0, min(stage, CopperOxidized)))
}

// CopperWax waxes the golem, false when it already is.
// vanilla: CopperGolem.mobInteract's honeycomb
func (m *Mob) CopperWax() bool {
	if m.copper.nextWeathering == copperWaxed {
		return false
	}
	m.copper.nextWeathering = copperWaxed
	return true
}

// CopperScrapeWith applies an axe: the wax first, else one stage of
// oxidation, each restarting the weathering timer.
// vanilla: CopperGolem.mobInteract
func (m *Mob) CopperScrapeWith() CopperScrape {
	switch {
	case m.copper.nextWeathering == copperWaxed:
		m.copper.nextWeathering = copperNextUnset
		return CopperScrapeWax
	case m.copper.weather != CopperUnaffected:
		m.copper.nextWeathering = copperNextUnset
		m.SetMetadataCopperWeather(m.copper.weather - 1)
		return CopperScrapeStage
	}
	return CopperScrapeNone
}

// CopperStruck scrapes one stage once per bolt.
// vanilla: CopperGolem.thunderHit
func (m *Mob) CopperStruck(bolt uuid.UUID) {
	if bolt == m.copper.lastBolt {
		return
	}
	m.copper.lastBolt = bolt
	if m.copper.weather != CopperUnaffected {
		m.copper.nextWeathering = copperNextUnset
		m.SetMetadataCopperWeather(m.copper.weather - 1)
	}
}

// CopperShearable reports a shearable item on the antenna slot.
// vanilla: CopperGolem.readyForShearing
func (m *Mob) CopperShearable() bool {
	return !m.Antenna.Empty() && registry.Item(m.Antenna.ItemID).InTag(registry.ItemTagIDShearableFromCopperGolem)
}

// CopperShear empties the antenna slot and returns what was on it.
// vanilla: CopperGolem.shear
func (m *Mob) CopperShear() protocol.ItemStack {
	stack := m.Antenna
	m.SetMetadataAntenna(protocol.ItemStack{})
	return stack
}

func (m *Mob) copperSounds(s MobSounds) MobSounds {
	// vanilla: CopperGolemOxidationLevels
	switch m.copper.weather {
	case CopperWeathered:
		s.Hurt, s.Death, s.Step = registry.SoundEntityCopperGolemWeatheredHurt, registry.SoundEntityCopperGolemWeatheredDeath, registry.SoundEntityCopperGolemWeatheredStep
	case CopperOxidized:
		s.Hurt, s.Death, s.Step = registry.SoundEntityCopperGolemOxidizedHurt, registry.SoundEntityCopperGolemOxidizedDeath, registry.SoundEntityCopperGolemOxidizedStep
	default:
		s.Hurt, s.Death, s.Step = registry.SoundEntityCopperGolemHurt, registry.SoundEntityCopperGolemDeath, registry.SoundEntityCopperGolemStep
	}
	return s
}

// ownGolemStep runs the snow and copper golems' own per-tick work.
func (m *Mob) ownGolemStep(w World, events []MobEvent) []MobEvent {
	switch m.Type {
	case TypeSnowGolem:
		return m.snowGolemStep(w, events)
	case TypeCopperGolem:
		return m.copperGolemStep(w, events)
	}
	return events
}

// copperGolemStep ages the golem a stage when its timer is due and, once
// oxidized, rolls to become a statue.
// vanilla: CopperGolem.updateWeathering
func (m *Mob) copperGolemStep(w World, events []MobEvent) []MobEvent {
	c := &m.copper
	if c.cooldown > 0 && !m.NoAI {
		c.cooldown--
	}
	if c.opened && (m.Dead || m.gone) {
		c.opened = false
		events = append(events, MobEvent{Mob: m, Kind: MobChestClosed, X: float64(c.openedAt.X), Y: float64(c.openedAt.Y), Z: float64(c.openedAt.Z)})
	}
	if m.Dead || c.nextWeathering == copperWaxed {
		return events
	}
	if c.nextWeathering == copperNextUnset {
		c.nextWeathering = m.GameTime + copperWeatherDelay(m)
		return events
	}
	oxidized := c.weather == CopperOxidized
	if m.GameTime >= c.nextWeathering && !oxidized {
		m.SetMetadataCopperWeather(c.weather + 1)
		if c.weather == CopperOxidized {
			c.nextWeathering = 0
		} else {
			c.nextWeathering += copperWeatherDelay(m)
		}
	}
	if !oxidized {
		return events
	}
	x, y, z := int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))
	// vanilla: CopperGolem.canTurnToStatue, turnToStatue
	if !w.IsAirAt(x, y, z) || m.Rand.Float32() > 0.0058 {
		return events
	}
	pose := copperStatuePoses[m.Rand.Intn(len(copperStatuePoses))]
	facing := [...]string{"south", "west", "north", "east"}[int(math.Floor(float64(m.Yaw)/90+0.5))&3]
	state, _ := registry.WithProps(registry.BlockOxidizedCopperGolemStatue.DefaultState(),
		registry.PropValue{P: registry.PropFacing, V: facing}, registry.PropValue{P: registry.PropCopperGolemPose, V: pose})
	// vanilla: CopperGolem.turnToStatue, Mob.dropPreservedEquipment
	for _, stack := range m.TakePreservedEquipment() {
		events = append(events, MobEvent{Mob: m, Kind: MobDroppedItem, Stack: stack, X: m.X, Y: m.Y, Z: m.Z})
	}
	// vanilla: CopperGolem.turnToStatue, Leashable.dropLeash
	if m.LeashHolderID != 0 {
		ev := m.leashDropEvent(m.RemoveLeash())
		ev.Untracked = true
		events = append(events, ev)
	}
	m.gone = true
	return append(events, MobEvent{Mob: m, Kind: MobTurnedToStatue, X: float64(x), Y: float64(y), Z: float64(z), BlockState: state})
}

var copperStatuePoses = [...]string{"standing", "sitting", "running", "star"}

// copperWeatherDelay is nextIntBetweenInclusive(504000, 552000).
func copperWeatherDelay(m *Mob) int64 { return int64(504000 + m.Rand.Intn(552000-504000+1)) }

func (m *Mob) copperGolemFields(c *mobCodec) {
	// vanilla: CopperGolem.addAdditionalSaveData, readAdditionalSaveData
	if c.load {
		m.SetMetadataCopperWeather(CopperUnaffected)
		name, _ := nbt.Get[nbt.String](c.tag, "weather_state")
		for i, stage := range copperStageNames {
			if string(name) == stage {
				m.SetMetadataCopperWeather(i)
			}
		}
		m.copper.nextWeathering = copperNextUnset
		if age, ok := nbt.LongValue(c.tag["next_weather_age"]); ok {
			m.copper.nextWeathering = age
		}
		// vanilla: LivingEntity.readAdditionalSaveData makes a fresh brain from a
		// saved one, and TRANSPORT_ITEMS_COOLDOWN_TICKS has no codec
		if _, ok := c.tag["Brain"]; ok {
			m.copper.cooldown = 0
		}
		return
	}
	// vanilla: LivingEntity.addAdditionalSaveData always writes the brain
	c.set("Brain", brainTag(nil))
	c.set("next_weather_age", nbt.Long(m.copper.nextWeathering))
	c.set("weather_state", nbt.String(copperStageNames[m.copper.weather]))
}

// CopperAccepts reports an item the golem's click acts on: shears with a
// shearable antenna, honeycomb on an unwaxed golem, an axe on a waxed or
// weathered one.
// vanilla: CopperGolem.mobInteract
func (m *Mob) CopperAccepts(item registry.Item) bool {
	switch item {
	case registry.ItemShears:
		return m.CopperShearable()
	case registry.ItemHoneycomb:
		return m.copper.nextWeathering != copperWaxed
	}
	return item.InTag(registry.ItemTagIDAxes) && (m.copper.nextWeathering == copperWaxed || m.copper.weather != CopperUnaffected)
}

// CopperGolemThrowCarried empties the golem's hand and returns the throw of
// that stack toward the point; false when the hand is empty.
// vanilla: CopperGolem.mobInteract, BehaviorUtils.throwItem
func (m *Mob) CopperGolemThrowCarried(px, py, pz float64) (MobEvent, bool) {
	stack := m.MainHand
	if stack.Empty() {
		return MobEvent{}, false
	}
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
	return m.throwEvent(stack, px, py, pz, piglinThrowSpeed, piglinThrowSpeed, piglinThrowDrop), true
}

// SaddleSlot is the stack in the mob's wire saddle slot: the antenna for a
// copper golem, the horse saddle otherwise.
func (m *Mob) SaddleSlot() protocol.ItemStack {
	if m.Type == TypeCopperGolem {
		return m.Antenna
	}
	return m.Horse.Saddle
}
