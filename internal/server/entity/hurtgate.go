package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// HurtGate lets the server veto or edit a hurt before it lands. Ask runs with
// every lock the caller held released and may take the mob's own, so the mob
// may have changed when it returns.
type HurtGate interface {
	// Listening reports whether anything wants the hurt, so an unobserved one
	// pays only this test.
	Listening() bool
	Ask(*HurtProbe)
}

// BarterGate lets the server change or refuse what a piglin throws for a
// gold ingot. Ask runs with every lock the caller held released.
type BarterGate interface {
	Listening() bool
	// Ask returns what to throw; nil throws nothing.
	Ask(m *Mob, input protocol.ItemStack, outcome []protocol.ItemStack) []protocol.ItemStack
}

// HurtProbe is one hurt put to the gate.
type HurtProbe struct {
	Mob *Mob
	// Type is the damage type; CauseID the causing entity's ID (0 for none),
	// Direct the direct entity's identity (nil for the causing one).
	Type    registry.DamageType
	CauseID int32
	Direct  uuid.UUID
	// Amount is what the hurt window lets through; the gate may edit it.
	Amount float64
	// Final is the health the hurt would take: armour, Resistance, protection
	// and absorption previewed without wear.
	Final float64
	// Cancel is set by the gate to refuse the hurt.
	Cancel bool
}

// hurtContext is what the entry point of a hurt knows that throughWindow's
// parameters do not; throughWindow consumes it.
type hurtContext struct {
	causeID int32
	// direct is HurtProbe.Direct.
	direct        uuid.UUID
	effectiveness float64
}

// previewHurt is the health a blow of amount would take, without wearing
// armour or spending absorption.
// vanilla: LivingEntity.actuallyHurt
func (m *Mob) previewHurt(amount float64, damageType registry.DamageType, effectiveness float64) float64 {
	if m.Type == TypeWolf && m.WearsWolfArmor() && !damageType.InTag(registry.DamageTypeTagIDBypassesWolfArmor) {
		return 0
	}
	if !rules.ArmourBypassed(damageType) {
		amount = m.armouredBy(amount, effectiveness)
	}
	return max(m.afterResistance(amount, damageType)-m.Absorption, 0)
}

// askHurt puts a hurt of amount to the gate and returns the amount to land,
// false when the gate refused it or the mob died meanwhile.
func (m *Mob) askHurt(amount float64, damageType registry.DamageType, ctx hurtContext) (float64, bool) {
	p := &HurtProbe{Mob: m, Type: damageType, CauseID: ctx.causeID, Direct: ctx.direct, Amount: amount,
		Final: m.previewHurt(amount, damageType, ctx.effectiveness)}
	m.Gate.Ask(p)
	if p.Cancel || p.Amount <= 0 || m.Dead {
		return 0, false
	}
	return p.Amount, true
}
