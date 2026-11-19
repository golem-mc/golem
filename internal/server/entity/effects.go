package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Status effects on a simulated body: the effect package's set and
// countdown, vanilla's immunity checks, and each due tick's work (poison
// and wither as hurts, regeneration and instant heals as health). The
// server builds the packets from the events' changes.

// LivingEffects holds the effect set and extra hearts shared by living bodies.
type LivingEffects struct {
	effects  effect.Set
	revision uint64
	// Absorption is the extra hearts spent before health.
	Absorption float64
}

const (
	mobEffectAbsorption    = registry.MobEffectAbsorption
	mobEffectPoison        = registry.MobEffectPoison
	mobEffectWither        = registry.MobEffectWither
	mobEffectRegeneration  = registry.MobEffectRegeneration
	mobEffectInstantHealth = registry.MobEffectInstantHealth
	mobEffectInstantDamage = registry.MobEffectInstantDamage
	mobEffectSpeed         = registry.MobEffectSpeed
	mobEffectSlowness      = registry.MobEffectSlowness
	mobEffectStrength      = registry.MobEffectStrength
	mobEffectWeakness      = registry.MobEffectWeakness
)

// effectSpeed applies speed (+20% a level) and slowness (-15% a level) to a
// movement_speed value, each multiplying the total, floored at 0.
// vanilla: MobEffects.SPEED, SLOWNESS (ADD_MULTIPLIED_TOTAL)
func (m *Mob) effectSpeed(speed float64) float64 {
	return math.Max(0, speed*m.effectFactor())
}

// effectFactor is the product of the speed and slowness multipliers.
func (m *Mob) effectFactor() float64 {
	f := 1.0
	if amp, ok := m.effects.Amplifier(mobEffectSpeed); ok {
		f *= 1 + 0.2*float64(amp+1)
	}
	if amp, ok := m.effects.Amplifier(mobEffectSlowness); ok {
		f *= 1 + -0.15*float64(amp+1)
	}
	return f
}

// effectLevel is the effect's amplifier plus one, or 0 when the body lacks it.
func (m *Mob) effectLevel(id int32) int {
	if amp, ok := m.effects.Amplifier(id); ok {
		return int(amp) + 1
	}
	return 0
}

// effectAttackDamage applies the strength (+3 a level) and weakness (-4 a
// level) modifiers to an attack_damage value, floored at 0.
// vanilla: MobEffects.STRENGTH, WEAKNESS (ADD_VALUE)
func (m *Mob) effectAttackDamage(damage float64) float64 {
	if amp, ok := m.effects.Amplifier(mobEffectStrength); ok {
		damage += 3 * float64(amp+1)
	}
	if amp, ok := m.effects.Amplifier(mobEffectWeakness); ok {
		damage -= 4 * float64(amp+1)
	}
	return math.Max(0, damage)
}

// effectsStep runs a tick of the effects between the base-tick bites and
// the body's actions: each due tick lands, and every change goes back as an
// event for the server's packets.
// vanilla: LivingEntity.tickEffects, MobEffectInstance.tickServer
func (m *Mob) effectsStep(events []MobEvent) []MobEvent {
	events = m.totemEvents(events)
	changes := m.effects.Tick(int32(m.Age), func(in *effect.Instance) bool {
		return m.applyEffectTick(in, &events)
	})
	for _, ch := range changes {
		events = append(events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: ch})
	}
	if len(changes) > 0 {
		m.revision++
		m.clampAbsorption()
	}
	return events
}

