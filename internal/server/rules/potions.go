package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The potions: what a potion's contents give a body that drinks them, and
// what a splash of them leaves in the bodies its spray reaches.

// DrinkPotion applies the brewed potion's effects then the stack's custom
// ones. Instant health and harm land immediately, not as one-tick instances:
// the heal in place, the harm appended to hurts for the caller's pipeline,
// neither sent as an effect. durationScale is potion_duration_scale.
// vanilla: PotionContents.onConsume -> applyToLivingEntity
func DrinkPotion(v *player.Vitals, effects *effect.Set, a *player.Attributes, contents protocol.PotionContents, durationScale float32, hurts *[]Hurt) []effect.Change {
	var changes []effect.Change
	for _, in := range PotionInstances(contents) {
		switch in.ID {
		case effectInstantHealth, effectInstantDamage:
			amount, hurt := InstantEffect(protocol.EntityTypePlayer, in.ID, in.Amplifier, 1)
			if hurt {
				*hurts = append(*hurts, Hurt{DamageTypeIndirectMagic, amount})
			} else {
				Heal(v, effects, a, amount)
			}
		default:
			if change, ok := AddEffect(v, effects, ScaledDuration(in, durationScale)); ok {
				changes = append(changes, change)
			}
		}
	}
	return changes
}

// IsHealOrHarm reports whether id is instant health or instant damage, the
// instantaneous effects a mob feels.
// vanilla: HealOrHarmMobEffect
func IsHealOrHarm(id int32) bool { return id == effectInstantHealth || id == effectInstantDamage }

// InstantEffect is what an instantaneous heal or harm effect does to a body
// of entityType at scale: the amount, and whether it hurts rather than heals.
// vanilla: HealOrHarmMobEffect.applyInstantaneousEffect
func InstantEffect(entityType, id, amplifier int32, scale float64) (amount float64, hurt bool) {
	base, hurt := HealOrHarm(entityType, id, amplifier)
	return InstantAmount(base, scale), hurt
}

// ApplyInstantSaturation applies minecraft:saturation's instantaneous effect
// to v and reports whether in was one.
// vanilla: MobEffect.applyInstantaneousEffect -> SaturationMobEffect.applyEffectTick
func ApplyInstantSaturation(v *player.Vitals, in effect.Instance) bool {
	if in.ID != effectSaturation {
		return false
	}
	food := in.Amplifier + 1
	Eat(v, registry.Food{Nutrition: food, Saturation: float32(food) * 2})
	return true
}

// ScaledDuration is the instance with its duration multiplied by scale in
// single precision, floored and kept at one tick at least; an infinite or
// zero duration is left alone.
// vanilla: MobEffectInstance.withScaledDuration
func ScaledDuration(in effect.Instance, scale float32) effect.Instance {
	if in.IsInfinite() || in.Duration == 0 {
		return in
	}
	// Java's (int) cast saturates and maps NaN to 0; Go's does neither.
	scaled := math.Floor(float64(float32(in.Duration) * scale))
	switch {
	case math.IsNaN(scaled):
		scaled = 0
	case scaled > math.MaxInt32:
		scaled = math.MaxInt32
	}
	in.Duration = int32(max(scaled, 1))
	return in
}

// PotionInstances is the effects a potion's contents carry, in the order
// they are handed out: the brewing's own, then the stack's custom ones
// over them.
// vanilla: PotionContents.getAllEffects
func PotionInstances(contents protocol.PotionContents) []effect.Instance {
	var out []effect.Instance
	if contents.Potion != protocol.NoPotion {
		for _, spec := range registry.PotionEffects(contents.Potion) {
			if in, ok := effect.FromSpec(spec); ok {
				out = append(out, in)
			}
		}
	}
	for _, spec := range contents.CustomEffects {
		out = append(out, effect.FromWireSpec(spec))
	}
	return out
}

