package entity

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Bits of a located event's Level above the EnchEquipment piece bits (one per
// piece whose stack changed).
const (
	// LocatedMoved: the body's cell changed or it landed, so every piece's
	// location_changed entries run.
	LocatedMoved int32 = 1 << 6
	// LocatedLanded: the run is a landing's, which does not repeat the tick entries.
	LocatedLanded int32 = 1 << 7
	// LocatedTick: the tick entries run too.
	LocatedTick int32 = 1 << 8
)

// ApplyLocatedMods puts the attribute modifiers of the entries that became
// active on the map and takes off those of the entries that stopped.
// vanilla: AttributeEffect.onChangedBlock, onDeactivated
func ApplyLocatedMods(a *player.Attributes, apply, remove []rules.LocatedAction) {
	for _, act := range remove {
		if m, ok := act.LocatedAttributeModifier(); ok {
			a.RemoveModifier(m.Attribute, m.ID)
		}
	}
	for _, act := range apply {
		if m, ok := act.LocatedAttributeModifier(); ok && act.Became && a.Has(m.Attribute) {
			a.RemoveModifier(m.Attribute, m.ID)
			a.AddModifier(m.Attribute, player.Modifier{ID: m.ID, Amount: m.Amount, Operation: m.Operation})
		}
	}
}

// Located is the set of location_changed entries active on the mob.
func (m *Mob) Located() *rules.LocatedState { return &m.loc }

// packLocatedStep reports a mob wearing an enchanted stack, so the server can
// run its pack location_changed and tick entries; the event's Level holds a
// bit per EnchEquipment piece whose stack changed, and LocatedMoved for a new
// cell. Every mob passes here each tick, so the equipment is scanned
// only when its revision moved.
// vanilla: LivingEntity.baseTick, collectEquipmentChanges, EnchantmentHelper.runLocationChangedEffects
func (m *Mob) packLocatedStep(events []MobEvent) []MobEvent {
	revisionMoved := !m.locChecked || m.locRevision != m.equipmentRevision
	var mask int32
	if revisionMoved {
		for i, p := range enchEquipment(m.Worn, m.MainHand, m.OffHand) {
			if !p.Stack.Equal(m.locPieces[i]) {
				mask |= 1 << i
			}
			m.locPieces[i] = p.Stack
		}
		m.locChecked, m.locRevision = true, m.equipmentRevision
	}
	// A reload can add a located entry to a definition.
	if gen := registry.EnchantmentGeneration(); revisionMoved || gen != m.locGen {
		m.locGen = gen
		m.locEnchanted = rules.HasLocated(m.MainHand) || rules.HasLocated(m.OffHand) ||
			slices.ContainsFunc(m.Worn[:], rules.HasLocated)
	}
	if !m.locEnchanted && m.loc.Empty() {
		m.locSeen = false
		return events
	}
	cell := [3]int32{rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)}
	if !m.locSeen || m.locCell != cell {
		mask |= LocatedMoved
	}
	m.locCell, m.locSeen = cell, true
	return append(events, MobEvent{Mob: m, Kind: MobPackLocated, X: m.X, Y: m.Y, Z: m.Z, Level: mask})
}

// locatedLanding reports a landing for the pack location_changed entries,
// ahead of the fall being spent. It is separate from packLocatedStep because
// baseTick runs before travel banks the landing.
// vanilla: LivingEntity.checkFallDamage
func (m *Mob) locatedLanding(events []MobEvent, fell float64) []MobEvent {
	if fell <= 0 || !m.locEnchanted && m.loc.Empty() {
		return events
	}
	return append(events, MobEvent{Mob: m, Kind: MobPackLocated, X: m.X, Y: m.Y, Z: m.Z, Level: LocatedMoved | LocatedLanded})
}