// applyEffectTick runs one due tick of an effect and reports whether it
// stays. A bite lands like a fire tick, through the window with no armour
// (magic and wither bypass it); a heal is capped at max health. Hunger and
// saturation do nothing on a mob. Speed, slowness, strength and weakness are
// read where pace and attack damage are; absorption stays only while its
// extra hearts last.
// vanilla: the applyEffectTick of PoisonMobEffect, WitherMobEffect,
// RegenerationMobEffect, HealOrHarmMobEffect and AbsorptionMobEffect
func (m *Mob) applyEffectTick(in *effect.Instance, events *[]MobEvent) bool {
	return applyLivingEffect(in, m.typeID(), m.Health, m.healthCap(), m.Absorption, m.heal, func(amount float64, typ registry.DamageType) { m.effectBite(events, typ, amount) })
}

// applyLivingEffect runs the due work shared by living bodies.
// vanilla: MobEffect.applyEffectTick
func applyLivingEffect(in *effect.Instance, typ int32, health, cap, absorption float64, heal func(float64), hurt func(float64, registry.DamageType)) bool {
	switch in.ID {
	case mobEffectPoison:
		if health > 1 {
			hurt(1, rules.DamageTypeMagic)
		}
	case mobEffectWither:
		hurt(1, rules.DamageTypeWither)
	case mobEffectRegeneration:
		if health < cap {
			heal(1)
		}
	case mobEffectInstantHealth, mobEffectInstantDamage:
		amount, harmful := rules.InstantEffect(typ, in.ID, in.Amplifier, 1)
		if harmful {
			hurt(amount, rules.DamageTypeMagic)
		} else {
			heal(amount)
		}
	case mobEffectAbsorption:
		return absorption > 0
	}
	return true
}

// effectBite is a sourceless hurt reported like a fire tick: no armour, no
// shove; a whole one has the damage event and voice, a softened one only
// health.
// vanilla: LivingEntity.hurtServer, actuallyHurt
func (m *Mob) effectBite(events *[]MobEvent, damageType registry.DamageType, amount float64) {
	landed, soft := m.plainHurt(amount, damageType, false)
	if !landed {
		return
	}
	m.noteLastDamage(damageType)
	if !soft {
		*events = append(*events, MobEvent{Mob: m, Kind: MobBurned, DamageType: damageType})
	}
}

// Heal restores health from outside the entity package.
func (m *Mob) Heal(amount float64) { m.heal(amount) }

// heal is LivingEntity.heal: nothing for the dead, capped at the maximum.
func (m *Mob) heal(amount float64) {
	if m.Health > 0 {
		m.SetMetadataHealth(math.Min(m.healthCap(), m.Health+amount))
	}
}

// typeID is the entity type ID the registry's tags are indexed by; every
// kind here has one.
func (m *Mob) typeID() int32 {
	id, _ := m.typeIDOK()
	return id
}

// TypeID is the body's entity type ID.
func (m *Mob) TypeID() int32 { return m.typeID() }

// AddEffect gives the body an effect and reports the change, if any. An
// effect the type refuses (the undead refuse poison and regeneration) never
// lands; /effect reports that as failed.
// vanilla: LivingEntity.addEffect, canBeAffected
func (m *Mob) AddEffect(in effect.Instance) (effect.Change, bool) {
	// vanilla: WitherBoss.addEffect
	if m.Type == TypeWither || m.Type == TypeEnderDragon || !rules.CanBeAffected(m.typeID(), in.ID) {
		m.EffectCause = effect.CauseOther
		return effect.Change{}, false
	}
	cause := m.takeEffectCause()
	if m.EffectGate != nil && m.EffectGate.Listening() {
		old, had := m.effects.Get(in.ID)
		var oldp *effect.Instance
		if had {
			oldp = &old
		}
		edited, ok := m.askEffect(oldp, &in, cause)
		if !ok {
			return effect.Change{}, false
		}
		in = edited
	}
	return m.LivingEffects.AddEffect(in)
}

// AddEffect gives a living body an effect after its type immunity check.
func (m *LivingEffects) AddEffect(in effect.Instance) (effect.Change, bool) {
	change, changed := m.effects.Add(in)
	if changed {
		m.revision++
	}
	if in.ID == mobEffectAbsorption {
		m.Absorption = math.Max(m.Absorption, float64(4*(1+in.Amplifier)))
	}
	m.clampAbsorption()
	return change, changed
}

