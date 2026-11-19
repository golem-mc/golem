package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// What the status effects do to a body. The effects themselves - stacking,
// countdown, rhythm - are package effect's; these are the vanilla effect
// classes' applyEffectTick and the places the damage pipeline reads them.

// The damage types the effects hurt with.
const (
	DamageTypeMagic         = registry.DamageTypeMagic
	DamageTypeWither        = registry.DamageTypeWither
	DamageTypeIndirectMagic = registry.DamageTypeIndirectMagic
)

const (
	effectHunger         = registry.MobEffectHunger
	effectPoison         = registry.MobEffectPoison
	effectWeakness       = registry.MobEffectWeakness
	effectWither         = registry.MobEffectWither
	effectRegeneration   = registry.MobEffectRegeneration
	effectInfested       = registry.MobEffectInfested
	effectOozing         = registry.MobEffectOozing
	effectAbsorption     = registry.MobEffectAbsorption
	effectHealthBoost    = registry.MobEffectHealthBoost
	effectSaturation     = registry.MobEffectSaturation
	effectInstantHealth  = registry.MobEffectInstantHealth
	effectInstantDamage  = registry.MobEffectInstantDamage
	effectResistance     = registry.MobEffectResistance
	effectFireResistance = registry.MobEffectFireResistance
	effectWaterBreathing = registry.MobEffectWaterBreathing
	effectConduitPower   = registry.MobEffectConduitPower
	effectNautilus       = registry.MobEffectBreathOfTheNautilus
	effectHaste          = registry.MobEffectHaste
	effectMiningFatigue  = registry.MobEffectMiningFatigue
	effectInvisibility   = registry.MobEffectInvisibility
	effectGlowing        = registry.MobEffectGlowing
	effectSlowFalling    = registry.MobEffectSlowFalling
	effectBlindness      = registry.MobEffectBlindness
	effectLevitation     = registry.MobEffectLevitation
	effectJumpBoost      = registry.MobEffectJumpBoost
)

// ResetsFall reports whether an active effect writes the body's fall distance
// off every tick: slow falling and levitation both do.
// vanilla: LivingEntity.aiStep
func ResetsFall(effects []effect.Instance) bool {
	return hasEffect(effects, effectSlowFalling) || hasEffect(effects, effectLevitation)
}

// HasBlindness reports whether the body carries Blindness, which restricts a
// player's mobility. vanilla: Player.isMobilityRestricted
func HasBlindness(effects []effect.Instance) bool { return hasEffect(effects, effectBlindness) }

// HasLevitation reports whether the body carries Levitation.
func HasLevitation(effects []effect.Instance) bool { return hasEffect(effects, effectLevitation) }

// HasSlowFalling reports whether the body carries Slow Falling, which caps
// the gravity of its travel at 0.01 while it is falling.
// vanilla: LivingEntity.getEffectiveGravity
func HasSlowFalling(effects []effect.Instance) bool {
	return hasEffect(effects, effectSlowFalling)
}

// JumpBoostPower is the rise Jump Boost adds to a jump, a tenth of a block a
// tick a level; zero without the effect.
// vanilla: LivingEntity.getJumpBoostPower
func JumpBoostPower(effects []effect.Instance) float64 {
	for _, in := range effects {
		if in.ID == effectJumpBoost {
			return float64(0.1 * (float32(in.Amplifier) + 1))
		}
	}
	return 0
}

// LevitationLift is the vertical velocity Levitation pulls a body towards,
// 0.05 a level; ok is false without the effect.
// vanilla: LivingEntity.travelInAir
func LevitationLift(effects []effect.Instance) (lift float64, ok bool) {
	for _, in := range effects {
		if in.ID == effectLevitation {
			return 0.05 * float64(in.Amplifier+1), true
		}
	}
	return 0, false
}

// SafeFallDistance is the free fall of a body whose only modifiers on the
// safe_fall_distance attribute are the active effects'.
// vanilla: Attributes.SAFE_FALL_DISTANCE, MobEffects.JUMP_BOOST
func SafeFallDistance(effects []effect.Instance) float64 {
	var a player.Attributes
	UpdateEffectAttributes(&a, effects)
	return a.Value(registry.AttributeSafeFallDistance)
}

