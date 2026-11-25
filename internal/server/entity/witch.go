package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The witch: it throws splash potions at its target and drinks a potion of
// its own against fire, water, wounds and distance.
// vanilla: Witch

// witchState is what the witch keeps beside the shared mob state.
type witchState struct {
	// drinking is Witch.DATA_USING_ITEM; usingTime counts the drink down.
	drinking  bool
	usingTime int
	// healCooldown is NearestHealableRaiderTargetGoal.cooldown, in ticks: the
	// witch hunts players only while it is not above zero.
	healCooldown int
}

// The witch's numbers.
// vanilla: Witch.aiStep, Witch.performRangedAttack, RangedAttackGoal
const (
	// witchDrinkTicks is the potion's use duration: Consumables.DEFAULT_DRINK
	// consumes for 1.6 seconds.
	witchDrinkTicks = 32
	// witchDrinkSpeed is the drinking modifier on movement_speed (ADD_VALUE).
	witchDrinkSpeed = -0.25
	// witchSwiftnessRange2 is the squared distance a target must pass before
	// the witch drinks swiftness.
	witchSwiftnessRange2 = 121.0
	// witchSparkleChance is the per-tick chance of the particle burst.
	witchSparkleChance = 7.5e-4
	// witchAttackInterval and witchAttackRadius are RangedAttackGoal's
	// arguments as the witch registers it.
	witchAttackInterval = 60
	witchAttackRadius   = 10.0
	// witchSeeTicks is the sight the goal wants before it stands to throw.
	witchSeeTicks = 5

	// The throw: 0.45 speed within two blocks, 0.75 beyond, inaccuracy 8,
	// aimed 1.1 below the eyes with a lift of a fifth of the distance.
	witchNearThrow      = 2.0
	witchNearPower      = 0.45
	witchFarPower       = 0.75
	witchInaccuracy     = 8.0
	witchAimBelowEyes   = 1.1
	witchAimLiftPerDist = 0.2
	// The distances that pick slowness and weakness.
	witchSlowRange = 8.0
	witchWeakRange = 3.0
	// The target health that picks poison.
	witchPoisonHealth = 8.0
	// witchHealCooldown is NearestHealableRaiderTargetGoal.DEFAULT_COOLDOWN,
	// before reducedTickDelay; witchHealingHealth is the raider health at or
	// below which it throws healing rather than regeneration.
	witchHealCooldown  = 200
	witchHealingHealth = 4.0
)

const (
	mobEffectWaterBreathing = registry.MobEffectWaterBreathing
	mobEffectFireResistance = registry.MobEffectFireResistance
)

func (m *Mob) hasEffect(id int32) bool {
	_, ok := m.effects.Get(id)
	return ok
}

// DrinkingPotion is Witch.isDrinkingPotion.
func (m *Mob) DrinkingPotion() bool { return m.Type == TypeWitch && m.witch.drinking }

// vanilla: Witch.registerGoals
var witchRaiderKinds = slices.DeleteFunc(slices.Clone(raiderKinds), func(kind MobType) bool { return kind == TypeWitch })

// witchGoals is Witch.registerGoals with Raider's: FloatGoal
// 1, RangedAttackGoal 2, stroll 2, looks 3, then HurtByTargetGoal 1 sparing
// raiders, a raider to heal 2 and the nearest player 3.
// vanilla: Witch.registerGoals, NearestHealableRaiderTargetGoal,
// NearestAttackableWitchTargetGoal
func witchGoals(m *Mob) []Goal {
	return withRaidGoals(m, []Goal{
		newFloatGoal(m),
		&hurtByGoal{m: m, priority: 1, ignore: isRaiderType},
		&witchAttackGoal{m: m},
		&randomStroll{m: m, priority: 2},
		&mobTargetGoal{m: m, priority: 2, eager: true, kinds: witchRaiderKinds,
			gate:    func() bool { return m.witch.healCooldown <= 0 && m.Rand.Intn(2) == 0 && m.InRaid },
			started: func() { m.witch.healCooldown = reducedTickDelay(witchHealCooldown) },
		},
		&nearestTargetGoal{m: m, priority: 3, refuse: func() bool { return m.witch.healCooldown > 0 }},
		&lookAtPlayer{m: m, priority: 3},
		&randomLookAround{m: m, priority: 3},
	})
}

