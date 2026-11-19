package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypeArmadillo is minecraft:armadillo.
const TypeArmadillo = registry.EntityTypeArmadillo

func init() {
	// vanilla: EntityTypes.ARMADILLO, Armadillo.createAttributes
	kinds[TypeArmadillo] = &kind{
		fireAverse: true,
		name:       "Armadillo", width: float64(float32(0.7)), height: float64(float32(0.65)), eyeHeight: float64(float32(0.26)),
		babyWidth: float64(float32(0.7) * float32(0.6)), babyHeight: float64(float32(0.65) * float32(0.6)), babyEye: 0.21875,
		babyable: true, maxHealth: 12, speed: 0.14,
		temptTag: &registry.ItemTagIDArmadilloFood, temptPace: 1.25, followPace: 1.25, strollPace: 1,
		ambientInterval: 120,
		sounds:          MobSounds{Ambient: registry.SoundEntityArmadilloAmbient, Hurt: registry.SoundEntityArmadilloHurt, Death: registry.SoundEntityArmadilloDeath, Eat: registry.SoundEntityArmadilloEat, Step: registry.SoundEntityArmadilloStep},
	}
	mobTracking[TypeArmadillo] = tracking{rangeChunks: 10, updateInterval: 3}
}

// ArmadilloState is the saved and synched Armadillo.ArmadilloState ordinal.
func (m *Mob) ArmadilloState() int { return m.armadilloState }

func armadilloGoals(m *Mob) []Goal {
	// vanilla: ArmadilloAi.initCoreActivity, initIdleActivity
	look := &lookAtPlayer{m: m, reach: 6}
	idle := &runOneGoal{m: m, priority: priorityStroll, flags: GoalMove, weights: []int{1, 1, 1}, choices: []Goal{
		&randomStroll{m: m, interval: 1, pace: 1, ignoreIdle: true, emptyPick: true,
			pick: func(t *tickView) (Waypoint, bool) { return landRandomPos(t.w, m, strollSpreadXZ, strollSpreadY) }},
		&lookWalkChoice{m: m, pace: 1, look: look.player},
		&doNothingGoal{m: m, min: 30, max: 60},
	}}
	return []Goal{&armadilloBallGoal{m: m}, newFloatGoal(m), &panicGoal{m: m, pace: 2, brain: true, causes: panicEnvironmentalCauses},
		&breedGoal{m: m}, &temptGoal{m: m},
		&followParentGoal{m: m, ranged: &followRange{min: 5, max: 16, scan: 16}},
		idle, look, &randomLookAround{m: m}}
}

func (m *Mob) armadilloStep(t *tickView) {
	// vanilla: Armadillo.customServerAiStep, shouldDropLoot; a baby or a
	// disabled mob_drops rule leaves the expired timer waiting rather than
	// rerolling it.
	if !m.Alive() {
		return
	}
	m.armadilloScareStep(t)
	m.ScuteTimer--
	if m.ScuteTimer <= 0 && !m.IsBaby() && t.w.BoolRule(world.RuleMobDrops) {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobShedScute})
		m.ScuteTimer = 6000 + m.Rand.Intn(6000)
	}
}

// RollArmadilloShed rolls the shedding gift table.
func (m *Mob) RollArmadilloShed(level rules.LootLevel) []protocol.ItemStack {
	ctx := rules.LootContext{This: m.LootEntity(), Level: level}
	return rules.RollLoot("minecraft:gameplay/armadillo_shed", &ctx, m.Rand)
}

// RollArmadilloBrush rolls the brush interaction table: the armadillo is the
// target (named name, nil for none) and interacting the brusher, empty for a
// dispenser. It carries no this_entity.
// vanilla: LootContextParamSets.ENTITY_INTERACT, LivingEntity.dropFromEntityInteractLootTable
func (m *Mob) RollArmadilloBrush(tool protocol.ItemStack, name *protocol.Text, interacting rules.LootEntity, level rules.LootLevel) []protocol.ItemStack {
	target := m.LootEntity()
	target.Name = name
	ctx := rules.LootContext{Target: target, Interacting: interacting, Tool: rules.ToolOf(tool), Level: level}
	return rules.RollLoot("minecraft:brush/armadillo", &ctx, m.Rand)
}

func (m *Mob) armadilloFields(c *mobCodec) {
	states := [...]string{"idle", "rolling", "scared", "unrolling"}
	if c.load {
		m.SetMetadataArmadilloState(0)
		name, _ := nbt.Get[nbt.String](c.tag, "state")
		for i, state := range states {
			if string(name) == state {
				m.SetMetadataArmadilloState(i)
			}
		}
	} else {
		c.set("state", nbt.String(states[m.armadilloState]))
	}
	mobInteger(c, "scute_time", &m.ScuteTimer, 0)
}

// vanilla: Armadillo.hurtServer, actuallyHurt
func (m *Mob) armadilloHurt(amount float64, damageType registry.DamageType, livingCause bool) {
	if amount <= 0 || m.NoAI {
		return
	}
	if livingCause {
		m.armadilloDanger = 80
		if !m.ArmadilloScared() && m.armadilloCanRoll() {
			m.armadilloSwitch(1)
			m.VX, m.VY, m.VZ = 0, 0, 0
			m.armadilloPendingRoll = true
			m.InLove = 0
			m.stopSeeking()
		}
	} else if damageType.InTag(panicEnvironmentalCauses) {
		m.armadilloDanger = 0
	}
}