// tickEffects runs one tick of the body's effects on the vitals, appending
// their blows to hurts.
//
// Deliberate difference: heals land in place before the food tick, but every
// hurt goes through the caller's pipeline after the tick, whereas vanilla
// lands baseTick's own hurts (burn, wall, drowning, void) before
// tickEffects. So a regeneration heal at full health is capped where
// vanilla's would follow a burn.
func tickEffects(v *player.Vitals, effects *effect.Set, a *player.Attributes, hurts *[]Hurt) []effect.Change {
	changes := effects.Tick(int32(v.AgeTicks), func(in *effect.Instance) bool {
		return applyEffectTick(v, effects, a, in, hurts)
	})
	clampAbsorption(v, effects)
	ClampHealth(v, effects, a)
	return changes
}

// Poison immunity beyond the undead tag, and the single-effect immunities.
var (
	nautilusEntityType   = mustEntityType("minecraft:nautilus")
	spiderEntityType     = mustEntityType("minecraft:spider")
	caveSpiderEntityType = mustEntityType("minecraft:cave_spider")
	parchedEntityType    = mustEntityType("minecraft:parched")
	witherSkelEntityType = mustEntityType("minecraft:wither_skeleton")
)

func mustEntityType(name string) int32 {
	id, ok := registry.EntityTypeID(name)
	if !ok {
		panic("rules: unknown entity type " + name)
	}
	return id
}

// CanBeAffected reports whether the entity type takes the effect: the
// ignores-poison-and-regen tag (the undead) refuses both, spiders and nautiluses
// refuse poison, the parched weakness, and single-effect breeders that one
// effect. It gates addEffect, so a refused effect never lands.
// vanilla: LivingEntity.canBeAffected, Spider.canBeAffected,
// AbstractNautilus.canBeAffected, Parched.canBeAffected, WitherSkeleton.canBeAffected
func CanBeAffected(entityType, effectID int32) bool {
	switch {
	case registry.EntityType(entityType).InTag(registry.EntityTypeTagIDImmuneToInfested):
		return effectID != effectInfested
	case registry.EntityType(entityType).InTag(registry.EntityTypeTagIDImmuneToOozing):
		return effectID != effectOozing
	}
	if effectID == effectPoison && (entityType == spiderEntityType || entityType == caveSpiderEntityType || entityType == nautilusEntityType) {
		return false
	}
	if effectID == effectWeakness && entityType == parchedEntityType {
		return false
	}
	if effectID == effectWither && entityType == witherSkelEntityType {
		return false
	}
	if registry.EntityType(entityType).InTag(registry.EntityTypeTagIDIgnoresPoisonAndRegen) {
		return effectID != effectPoison && effectID != effectRegeneration
	}
	return true
}

// HealOrHarm is a due instant_health or instant_damage tick: whether it lands
// as a magic hurt, and 4 health or 6 harm by what it does to the body, shifted
// left by the amplifier. The inverted-healing-and-harm tag (the undead) swaps
// the direction, so Healing hurts a zombie for 6.
// vanilla: HealOrHarmMobEffect.applyEffectTick
func HealOrHarm(entityType, effectID, amplifier int32) (amount float64, hurt bool) {
	hurt = effectID == effectInstantDamage
	if registry.EntityType(entityType).InTag(registry.EntityTypeTagIDInvertedHealingAndHarm) {
		hurt = !hurt
	}
	base := int32(4)
	if hurt {
		base = 6
	}
	return float64(max(javaShift(base, amplifier), 0)), hurt
}

// InstantAmount scales an instantaneous amount by a splash's or cloud's
// distance factor, truncating (int)(scale*base + 0.5); a drink's factor 1
// leaves the base.
// vanilla: HealOrHarmMobEffect.applyInstantaneousEffect
func InstantAmount(base, factor float64) float64 {
	return math.Floor(base*factor + 0.5)
}

// OozingSlimeCount is the slimes a dying body's oozing asks for.
// vanilla: MobEffects registers OozingMobEffect with the spawnedCount that
// answers two whatever the dice say
func OozingSlimeCount() int {
	return 2
}

// OozingSlimesToSpawn clamps the requested count to what the nearby slimes
// leave of the cramming rule; a rule below 1 applies no cap.
// vanilla: OozingMobEffect.numberOfSlimesToSpawn
func OozingSlimesToSpawn(maxEntityCramming, nearbySlimes, numberRequested int) int {
	if maxEntityCramming < 1 {
		return numberRequested
	}
	return max(0, min(maxEntityCramming-nearbySlimes, numberRequested))
}

// WeavingMaxCobwebs is the most cobwebs a dying body's weaving lays, one
// toss in two a third web. vanilla: MobEffects registers WeavingMobEffect
// with the maxCobwebs of two plus the next int in two
func WeavingMaxCobwebs(dice *rand.Rand) int {
	return 2 + dice.Intn(2)
}