// witchEye is the y of a target's eyes.
func witchEye(p *Player) float64 {
	if p.Sneaking {
		return p.Y + 1.27
	}
	return p.Y + p.eyeHeight()
}

// witchAttackGoal is RangedAttackGoal(this, 1.0, 60, 10.0F): approach until
// within ten blocks with five ticks of sight, throwing every 60 ticks.
type witchAttackGoal struct {
	m          *Mob
	attackTime int
	seeTime    int
}

func (g *witchAttackGoal) Priority() int    { return 2 }
func (g *witchAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *witchAttackGoal) CanStart(t *tickView) bool {
	return g.m.TargetID != 0 && g.m.targetIn(t) != nil
}

func (g *witchAttackGoal) Start(*tickView) { g.attackTime = -1 }

func (g *witchAttackGoal) ShouldContinue(t *tickView) bool {
	return g.CanStart(t) || g.m.targetIn(t) != nil && len(g.m.navOf().nav.path) > 0
}

func (g *witchAttackGoal) Stop(*tickView) {
	g.seeTime = 0
	g.attackTime = -1
}

func (g *witchAttackGoal) RequiresEveryTick() bool { return true }

func (g *witchAttackGoal) Tick(t *tickView) {
	g.tick(t, witchAttackInterval, g.m.witchThrow)
}

// tick shares RangedAttackGoal's fixed-interval hunt with the drowned.
func (g *witchAttackGoal) tick(t *tickView, interval int, shoot func(*tickView, *Player)) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	dx, dy, dz := p.X-m.X, p.Y-m.Y, p.Z-m.Z
	distSq := dx*dx + dy*dy + dz*dz
	seen := m.seesTarget(t.w, p)
	if seen {
		g.seeTime++
	} else {
		g.seeTime = 0
	}
	if distSq > witchAttackRadius*witchAttackRadius || g.seeTime < witchSeeTicks {
		m.seekTo(t.w, p.X, p.Y, p.Z, 1.0, navReach)
	} else {
		m.stopSeeking()
	}
	// vanilla: LookControl.setLookAt(target, 30, 30)
	if dx != 0 || dz != 0 {
		m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz)), headTurnSpeed)
	}
	m.Pitch = turnToward(m.Pitch, float32(pitchToward(dx, witchEye(p)-(m.Y+m.eyeHeight()), dz)), headTurnSpeed)

	g.attackTime--
	if g.attackTime == 0 {
		if !seen {
			return
		}
		shoot(t, p)
		g.attackTime = interval
	} else if g.attackTime < 0 {
		g.attackTime = interval
	}
}

// witchPotionFor is Witch.performRangedAttack's choice: slowness for a far
// unslowed target, poison for a healthy unpoisoned one, weakness a quarter
// of the time for a near one, else harming.
func witchPotionFor(p *Player, dist float64, roll float32) string {
	switch {
	case dist >= witchSlowRange && !p.Slowed:
		return "minecraft:slowness"
	case p.Health >= witchPoisonHealth && !p.Poisoned:
		return "minecraft:poison"
	case dist <= witchWeakRange && !p.Weakened && roll < 0.25:
		return "minecraft:weakness"
	}
	return "minecraft:harming"
}

// witchThrow is Witch.performRangedAttack: from the eyes less 0.1, aimed
// ahead of the target by its horizontal velocity. A raider target gets
// healing or regeneration and is let go.
// vanilla: Witch.performRangedAttack, Entity.getDeltaMovement
func (m *Mob) witchThrow(t *tickView, p *Player) {
	if m.witch.drinking {
		return
	}
	xd := p.X + p.VX - m.X
	yd := witchEye(p) - float64(float32(witchAimBelowEyes)) - m.Y
	zd := p.Z + p.VZ - m.Z
	dist := math.Sqrt(xd*xd + zd*zd)
	var potion string
	if o := t.mobByID(p.EntityID); o != nil && isRaiderType(o.Type) {
		potion = "minecraft:regeneration"
		if o.Health <= witchHealingHealth {
			potion = "minecraft:healing"
		}
		m.SetMetadataTargetID(0)
	} else {
		potion = witchPotionFor(p, dist, float32(m.Rand.Float64()))
	}
	power := witchFarPower
	if dist <= witchNearThrow {
		power = witchNearPower
	}
	vx, vy, vz := ShootVelocity(xd, yd+dist*witchAimLiftPerDist, zd, float64(float32(power)), witchInaccuracy, m.Rand)
	t.events = append(t.events, MobEvent{
		Mob: m, Kind: MobThrewPotion, Potion: potion,
		X: m.X, Y: m.Y + m.eyeHeight() - ThrownSpawnBelow, Z: m.Z,
		VX: vx, VY: vy, VZ: vz,
		Pitch: float32(0.8 + m.Rand.Float64()*0.4),
	})
}