// clampAbsorption is onAttributeUpdated's max_absorption cap: an ended or
// weaker absorption takes the hearts above its own with it.
// vanilla: LivingEntity.onAttributeUpdated
func (m *LivingEffects) clampAbsorption() {
	m.Absorption = math.Max(0, math.Min(m.Absorption, rules.MaxAbsorption(&m.effects)))
}

// absorb spends absorption on a hurt first and returns what is left.
// vanilla: LivingEntity.actuallyHurt
func (m *LivingEffects) absorb(damage float64) float64 {
	left := math.Max(damage-m.Absorption, 0)
	m.Absorption = math.Max(0, m.Absorption-(damage-left))
	return left
}

// RemoveEffect takes one effect off the body.
func (m *LivingEffects) RemoveEffect(id int32) (effect.Change, bool) {
	change, ok := m.effects.Remove(id)
	if ok {
		m.revision++
	}
	m.clampAbsorption()
	return change, ok
}

// ClearEffects takes every effect off the body (vanilla: LivingEntity.removeAllEffects).
func (m *LivingEffects) ClearEffects() []effect.Change {
	changes := m.effects.Clear()
	if len(changes) > 0 {
		m.revision++
	}
	m.clampAbsorption()
	return changes
}

// AllEffects is the active effects in effect ID order.
func (m *LivingEffects) AllEffects() []effect.Instance {
	return m.effects.All()
}

// RestoreEffects loads the set from an entity file's active_effects.
func (m *LivingEffects) RestoreEffects(e effect.Set) {
	m.effects = e
	m.revision++
}

// InheritEffects is convertCommon's copy of active_effects from the replaced
// body, minus what the new type refuses.
// vanilla: ConversionType.convertCommon
func (m *Mob) InheritEffects(old *Mob) {
	cloned := old.effects.Clone()
	for _, in := range cloned.All() {
		m.AddEffect(in)
	}
	// Copied as is, so a spent shield stays spent; the clamp drops it if the
	// new type refused the effect.
	m.Absorption = old.Absorption
	m.clampAbsorption()
}

// HurtByInstantMagic is an instantaneous harm (or healing on the undead)
// landing on the mob: indirect magic from the potion or cloud at (fromX,
// fromZ), caused by ownerID. An enderman teleports away from a thrown potion
// (thrown) instead and takes nothing; a cloud's rain it feels. Landed means the damage got through the hurt
// window; softened is the window's.
// vanilla: HealOrHarmMobEffect.applyInstantaneousEffect, EnderMan.hurtServer
func (m *Mob) HurtByInstantMagic(amount, fromX, fromZ float64, ownerID int32, ownerIsPlayer, creative, thrown bool) (landed, softened bool) {
	if m.Dead || m.InvulnerableTo(rules.DamageTypeIndirectMagic, creative) {
		return false, false
	}
	if thrown {
		m.dodgesProjectile(rules.DamageTypeIndirectMagic, creative)
		// A thrown potion takes the not-a-projectile branch: no damage even
		// to a passenger, which only skips the teleport.
		if m.Type == TypeEnderman {
			return false, false
		}
	}
	landed, softened = m.hurtByDamage(amount, rules.DamageTypeIndirectMagic, ownerID, ownerIsPlayer, creative, fromX-m.X, fromZ-m.Z)
	if ownerID == 0 && !thrown {
		m.endermanHurtRoll(rules.DamageTypeIndirectMagic, creative)
	}
	return landed, softened
}

// EffectRevision changes when the displayed effect set changes.
func (m *LivingEffects) EffectRevision() uint64 { return m.revision }

// HasEffect reports whether an effect is active without copying its instance.
func (m *LivingEffects) HasEffect(id int32) bool { _, ok := m.effects.Amplifier(id); return ok }
