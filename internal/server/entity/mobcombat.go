package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// A hurt reaches the combat tracker in steps: loseHealth knows its damage,
// fall distance and location, noteLastDamage then gives its type, and the
// caller sets blowAttacker after that. So the hurt waits in m.hurt and is
// recorded when the next hurt starts or the death is worded, while
// blowAttacker still names it. It keeps the tick it landed at, so a late
// record does not hold it in the tracker's window.
// vanilla: LivingEntity.actuallyHurt, CombatTracker.recordDamage

// startHurt opens the record of a hurt taking dmg (> 0) off the health.
func (m *Mob) startHurt(dmg float64) {
	m.recordHurt()
	m.hurt = rules.CombatEntry{Damage: dmg, FallDistance: m.FallDistance, Location: m.fallLocation(), Tick: m.Age}
}

// noteHurtType names the open hurt's type. A typed one is an earlier hurt,
// recorded before blowAttacker moves on to this one.
func (m *Mob) noteHurtType(damageType registry.DamageType) {
	if m.hurt.DamageType != 0 {
		m.recordHurt()
	}
	if m.hurt.Damage > 0 {
		m.hurt.DamageType = damageType
	}
}

// noteFallHurt is noteLastDamage for a landing of landed blocks; the fall is
// banked off before the hurt lands.
func (m *Mob) noteFallHurt(damageType registry.DamageType, landed float64) {
	m.noteLastDamage(damageType)
	if m.hurt.Damage > 0 {
		m.hurt.FallDistance = landed
	}
}

// recordHurt moves the open hurt into the tracker. The attacker is named
// only when the death is worded (CombatDeathMessage).
func (m *Mob) recordHurt() {
	e := m.hurt
	if e.Damage <= 0 {
		return
	}
	m.hurt = rules.CombatEntry{}
	if e.DamageType == 0 {
		e.DamageType = m.lastDamageType
	}
	if m.blowAttacker != 0 {
		e.AttackerID = m.blowAttacker + 1
		e.Attacker = &rules.Killer{}
	}
	m.combat.Record(e.Tick, e)
}

// fallLocation is where the body is hurt for a fall message: the block it
// last climbed, else water, else "". The climbed block is read when climbed;
// vanilla reads it when hurt.
// vanilla: FallLocation.getCurrentFallLocation
func (m *Mob) fallLocation() rules.FallLocation {
	if m.climbedFrom != "" {
		return m.climbedFrom
	}
	if m.InWater() {
		return rules.FallLocationWater
	}
	return ""
}

// CombatDeathMessage words the body's death from its combat tracker. name is
// the attacker an entity ID is, nil once it is gone; credit is the player
// credited with the kill, nil for none.
// vanilla: CombatTracker.getDeathMessage
func (m *Mob) CombatDeathMessage(victim protocol.Text, name func(entityID int32) *rules.Killer, credit *rules.Killer) protocol.Text {
	m.recordHurt()
	byID := func(attackerID int32) *rules.Killer { return name(attackerID - 1) }
	named := m.combat.WithAttackers(byID)
	return named.DeathMessageAlive(victim, m.Age, credit, m.PlayerKillCredit(),
		func(attackerID int32) bool { return byID(attackerID) != nil })
}