// applyEffectTick runs one due effect tick, reporting whether it stays.
// Not here: bad_omen and raid_omen (the raid pass runs them), the
// attribute-riding effects, the invisible and glowing flags, water
// breathing's air (HasWaterBreathing), and the client-only nausea, night
// vision, blindness and darkness.
// vanilla: the applyEffectTick of HungerMobEffect, PoisonMobEffect,
// WitherMobEffect, RegenerationMobEffect, AbsorptionMobEffect,
// SaturationMobEffect and HealOrHarmMobEffect
func applyEffectTick(v *player.Vitals, effects *effect.Set, a *player.Attributes, in *effect.Instance, hurts *[]Hurt) bool {
	switch id := in.ID; id {
	case effectHunger:
		Exhaust(v, float64(0.005*float32(in.Amplifier+1)))
	case effectPoison:
		if v.Health > 1 {
			*hurts = append(*hurts, Hurt{DamageTypeMagic, 1})
		}
	case effectWither:
		*hurts = append(*hurts, Hurt{DamageTypeWither, 1})
	case effectRegeneration:
		if v.Health < MaxHealthOf(effects, a) {
			Heal(v, effects, a, 1)
		}
	case effectAbsorption:
		return v.Absorption > 0
	case effectSaturation:
		// FoodData.eat(amplifier + 1, 1.0f)
		ApplyInstantSaturation(v, *in)
	case effectInstantHealth, effectInstantDamage:
		amount, hurt := InstantEffect(protocol.EntityTypePlayer, id, in.Amplifier, 1)
		if hurt {
			*hurts = append(*hurts, Hurt{DamageTypeMagic, amount})
		} else {
			Heal(v, effects, a, amount)
		}
	}
	return true
}

// javaShift is Java's int x << n, whose shift distance is taken mod 32.
func javaShift(x, n int32) int32 {
	return x << (n & 31)
}

// Heal is LivingEntity.heal: nothing for the dead, capped at MaxHealthOf.
func Heal(v *player.Vitals, effects *effect.Set, a *player.Attributes, amount float64) {
	if v.Health > 0 {
		v.Health = math.Min(MaxHealthOf(effects, a), v.Health+amount)
	}
}

// MaxHealthOf is the player's max_health attribute value, its health boost
// modifier first brought in line with the effects so a boost that changed
// this tick counts before the attribute pass runs. A nil map is a fresh
// player's. vanilla: LivingEntity.getMaxHealth, MobEffect.addAttributeModifiers
func MaxHealthOf(effects *effect.Set, a *player.Attributes) float64 {
	if a == nil {
		a = &player.Attributes{}
	}
	in, ok := effect.Instance{}, false
	if effects != nil {
		in, ok = effects.Get(effectHealthBoost)
	}
	healthBoost.apply(a, in, ok)
	return a.Value(healthBoost.attribute)
}

// MaxAbsorption is the max_absorption attribute: none but what the
// absorption effect's modifier adds, four per level.
func MaxAbsorption(effects *effect.Set) float64 {
	if in, ok := effects.Get(effectAbsorption); ok {
		return float64(4 * (in.Amplifier + 1))
	}
	return 0
}

// clampAbsorption is onAttributeUpdated's max_absorption cap: a weaker or
// ended absorption effect takes the extra hearts above its own with it.
func clampAbsorption(v *player.Vitals, effects *effect.Set) {
	v.Absorption = math.Max(0, math.Min(v.Absorption, MaxAbsorption(effects)))
}

// ClampHealth is onAttributeUpdated's max_health cap: a weaker or ended
// health boost takes the health above the bar it leaves behind with it.
// vanilla: LivingEntity.onAttributeUpdated
func ClampHealth(v *player.Vitals, effects *effect.Set, a *player.Attributes) {
	v.Health = math.Min(v.Health, MaxHealthOf(effects, a))
}

// AddEffect gives the body an effect (vanilla: LivingEntity.addEffect),
// reporting the change if there was one. Absorption fills the extra hearts
// for the level given, whether or not the effect changed.
func AddEffect(v *player.Vitals, effects *effect.Set, in effect.Instance) (effect.Change, bool) {
	change, changed := effects.Add(in)
	if in.ID == effectAbsorption {
		v.Absorption = math.Max(v.Absorption, float64(4*(1+in.Amplifier)))
	}
	clampAbsorption(v, effects)
	return change, changed
}