// SplashInstance scales a potion's instance by a splash's reach factor:
// duration rounded at a half, icon following particles, hidden chain
// dropped. An effect ending within 20 ticks does not land, and an instant
// one never lands as an instance (see InstantEffect). An infinite or zero
// duration is kept as it is; durationScale is the stack's
// potion_duration_scale.
// vanilla: ThrownSplashPotion.onHitAsPotion, MobEffectInstance.mapDuration
func SplashInstance(in effect.Instance, scale float64, durationScale float32) (effect.Instance, bool) {
	if registry.MobEffectOf(in.ID).Instant {
		return effect.Instance{}, false
	}
	out := effect.Instance{
		ID:        in.ID,
		Duration:  in.Duration,
		Amplifier: in.Amplifier,
		Ambient:   in.Ambient,
		Visible:   in.Visible,
		ShowIcon:  in.Visible,
	}
	if !in.IsInfinite() && in.Duration != 0 {
		out.Duration = int32(scale*float64(in.Duration)*float64(durationScale) + 0.5)
	}
	if !out.IsInfinite() && out.Duration <= splashKeepTicks {
		return effect.Instance{}, false
	}
	return out, true
}

// LingeringInstance is a cloud's instance: duration scaled by the item's
// potion_duration_scale and floored; an instant one is kept as is, for the
// cloud to apply at CloudInstantScale each pass.
// vanilla: PotionContents.forEachEffect, AreaEffectCloud.serverTick
func LingeringInstance(in effect.Instance, scale float64) effect.Instance {
	if !registry.MobEffectOf(in.ID).Instant && !in.IsInfinite() {
		in.Duration = int32(math.Max(1, math.Floor(float64(in.Duration)*scale)))
	}
	return in
}

// The lingering potion's potion_duration_scale, and a cloud's half strength
// for instantaneous effects.
// vanilla: Items.LINGERING_POTION, AreaEffectCloud.serverTick
const (
	LingeringDurationScale = 0.25
	CloudInstantScale      = 0.5
)

// waterPotion is the water brewing, whose splash douses fire and hurts the
// water-sensitive rather than landing effects.
var waterPotion = registry.MustPotion("minecraft:water")

// PotionIsWater reports whether a stack's contents are the plain water
// brewing — the potion present and no custom effects on top, the
// PotionContents.is(Potions.WATER) the water path is taken by.
// vanilla: AbstractThrownPotion.onHit
func PotionIsWater(contents protocol.PotionContents) bool {
	return contents.Potion == waterPotion && len(contents.CustomEffects) == 0
}

// splashKeepTicks is the shortest effect a splash lands at all, vanilla's
// MobEffectInstance.endsWithin(20) the splash's own instances read.
const splashKeepTicks = 20

// InstantBrewing reports whether the potion the contents name has an
// instant effect among its own, which picks the splash's particle burst.
// vanilla: Potion.hasInstantEffects
func InstantBrewing(contents protocol.PotionContents) bool {
	if contents.Potion == protocol.NoPotion {
		return false
	}
	for _, spec := range registry.PotionEffects(contents.Potion) {
		if id, ok := registry.MobEffectID(spec.Effect); ok && registry.MobEffectOf(id).Instant {
			return true
		}
	}
	return false
}

// BasePotionColour is the colour of a potion of no effect at all, the
// un-brewed default every contents' own colour falls back to (0xFF385DC6).
// vanilla: PotionContents.BASE_POTION_COLOR
const BasePotionColour = -13083194

// PotionColour is the level-weighted average of the visible effects'
// colours, opaque, or the unbrewed default. Custom colours are dropped by the
// slot reader, so not modelled.
// vanilla: PotionContents.getColor/getColorOptional
func PotionColour(contents protocol.PotionContents) int32 {
	var red, green, blue, weight int32
	for _, in := range PotionInstances(contents) {
		if !in.Visible {
			continue
		}
		props := registry.MobEffectOf(in.ID)
		strength := in.Amplifier + 1
		red += strength * (props.Color >> 16 & 0xFF)
		green += strength * (props.Color >> 8 & 0xFF)
		blue += strength * (props.Color & 0xFF)
		weight += strength
	}
	if weight == 0 {
		return BasePotionColour
	}
	// 0xFF385DC6 at a step: the opaque alpha the ARGB.opaque of the
	// original gives, at the average's own channels.
	return (BasePotionColour & -0x1000000) | (red/weight)<<16 | (green/weight)<<8 | blue/weight
}
