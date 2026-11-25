package rules

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// EquipmentModifiers returns the modifiers a stack in slot applies: its
// attribute_modifiers whose group holds the slot, plus its enchantments'
// attribute effects (data id over the slot). A broken item applies none.
// vanilla: ItemStack.forEachModifier(EquipmentSlot, ...)
func EquipmentModifiers(stack protocol.ItemStack, slot string) []registry.AttributeModifier {
	if stack.Empty() || broken(stack) {
		return nil
	}
	var out []registry.AttributeModifier
	for _, m := range registry.StackModifiers(stack) {
		if registry.SlotGroupHolds(m.Slot, slot) {
			out = append(out, m)
		}
	}
	for _, ench := range enchantsOn(stack) {
		if !ench.def.MatchesSlot(slot) {
			continue
		}
		for _, attr := range ench.def.Effects.Attributes {
			op, ok := registry.AttributeOperationByName(attr.Operation)
			target, known := registry.AttributeByName(attr.Attribute)
			if !ok || !known {
				continue
			}
			out = append(out, registry.AttributeModifier{
				Attribute: registry.AttributeID(target.ID),
				ID:        attr.ID + "/" + slot,
				Amount:    enchLevelValue(attr.Amount, ench.level),
				Operation: op,
				Slot:      slot,
			})
		}
	}
	return out
}

// EquipmentAttributeValue is the attribute's default with only the
// equipment's modifiers: a mob's view, as mobs keep no attribute map here.
// vanilla: LivingEntity.collectEquipmentChanges, AttributeInstance.calculateValue
func EquipmentAttributeValue(pieces []EnchPiece, id registry.AttributeID) float64 {
	attr := id.Attribute()
	in := player.AttributeInstance{Attribute: attr, Base: attr.Default}
	for _, p := range pieces {
		for _, m := range EquipmentModifiers(p.Stack, p.Slot) {
			if m.Attribute == id {
				in.Modifiers = append(in.Modifiers, player.Modifier{ID: m.ID, Amount: m.Amount, Operation: m.Operation})
			}
		}
	}
	return in.Value()
}

// BurningTimeAttribute scales every ignition's tick count.
// vanilla: LivingEntity.igniteForTicks
const BurningTimeAttribute = registry.AttributeBurningTime

// ExplosionKnockbackResistanceAttribute is the share of a blast's push a
// living body shrugs off.
// vanilla: ServerExplosion.hurtEntities
const ExplosionKnockbackResistanceAttribute = registry.AttributeExplosionKnockbackResistance

// ApproximateAttributeWith is the attribute with the stack's
// attribute_modifiers over base, which a mob's pickup ranks pieces by.
// Callers pass base 0, exact for the add-value modifiers weapons and armour
// carry.
// vanilla: Mob.getApproximateAttributeWith, ItemAttributeModifiers.compute
func ApproximateAttributeWith(stack protocol.ItemStack, attribute registry.AttributeID, slot string, base float64) float64 {
	if stack.Empty() {
		return base
	}
	value := base
	for _, mod := range registry.StackModifiers(stack) {
		if mod.Attribute != attribute || !registry.SlotGroupHolds(mod.Slot, slot) {
			continue
		}
		switch mod.Operation {
		case registry.AttributeAddValue:
			value += mod.Amount
		case registry.AttributeAddMultipliedBase:
			value += mod.Amount * base
		case registry.AttributeAddMultipliedTotal:
			value += mod.Amount * value
		}
	}
	return value
}

// broken is ItemStack.isBroken: worn to its max damage.
func broken(stack protocol.ItemStack) bool {
	return registry.StackDamageable(stack) && stack.Damage >= registry.StackMaxDamage(stack)
}

// UpdateEquipmentAttributes moves the modifiers of every equipment slot whose
// stack has changed since the last call: the old stack's come off, then the
// new one's go on. vanilla: LivingEntity.collectEquipmentChanges
func UpdateEquipmentAttributes(a *player.Attributes, now player.Equipment) {
	var changed [len(player.EquipmentSlots)]bool
	for i, slot := range player.EquipmentSlots {
		// Two empty stacks carry no modifiers; skipping them spares the
		// component compare on most slots of most players every tick.
		if now[i].Empty() && a.Worn[i].Empty() || now[i].Equal(a.Worn[i]) {
			continue
		}
		changed[i] = true
		for _, m := range EquipmentModifiers(a.Worn[i], slot) {
			a.RemoveModifier(m.Attribute, m.ID)
		}
	}
	for i, slot := range player.EquipmentSlots {
		if !changed[i] {
			continue
		}
		for _, m := range EquipmentModifiers(now[i], slot) {
			a.RemoveModifier(m.Attribute, m.ID)
			a.AddModifier(m.Attribute, player.Modifier{ID: m.ID, Amount: m.Amount, Operation: m.Operation})
		}
		a.Worn[i] = now[i]
	}
}

