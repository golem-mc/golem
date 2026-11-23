package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ArmorStand.createAttributes
var armorStandAttributeSupplier = &player.Supplier{
	Names: mannequinAttributeSupplier.Names,
	Bases: map[string]float64{"minecraft:step_height": 0},
}

// Attributes is the stand's living attribute map.
func (s *ArmorStand) Attributes() *player.Attributes { return s.attrs }

// HealthCap is the stand's effective maximum health.
func (s *ArmorStand) HealthCap() float64 { return s.attrs.Value(registry.AttributeMaxHealth) }

// OnAttributeUpdated refreshes health and reports whether the stand resized.
// vanilla: LivingEntity.onAttributeUpdated, ArmorStand.refreshDimensions
func (s *ArmorStand) OnAttributeUpdated() bool {
	s.Health = min(s.Health, s.HealthCap())
	scale := float64(float32(s.attrs.Value(registry.AttributeScale)))
	changed := scale != s.scale
	s.scale = scale
	return changed
}

// AddEffect gives a stand a command effect through the living immunity rules.
func (s *ArmorStand) AddEffect(in effect.Instance) (effect.Change, bool) {
	if !rules.CanBeAffected(protocol.EntityTypeArmorStand, in.ID) {
		return effect.Change{}, false
	}
	return s.LivingEffects.AddEffect(in)
}

// SyncEffectAttributes applies effect modifiers and the resulting health cap.
func (s *ArmorStand) SyncEffectAttributes() {
	rules.UpdateSupportedEffectAttributes(s.attrs, s.AllEffects())
	s.Health = min(s.Health, s.HealthCap())
}

// vanilla: LivingEntity.tickEffects, LivingEntity.aiStep
func (s *ArmorStand) tickEffects() {
	s.age++
	if s.age == 1 {
		// LivingEntity.removeFrost instantiates movement_speed on the first tick.
		s.attrs.Instance(registry.AttributeMovementSpeed)
	}
	changes := s.effects.Tick(s.age, func(in *effect.Instance) bool {
		return applyLivingEffect(in, protocol.EntityTypeArmorStand, s.Health, s.HealthCap(), s.Absorption, func(healing float64) {
			s.Health = min(s.Health+healing, s.HealthCap())
		}, func(float64, registry.DamageType) {
			// ArmorStand.hurtServer refuses the magic and wither damage types.
		})
	})
	if len(changes) != 0 {
		s.clampAbsorption()
		s.SyncEffectAttributes()
	}
}