// ArmadilloScared reports whether the animal is in any threatened state.
func (m *Mob) ArmadilloScared() bool { return m.Type == TypeArmadillo && m.armadilloState != 0 }

func (m *Mob) armadilloCanRoll() bool {
	if src := m.lastDamageSource(); src != 0 && src.InTag(panicEnvironmentalCauses) {
		return false
	}
	return !m.panicking() && !m.InWater() && !m.InLava() && m.LeashHolderID == 0 && !m.RidesVehicle && len(m.Passengers) == 0
}

func (m *Mob) armadilloSound(t *tickView, sound registry.Sound) {
	volume, pitch := float32(1), float32(1)
	if sound == registry.SoundEntityArmadilloRoll || sound == registry.SoundEntityArmadilloUnrollFinish {
		volume, pitch = m.SoundVolume(), m.VoicePitch()
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobEntityAction})
	}
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: volume, Pitch: pitch})
}

func (m *Mob) armadilloSwitch(state int) {
	if m.armadilloState != state {
		m.SetMetadataArmadilloState(state)
		m.armadilloTicks = 0
	}
}

// vanilla: Armadillo.isScaredBy, ArmadilloAi.ArmadilloBallUp
func (m *Mob) armadilloScareStep(t *tickView) {
	m.armadilloTicks++
	if m.armadilloDanger > 0 {
		m.armadilloDanger--
	}
	near := func(x, y, z, w, h float64) bool {
		reach := 7 + (m.width()+w)/2
		return math.Abs(x-m.X) < reach && math.Abs(z-m.Z) < reach && y+h > m.Y-2 && y < m.Y+m.height()+2
	}
	// vanilla: SensorType.ARMADILLO_SCARE_DETECTED scans every five ticks.
	m.armadilloScan--
	if m.armadilloScan <= 0 {
		m.armadilloScan = 5
		for _, o := range t.others {
			if o != m && o.Alive() && (o.ID == m.LastAttackerID() || registry.EntityType(o.typeID()).InTag(registry.EntityTypeTagIDUndead)) && near(o.X, o.Y, o.Z, o.width(), o.height()) {
				m.armadilloDanger = 80
				break
			}
		}
		for _, p := range t.players {
			pw, ph, _ := p.box()
			if !p.Dead && (p.EntityID == m.LastAttackerID() || !p.Spectator && (p.Sprinting || p.Passenger)) && near(p.X, p.Y, p.Z, pw, ph) {
				m.armadilloDanger = 80
				break
			}
		}
	}
	if m.armadilloPendingRoll {
		m.armadilloSound(t, registry.SoundEntityArmadilloRoll)
		m.armadilloPendingRoll = false
	}
	if !m.armadilloCanRoll() {
		m.armadilloDanger = 0
	}
	if m.armadilloDanger == 0 {
		if m.ArmadilloScared() {
			m.armadilloSwitch(0)
			m.armadilloSound(t, registry.SoundEntityArmadilloUnrollFinish)
		}
		return
	}
	if m.armadilloState == 0 && m.OnGround {
		m.armadilloSwitch(1)
		m.VX, m.VY, m.VZ = 0, 0, 0
		m.InLove = 0
		m.stopSeeking()
		m.walk.speed = 0
		m.armadilloSound(t, registry.SoundEntityArmadilloRoll)
	}
	if m.armadilloState == 1 && m.armadilloTicks > 10 {
		m.armadilloSwitch(2)
		if m.OnGround {
			m.armadilloSound(t, registry.SoundEntityArmadilloLand)
		}
	}
	danger := m.armadilloDanger > 75
	if danger != m.armadilloDangerAround {
		m.armadilloPeek = 150 + m.Rand.Intn(301)
	}
	m.armadilloDangerAround = danger
	if m.armadilloPeek > 0 {
		m.armadilloPeek--
	}
	if m.armadilloState == 2 {
		if m.armadilloPeek == 0 && m.OnGround && danger {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: 64})
			m.armadilloPeek = 150 + m.Rand.Intn(301)
		}
		if m.armadilloDanger < 30 {
			m.armadilloSwitch(3)
			m.armadilloSound(t, registry.SoundEntityArmadilloUnrollStart)
		}
	} else if m.armadilloState == 3 && m.armadilloDanger > 30 {
		m.armadilloSwitch(2)
	}
}

type armadilloBallGoal struct{ m *Mob }

func (g *armadilloBallGoal) Priority() int                   { return -1 }
func (g *armadilloBallGoal) Flags() GoalFlags                { return GoalMove | GoalLook | GoalJump }
func (g *armadilloBallGoal) CanStart(*tickView) bool         { return g.m.ArmadilloScared() }
func (g *armadilloBallGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *armadilloBallGoal) Start(*tickView)                 { g.m.stopSeeking() }
func (g *armadilloBallGoal) Tick(*tickView)                  { g.m.stopSeeking(); g.m.HeadYaw = g.m.Yaw; g.m.Pitch = 0 }
func (g *armadilloBallGoal) Stop(*tickView)                  {}
func (g *armadilloBallGoal) RequiresEveryTick() bool         { return true }