// Player-state modifiers: creative reach, sprint, and the crouch's
// locator-bar hide.
// vanilla: ServerPlayer's CREATIVE_*_INTERACTION_RANGE_MODIFIER and
// WAYPOINT_TRANSMIT_RANGE_CROUCH_MODIFIER, LivingEntity.SPEED_MODIFIER_SPRINTING
var (
	creativeBlockRange  = player.Modifier{ID: "minecraft:creative_mode_block_range", Amount: 0.5}
	creativeEntityRange = player.Modifier{ID: "minecraft:creative_mode_entity_range", Amount: 2}
	sprintingSpeed      = player.Modifier{ID: "minecraft:sprinting", Amount: float64(float32(0.3)),
		Operation: registry.AttributeAddMultipliedTotal}
	// crouchHide takes the whole of a crouching player's waypoint transmit
	// range down: the crouch is how a player steps off the locator bar.
	crouchHide = player.Modifier{ID: "minecraft:waypoint_transmit_range_crouch", Amount: -1,
		Operation: registry.AttributeAddMultipliedTotal}
)

// UpdateStateAttributes puts on or takes off the modifiers that follow the
// game mode, the sprint and the crouch.
// vanilla: ServerPlayer.updatePlayerAttributes, LivingEntity.setSprinting
func UpdateStateAttributes(a *player.Attributes, creative, sprinting, crouching bool) {
	toggle(a, registry.AttributeBlockInteractionRange, creativeBlockRange, creative)
	toggle(a, registry.AttributeEntityInteractionRange, creativeEntityRange, creative)
	toggle(a, registry.AttributeMovementSpeed, sprintingSpeed, sprinting)
	// ServerPlayer.updatePlayerAttributes obtains the instance even while standing.
	a.Instance(registry.AttributeWaypointTransmitRange)
	toggle(a, registry.AttributeWaypointTransmitRange, crouchHide, crouching)
}

func toggle(a *player.Attributes, attribute registry.AttributeID, m player.Modifier, on bool) {
	if on {
		a.AddModifier(attribute, m)
	} else {
		a.RemoveModifier(attribute, m.ID)
	}
}

// freezeSlowModifier is the transient movement_speed modifier the powder
// snow puts on a body: vanilla's own ID, an ADD_VALUE whose amount follows
// the frozen share of the full.
var freezeSlowModifier = player.Modifier{ID: "minecraft:powder_snow", Operation: registry.AttributeAddValue}

// UpdateFreezeAttributes applies powder snow's slow (-0.05 times the frozen
// share) while frozen ticks remain and the block underneath is not air, and
// removes it otherwise. It tracks the counter, so is refreshed every tick.
// vanilla: LivingEntity.tryAddFrost, removeFrost
func UpdateFreezeAttributes(a *player.Attributes, ticksFrozen int, underfootAir bool) {
	if ticksFrozen <= 0 || underfootAir {
		a.RemoveModifier(registry.AttributeMovementSpeed, freezeSlowModifier.ID)
		return
	}
	m := freezeSlowModifier
	m.Amount = FreezeSpeedAmount(ticksFrozen)
	a.AddModifier(registry.AttributeMovementSpeed, m)
}

// effectModifiers are the effects' attribute modifiers, per level: speed
// and slowness on movement_speed, strength +3 and weakness -4 attack_damage,
// jump boost +1 safe_fall_distance, haste and mining fatigue 0.1
// attack_speed, invisibility all of waypoint_transmit_range, health boost +4
// max_health, absorption +4 max_absorption, luck and unluck ±1 luck. Speed,
// slowness, haste, mining fatigue and invisibility multiply the total; the
// rest add.
// vanilla: MobEffects.SPEED, SLOWNESS, STRENGTH, WEAKNESS, JUMP_BOOST,
// HASTE, MINING_FATIGUE, INVISIBILITY, HEALTH_BOOST, ABSORPTION, LUCK,
// UNLUCK
var effectModifiers = []effectModifier{
	{registry.MobEffectSpeed, registry.AttributeMovementSpeed, "minecraft:effect.speed", float64(float32(0.2)), registry.AttributeAddMultipliedTotal},
	{registry.MobEffectSlowness, registry.AttributeMovementSpeed, "minecraft:effect.slowness", float64(float32(-0.15)), registry.AttributeAddMultipliedTotal},
	{registry.MobEffectStrength, registry.AttributeAttackDamage, "minecraft:effect.strength", 3, registry.AttributeAddValue},
	{registry.MobEffectWeakness, registry.AttributeAttackDamage, "minecraft:effect.weakness", -4, registry.AttributeAddValue},
	{registry.MobEffectJumpBoost, registry.AttributeSafeFallDistance, "minecraft:effect.jump_boost", 1, registry.AttributeAddValue},
	{registry.MobEffectHaste, registry.AttributeAttackSpeed, "minecraft:effect.haste", float64(float32(0.1)), registry.AttributeAddMultipliedTotal},
	{registry.MobEffectMiningFatigue, registry.AttributeAttackSpeed, "minecraft:effect.mining_fatigue", float64(float32(-0.1)), registry.AttributeAddMultipliedTotal},
	{registry.MobEffectInvisibility, registry.AttributeWaypointTransmitRange, "minecraft:effect.waypoint_transmit_range_hide", -1, registry.AttributeAddMultipliedTotal},
	{registry.MobEffectHealthBoost, registry.AttributeMaxHealth, "minecraft:effect.health_boost", 4, registry.AttributeAddValue},
	{registry.MobEffectAbsorption, registry.AttributeMaxAbsorption, "minecraft:effect.absorption", 4, registry.AttributeAddValue},
	{registry.MobEffectLuck, registry.AttributeLuck, "minecraft:effect.luck", 1, registry.AttributeAddValue},
	{registry.MobEffectUnluck, registry.AttributeLuck, "minecraft:effect.unluck", -1, registry.AttributeAddValue},
}

