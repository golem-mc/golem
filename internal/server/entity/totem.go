package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// dies runs between zero health and death, where a held totem may save the
// body. It reports whether the mob died; softened marks a window-softened
// blow, whose death plays no sound.
// vanilla: LivingEntity.hurtServer
func (m *Mob) dies(damageType registry.DamageType, softened bool) bool {
	if m.dragon != nil {
		// vanilla: EnderDragon.handleKillingBlow
		if !m.dragonIsSitting() {
			m.SetMetadataHealth(1)
			m.SetMetadataDragonPhase(dragonDying)
			m.dragon.hasTarget = false
			m.queued = append(m.queued, MobEvent{Mob: m, Kind: MobKillingBlow, DamageType: damageType, Softened: softened})
		}
		return false
	}
	if m.totemSave(damageType) {
		return false
	}
	m.Dead = true
	m.deathSoftened = softened
	return true
}

// totemSave spends a held death-protection item, leaving the body at one
// health with the item's death effects. The effect changes and animation
// are sent next tick. Absorption hearts mean nothing on a mob, so
// Absorption II lands as the effect alone.
// vanilla: LivingEntity.checkTotemDeathProtection
func (m *Mob) totemSave(damageType registry.DamageType) bool {
	hand, death := rules.TotemHand(damageType, m.MainHand, m.OffHand)
	if hand < 0 {
		return false
	}
	slot := protocol.EquipMainHand
	if hand != protocol.HandMain {
		slot = protocol.EquipOffHand
	}
	held := m.EquipmentSlot(slot)
	if held.Count--; held.Count <= 0 {
		held = protocol.ItemStack{}
	}
	m.SetEquipment(slot, held)
	m.SetMetadataHealth(1)
	m.totemChanges = append(m.totemChanges, rules.ConsumeOn(m, death, m.Rand).Changes...)
	m.totemSaved = true
	return true
}

// DeathSoftened reports whether the killing blow was window-softened, which
// plays no death sound.
// vanilla: LivingEntity.hurtServer, tookFullDamage
func (m *Mob) DeathSoftened() bool { return m.deathSoftened }

// TotemSaved reports a save waiting to be told; its blow plays neither hurt
// nor death sound.
// vanilla: LivingEntity.hurtServer
func (m *Mob) TotemSaved() bool { return m.totemSaved }

// totemEvents hands the tick the saves the blows since the last one made.
func (m *Mob) totemEvents(events []MobEvent) []MobEvent {
	if !m.totemSaved {
		return events
	}
	events = append(events, MobEvent{Mob: m, Kind: MobTotemSaved})
	for _, ch := range m.totemChanges {
		events = append(events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: ch})
	}
	m.totemSaved = false
	m.totemChanges = m.totemChanges[:0]
	return events
}
