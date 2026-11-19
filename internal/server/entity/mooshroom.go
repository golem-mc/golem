package entity

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// FeedMooshroomFlower reports whether the flower interaction was handled and consumed.
// vanilla: MushroomCow.mobInteract, getEffectsFromItemStack
func (m *Mob) FeedMooshroomFlower(item registry.Item) (handled, consumed bool) {
	if m.Type != TypeMooshroom || m.Mushroom != "brown" || m.IsBaby() || !m.Alive() {
		return false, false
	}
	// The other three ingredients are common to every stew recipe.
	if item == registry.ItemBowl || item == registry.ItemRedMushroom || item == registry.ItemBrownMushroom {
		return false, false
	}
	for _, recipe := range registry.StewRecipes {
		for _, ingredient := range recipe.Ingredients {
			if !slices.Contains(ingredient, int32(item)) {
				continue
			}
			if m.StewEffects != nil {
				return true, false
			}
			m.StewEffects = []protocol.StewEffect{{Effect: registry.MustMobEffect(recipe.Effect), Duration: recipe.Duration}}
			return true, true
		}
	}
	return false, false
}

// vanilla: SuspiciousStewEffects.CODEC, Entry.CODEC
func readMooshroomStewEffects(compound nbt.Compound) []protocol.StewEffect {
	if list, ok := nbt.Get[nbt.List](compound, "stew_effects"); ok {
		effects := make([]protocol.StewEffect, 0, len(list.Items))
		for _, entry := range list.Items {
			tag, ok := nbt.CompoundView(entry)
			if !ok {
				continue
			}
			name, _ := nbt.Get[nbt.String](tag, "id")
			id, ok := registry.MobEffectID(registry.Qualify(string(name)))
			if !ok {
				continue
			}
			duration, ok := nbt.IntValue(tag["duration"])
			if !ok {
				duration = 160 // vanilla: SuspiciousStewEffects.DEFAULT_DURATION
			}
			effects = append(effects, protocol.StewEffect{Effect: id, Duration: duration})
		}
		return effects
	}
	return nil
}