// witchStep is Witch.aiStep: the drink counting down or a new one picked,
// and the rare particle burst.
func (m *Mob) witchStep(w World, players []Player, others []*Mob, events []MobEvent) []MobEvent {
	if m.Dead {
		return events
	}
	m.witch.healCooldown--
	if m.witch.drinking {
		// vanilla: usingTime-- <= 0
		due := m.witch.usingTime <= 0
		m.witch.usingTime--
		if due {
			events = m.witchFinishDrink(events)
		}
	} else if potion := m.witchPickPotion(&tickView{w: w, players: players, others: others}); potion != "" {
		var stack protocol.ItemStack
		stack.ItemID, stack.Count = int32(registry.ItemPotion), 1
		stack.SetPotion(registry.MustPotion(potion))
		m.SetEquipment(protocol.EquipMainHand, stack)
		m.witch.usingTime = witchDrinkTicks
		m.SetMetadataWitchDrinking(true)
		events = append(events, MobEvent{Mob: m, Kind: MobWitchDrank, Pitch: float32(0.8 + m.Rand.Float64()*0.4)})
	}
	if float64(float32(m.Rand.Float64())) < witchSparkleChance {
		events = append(events, MobEvent{Mob: m, Kind: MobWitchSparkled})
	}
	return events
}

// witchPickPotion rolls the four drinks in Witch.aiStep's order.
func (m *Mob) witchPickPotion(t *tickView) string {
	roll := func() float32 { return float32(m.Rand.Float64()) }
	fireDamage := m.lastDamageSource() != 0 && m.lastDamageSource().InTag(registry.DamageTypeTagIDIsFire)
	switch {
	case roll() < 0.15 && m.waterFluid.eyesIn && !m.hasEffect(mobEffectWaterBreathing):
		return "minecraft:water_breathing"
	case roll() < 0.15 && (m.OnFire() || fireDamage) && !m.hasEffect(mobEffectFireResistance):
		return "minecraft:fire_resistance"
	case roll() < 0.05 && m.Health < m.healthCap():
		return "minecraft:healing"
	case roll() < 0.5:
		if p := m.targetIn(t); m.TargetID != 0 && p != nil && !m.hasEffect(mobEffectSpeed) &&
			Dist2(p.X, p.Y, p.Z, m.X, m.Y, m.Z) > witchSwiftnessRange2 {
			return "minecraft:swiftness"
		}
	}
	return ""
}

// witchFinishDrink empties the hand and gives the witch the potion's
// effects.
func (m *Mob) witchFinishDrink(events []MobEvent) []MobEvent {
	m.SetMetadataWitchDrinking(false)
	stack := m.MainHand
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
	if stack.ItemID == int32(registry.ItemPotion) {
		events = m.drinkPotion(stack, events)
	}
	return append(events, MobEvent{Mob: m, Kind: MobWitchDrankUp})
}

// drinkPotion gives the mob the effects of the potion stack it drank.
func (m *Mob) drinkPotion(stack protocol.ItemStack, events []MobEvent) []MobEvent {
	contents, ok := stack.PotionContentsOf()
	if !ok {
		return events
	}
	for _, in := range rules.PotionInstances(contents) {
		if change, ok := m.AddEffect(rules.ScaledDuration(in, stack.PotionDurationScaleOf())); ok {
			events = append(events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: change})
		}
	}
	return events
}

// witchMagicFactor is the share of the damage types in
// minecraft:witch_resistant_to a witch takes.
// vanilla: Witch.getDamageAfterMagicAbsorb
const witchMagicFactor = float64(float32(0.15))

// OwnsHarming reports whether in is this witch's own harming splash, which
// does nothing to it (it is the damage source).
// vanilla: Witch.getDamageAfterMagicAbsorb
func (m *Mob) OwnsHarming(in effect.Instance, throwerID int32) bool {
	return m.Type == TypeWitch && in.ID == mobEffectInstantDamage && throwerID == m.ID
}