// healthBoost is health boost's max_health modifier, which MaxHealthOf
// applies ahead of the attribute pass.
var healthBoost = effectModifiers[slices.IndexFunc(effectModifiers, func(em effectModifier) bool {
	return em.effect == registry.MobEffectHealthBoost
})]

type effectModifier struct {
	effect    int32
	attribute registry.AttributeID
	id        string
	perLevel  float64
	operation registry.AttributeOperation
}

// UpdateEffectAttributes applies the effects' modifiers (amount times
// amplifier+1) and removes those of ended effects.
// vanilla: MobEffect.addAttributeModifiers, removeAttributeModifiers
func UpdateEffectAttributes(a *player.Attributes, active []effect.Instance) {
	updateEffectAttributes(a, active, func(effectModifier) bool { return true })
}

// UpdateSupportedEffectAttributes applies effect modifiers to attributes in the body's supplier.
func UpdateSupportedEffectAttributes(a *player.Attributes, active []effect.Instance) {
	updateEffectAttributes(a, active, func(em effectModifier) bool { return a.Has(em.attribute) })
}

// HasEffectModifier reports whether the effect carries an attribute modifier.
func HasEffectModifier(id int32) bool {
	return slices.ContainsFunc(effectModifiers, func(em effectModifier) bool { return em.effect == id })
}

// ApplyEffectChange puts on or takes off the one modifier of the changed
// effect, where the map has its attribute.
// vanilla: MobEffect.addAttributeModifiers, removeAttributeModifiers
func ApplyEffectChange(a *player.Attributes, ch effect.Change) {
	for _, em := range effectModifiers {
		if em.effect == ch.Instance.ID && a.Has(em.attribute) {
			em.apply(a, ch.Instance, ch.Kind != effect.Removed)
		}
	}
}

// ApplyActiveEffects puts on the modifiers of the active effects, touching
// no other effect's.
func ApplyActiveEffects(a *player.Attributes, active []effect.Instance) {
	for _, in := range active {
		ApplyEffectChange(a, effect.Change{Kind: effect.Added, Instance: in})
	}
}

// UpdateEffectAttributesOf is UpdateEffectAttributes for the modifiers on one
// attribute.
func UpdateEffectAttributesOf(a *player.Attributes, active []effect.Instance, attribute registry.AttributeID) {
	updateEffectAttributes(a, active, func(em effectModifier) bool { return em.attribute == attribute })
}

func updateEffectAttributes(a *player.Attributes, active []effect.Instance, want func(effectModifier) bool) {
	for _, em := range effectModifiers {
		if !want(em) {
			continue
		}
		i := slices.IndexFunc(active, func(in effect.Instance) bool { return in.ID == em.effect })
		if i < 0 {
			em.apply(a, effect.Instance{}, false)
		} else {
			em.apply(a, active[i], true)
		}
	}
}

// apply puts the modifier on for in, or takes it off when the effect is not on.
func (em effectModifier) apply(a *player.Attributes, in effect.Instance, on bool) {
	if !on {
		a.RemoveModifier(em.attribute, em.id)
		return
	}
	a.ReplaceModifier(em.attribute, player.Modifier{
		ID: em.id, Amount: em.perLevel * float64(in.Amplifier+1), Operation: em.operation, Persistent: true,
	})
}

// ArmourValue is what the armour attribute gives the damage formula: its
// value rounded down, and the toughness as it is.
// vanilla: LivingEntity.getArmorValue, getDamageAfterArmorAbsorb
func ArmourValue(a *player.Attributes) (points, toughness float64) {
	return math.Floor(a.Value(registry.AttributeArmor)), a.Value(registry.AttributeArmorToughness)
}

// KnockbackScale is what is left of a shove after the knockback
// resistance, nothing at all from full resistance up.
// vanilla: LivingEntity.knockback
func KnockbackScale(a *player.Attributes) float64 {
	return max(0, 1-a.Value(registry.AttributeKnockbackResistance))
}