// RemoveEffect takes one effect off the body.
func RemoveEffect(v *player.Vitals, effects *effect.Set, a *player.Attributes, id int32) (effect.Change, bool) {
	change, ok := effects.Remove(id)
	clampAbsorption(v, effects)
	ClampHealth(v, effects, a)
	return change, ok
}

// ClearEffects takes every effect off the body (vanilla: LivingEntity.removeAllEffects).
func ClearEffects(v *player.Vitals, effects *effect.Set, a *player.Attributes) []effect.Change {
	changes := effects.Clear()
	clampAbsorption(v, effects)
	ClampHealth(v, effects, a)
	return changes
}

// Consumed is what a finished use's on_consume_effects came to: the effect
// changes, the sounds play_sound named, and the diameter of each
// teleport_randomly, left to the caller since it needs the world.
type Consumed struct {
	Changes   []effect.Change
	Sounds    []protocol.SoundHolder
	Teleports []registry.ConsumeEffect
}

// EffectBody is a body whose effects a consume effect changes: the player's
// vitals and set, or a mob.
type EffectBody interface {
	AddEffect(in effect.Instance) (effect.Change, bool)
	RemoveEffect(id int32) (effect.Change, bool)
	ClearEffects() []effect.Change
}

// playerEffects is a player's vitals and effect set as an EffectBody.
type playerEffects struct {
	v       *player.Vitals
	effects *effect.Set
	a       *player.Attributes
}

func (p playerEffects) AddEffect(in effect.Instance) (effect.Change, bool) {
	return AddEffect(p.v, p.effects, in)
}

func (p playerEffects) RemoveEffect(id int32) (effect.Change, bool) {
	return RemoveEffect(p.v, p.effects, p.a, id)
}

func (p playerEffects) ClearEffects() []effect.Change { return ClearEffects(p.v, p.effects, p.a) }

// Consume runs a consumable's on_consume_effects on the body, in order.
// vanilla: Consumable.onConsume and the ConsumeEffect classes
func Consume(v *player.Vitals, effects *effect.Set, a *player.Attributes, consume []registry.ConsumeEffect, rng *rand.Rand) Consumed {
	return ConsumeOn(playerEffects{v, effects, a}, consume, rng)
}

// ConsumeOn is Consume on any body that carries effects.
func ConsumeOn(b EffectBody, consume []registry.ConsumeEffect, rng *rand.Rand) Consumed {
	var out Consumed
	for _, ce := range consume {
		switch ce.Type {
		case registry.ApplyEffects:
			if rng.Float32() >= ce.Probability {
				continue
			}
			for _, spec := range ce.Effects {
				if in, ok := effect.FromSpec(spec); ok {
					if change, ok := b.AddEffect(in); ok {
						out.Changes = append(out.Changes, change)
					}
				}
			}
		case registry.RemoveEffects:
			for _, name := range ce.Remove {
				if id, ok := registry.MobEffectID(name); ok {
					if change, ok := b.RemoveEffect(id); ok {
						out.Changes = append(out.Changes, change)
					}
				}
			}
		case registry.ClearAllEffects:
			out.Changes = append(out.Changes, b.ClearEffects()...)
		case registry.PlaySound:
			out.Sounds = append(out.Sounds, ce.Sound)
		case registry.TeleportRandomly:
			out.Teleports = append(out.Teleports, ce)
		}
	}
	return out
}

// FireImmune reports whether fire resistance turns a hurt of the given type
// away whole (vanilla: LivingEntity.hurtServer).
func FireImmune(effects []effect.Instance, damageType registry.DamageType) bool {
	return damageType.InTag(registry.DamageTypeTagIDIsFire) && hasEffect(effects, effectFireResistance)
}

// HasWaterBreathing reports whether the body's effects spare its breath under
// water: water_breathing, conduit_power or breath_of_the_nautilus (vanilla:
// MobEffectUtil.hasWaterBreathing).
func HasWaterBreathing(effects []effect.Instance) bool {
	return hasEffect(effects, effectWaterBreathing) ||
		hasEffect(effects, effectConduitPower) ||
		hasEffect(effects, effectNautilus)
}

// turtleHelmetBreathingTicks is how long the water breathing a turtle helmet
// grants lasts; the helmet refreshes it every tick it is worn out of water.
// vanilla: Player.turtleHelmetTick
const turtleHelmetBreathingTicks = 200

