package rules

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The totem of undying per death_protection: which hand saves a body at no
// health, and what the item then does. Spending it and telling the world
// are the server's.

// TotemHand is the first hand (main, then off) holding a death_protection
// stack, with its death effects; -1 for none. bypasses_invulnerability
// blows (/kill, the void) are beyond any totem.
// vanilla: LivingEntity.checkTotemDeathProtection
func TotemHand(damageType registry.DamageType, mainhand, offhand protocol.ItemStack) (int32, []registry.ConsumeEffect) {
	if damageType.InTag(registry.DamageTypeTagIDBypassesInvulnerability) {
		return -1, nil
	}
	if !mainhand.Empty() {
		if death, ok := registry.StackDeathProtection(mainhand); ok {
			return protocol.HandMain, death
		}
	}
	if !offhand.Empty() {
		if death, ok := registry.StackDeathProtection(offhand); ok {
			return protocol.HandOff, death
		}
	}
	return -1, nil
}

// TotemSave sets health to 1 (setHealth(1.0F)) and runs the item's death
// effects (the totem clears all effects and gives three); the caller tells
// the changes.
// vanilla: LivingEntity.checkTotemDeathProtection, DeathProtection.applyEffects
func TotemSave(v *player.Vitals, effects *effect.Set, a *player.Attributes, death []registry.ConsumeEffect, rng *rand.Rand) []effect.Change {
	v.Dead = false
	v.Health = 1
	return Consume(v, effects, a, death, rng).Changes
}