// TurtleHelmetBreathing is the turtle helmet's water breathing with the eyes
// out of water: 200 ticks, not ambient, no particles, icon shown. The caller
// reads the head slot, the one the equippable component names.
// vanilla: Player.turtleHelmetTick
func TurtleHelmetBreathing(head protocol.ItemStack, eyesInWater bool) (effect.Instance, bool) {
	if eyesInWater || head.Empty() || head.ItemID != int32(registry.ItemTurtleHelmet) {
		return effect.Instance{}, false
	}
	return effect.New(effectWaterBreathing, turtleHelmetBreathingTicks, 0, false, false, true), true
}

// ShouldRefillAirSupply reports whether the body's effects let its breath
// climb back up: breath_of_the_nautilus on its own does not, every other
// combination does (vanilla: MobEffectUtil.shouldEffectsRefillAirsupply).
func ShouldRefillAirSupply(effects []effect.Instance) bool {
	return !hasEffect(effects, effectNautilus) ||
		hasEffect(effects, effectWaterBreathing) ||
		hasEffect(effects, effectConduitPower)
}

// Invisible reports whether the body's effects hide it from other players.
// vanilla: LivingEntity.updateInvisibilityStatus
func Invisible(effects []effect.Instance) bool {
	return hasEffect(effects, effectInvisibility)
}

// InvisibleToOthers reports a spectator always, any other mode by the
// Invisibility effect.
// vanilla: ServerPlayer.updateInvisibilityStatus
func InvisibleToOthers(mode int32, effects []effect.Instance) bool {
	return mode == protocol.GameModeSpectator || Invisible(effects)
}

// SpectatorBodyVisible reports whether a viewer sees a player's body: a
// spectator viewer only bodies whose camera is on themselves, others no
// spectator's body.
// vanilla: ServerPlayer.broadcastToPlayer
func SpectatorBodyVisible(viewerSpectator, bodySpectator, bodyOnOwnCamera bool) bool {
	if viewerSpectator {
		return bodyOnOwnCamera
	}
	return !bodySpectator
}

// Glowing reports whether the body's effects outline it through walls.
// vanilla: LivingEntity.isCurrentlyGlowing
func Glowing(effects []effect.Instance) bool {
	return hasEffect(effects, effectGlowing)
}

// EffectAddedSound is the sound an effect plays when newly added, or SoundNone.
// vanilla: MobEffect.onEffectAdded, MobEffects.BAD_OMEN
func EffectAddedSound(id int32) registry.Sound {
	switch id {
	case registry.MobEffectBadOmen:
		return registry.SoundEventMobEffectBadOmen
	case registry.MobEffectTrialOmen:
		return registry.SoundEventMobEffectTrialOmen
	case registry.MobEffectRaidOmen:
		return registry.SoundEventMobEffectRaidOmen
	}
	return registry.SoundNone
}

// Resist applies the resistance effect to a hurt's damage, a fifth off per
// level (vanilla: LivingEntity.getDamageAfterMagicAbsorb). Starvation
// bypasses effects, the void and /kill resistance.
func Resist(effects []effect.Instance, damageType registry.DamageType, damage float64) float64 {
	if damageType.InTag(registry.DamageTypeTagIDBypassesEffects) ||
		damageType.InTag(registry.DamageTypeTagIDBypassesResistance) {
		return damage
	}
	for _, in := range effects {
		if in.ID == effectResistance {
			absorb := 25 - (in.Amplifier+1)*5
			return math.Max(float64(float32(damage)*float32(absorb)/25), 0)
		}
	}
	return damage
}

// ProtectionBypassed reports whether a hurt skips enchantment protection
// (vanilla: LivingEntity.getDamageAfterMagicAbsorb returns before it for
// bypasses_effects and bypasses_enchantments).
func ProtectionBypassed(damageType registry.DamageType) bool {
	return damageType.InTag(registry.DamageTypeTagIDBypassesEffects) ||
		damageType.InTag(registry.DamageTypeTagIDBypassesEnchantments)
}

// AbsorbDamage spends the body's extra hearts on a hurt first and returns
// what is left for the health (vanilla: Player.actuallyHurt).
func AbsorbDamage(v *player.Vitals, damage float64) float64 {
	left := math.Max(damage-v.Absorption, 0)
	v.Absorption = math.Max(0, v.Absorption-(damage-left))
	return left
}

func hasEffect(effects []effect.Instance, id int32) bool {
	for _, in := range effects {
		if in.ID == id {
			return true
		}
	}
	return false
}

// Levitating reports whether the body carries Levitation, which ends a glide.
// vanilla: LivingEntity.canGlide
func Levitating(effects []effect.Instance) bool {
	return hasEffect(effects, effectLevitation)
}
