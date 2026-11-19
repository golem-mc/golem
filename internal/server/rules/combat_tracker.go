package rules

import (
	"reflect"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Ticks a CombatTracker keeps its entries without a new hit: 100 outside
// combat, 300 once a living attacker started one.
// vanilla: CombatTracker.RESET_DAMAGE_STATUS_TIME, RESET_COMBAT_STATUS_TIME
const (
	resetDamageTicks = 100
	resetCombatTicks = 300
)

// fallSignificantDistance is the fall (blocks) or damage a hit must pass to
// decide the death message of a fall.
// vanilla: CombatTracker.getMostSignificantFall
const fallSignificantDistance = 5

// killCreditTicks is how long an attacker keeps the kill credit after its
// last blow. vanilla: LivingEntity.resolvePlayerResponsibleForDamage, baseTick
const killCreditTicks = 100

// FallLocation is where a body was when it was hurt, the id of the vanilla
// FallLocation; the empty string is none.
type FallLocation string

// The fall locations. vanilla: FallLocation
const (
	FallLocationGeneric       FallLocation = "generic"
	FallLocationLadder        FallLocation = "ladder"
	FallLocationVines         FallLocation = "vines"
	FallLocationWeepingVines  FallLocation = "weeping_vines"
	FallLocationTwistingVines FallLocation = "twisting_vines"
	FallLocationScaffolding   FallLocation = "scaffolding"
	FallLocationOtherClimb    FallLocation = "other_climbable"
	FallLocationWater         FallLocation = "water"
)

// BlockFallLocation is the fall location of the block a body last climbed.
// vanilla: FallLocation.blockToFallLocation
func BlockFallLocation(state uint16) FallLocation {
	block := registry.BlockOf(state)
	switch block {
	case registry.BlockLadder:
		return FallLocationLadder
	case registry.BlockVine:
		return FallLocationVines
	case registry.BlockWeepingVines, registry.BlockWeepingVinesPlant:
		return FallLocationWeepingVines
	case registry.BlockTwistingVines, registry.BlockTwistingVinesPlant:
		return FallLocationTwistingVines
	case registry.BlockScaffolding:
		return FallLocationScaffolding
	}
	if block.InGroup(registry.GroupTrapdoor) {
		return FallLocationLadder
	}
	return FallLocationOtherClimb
}

// CombatEntry is one hurt the tracker remembers.
type CombatEntry struct {
	DamageType registry.DamageType
	// Attacker is who stood behind the blow, nil for none.
	Attacker *Killer
	// DirectOnly marks an Attacker that is only the projectile or explosive
	// that carried the blow, with no living causing entity behind it: it words
	// the death but is neither combat, kill credit nor a knock-off.
	// vanilla: DamageSource.getEntity, getDirectEntity
	DirectOnly bool
	// AttackerID is the entity ID behind the blow plus one, zero for none.
	AttackerID   int32
	Damage       float64
	FallDistance float64
	Location     FallLocation
	// Tick is the body's tick the hurt was recorded at.
	Tick int
}

// CombatTracker is the recent hurts of one body, which word its death.
// vanilla: CombatTracker
type CombatTracker struct {
	entries        []CombatEntry
	lastDamageTick int
	inCombat       bool
	takingDamage   bool
}

// Record adds a hurt taken at the body's tick. A death clears the tracker
// (Clear), so no hurt finds a dead body's entries.
// vanilla: CombatTracker.recordDamage
func (t *CombatTracker) Record(tick int, entry CombatEntry) {
	t.recheck(tick)
	entry.Tick = tick
	t.entries = append(t.entries, entry)
	t.lastDamageTick = tick
	t.takingDamage = true
	if !t.inCombat && entry.Attacker != nil && !entry.DirectOnly {
		t.inCombat = true
	}
}

// Clear forgets every entry, as a death does.
func (t *CombatTracker) Clear() {
	*t = CombatTracker{}
}

// WithAttackers is a copy of the tracker whose entries' attackers are
// name(AttackerID), nil for one that is gone.
func (t *CombatTracker) WithAttackers(name func(attackerID int32) *Killer) CombatTracker {
	c := *t
	c.entries = slices.Clone(t.entries)
	for i := range c.entries {
		if id := c.entries[i].AttackerID; id != 0 {
			c.entries[i].Attacker = name(id)
		}
	}
	return c
}

// Attackers are the attacker IDs (entity ID plus one) of the remembered
// hurts, which KillCredit may ask alive about. A tracker whose window has
// passed at tick remembers nothing.
func (t *CombatTracker) Attackers(tick int) []int32 {
	t.recheck(tick)
	var ids []int32
	for _, v := range t.entries {
		if id := v.AttackerID; id != 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// KillCredit is the entry whose attacker is credited at tick, nil for none.
// remembered reports that the body remembers a player who hurt it; player is
// that player when they can be credited, as an entry with no AttackerID, and
// nil credits nobody, never a mob. Without a remembered player it is the
// latest angering attacker if it is still within the memory. alive reports
// whether an attacker (entity ID plus one) is still there; a nil alive keeps
// all.
// vanilla: LivingEntity.getKillCredit
func (t *CombatTracker) KillCredit(tick int, player *Killer, remembered bool, alive func(attackerID int32) bool) *CombatEntry {
	t.recheck(tick)
	credit := func(e *CombatEntry) *CombatEntry {
		if tick-e.Tick <= killCreditTicks && (alive == nil || alive(e.AttackerID)) {
			return e
		}
		return nil
	}
	if player != nil {
		return &CombatEntry{Attacker: player}
	}
	if remembered {
		return nil
	}
	// vanilla: LivingEntity.resolveMobResponsibleForDamage keeps one slot,
	// so an older mob never stands in for a gone or expired latest one.
	for i := len(t.entries) - 1; i >= 0; i-- {
		if e := &t.entries[i]; e.Attacker != nil && !e.DirectOnly && !e.DamageType.InTag(registry.DamageTypeTagIDNoAnger) {
			return credit(e)
		}
	}
	return nil
}

func (t *CombatTracker) recheck(tick int) {
	reset := resetDamageTicks
	if t.inCombat {
		reset = resetCombatTicks
	}
	if t.takingDamage && tick-t.lastDamageTick > reset {
		t.Clear()
	}
}

// isFallVariantType is a damage type whose death_message_type is the fall
// variants.
// vanilla: DamageType.deathMessageType
func isFallVariantType(damageType registry.DamageType) bool {
	return damageType.Info().DeathMessage == registry.DeathMessageFallVariants
}

// isFallTagType is a damage type in the is_fall tag.
func isFallTagType(damageType registry.DamageType) bool {
	return isFallVariantType(damageType) || damageType == DamageTypeStalagmite
}

// mostSignificantFall is the entry a fall message is worded by, nil for none.
// vanilla: CombatTracker.getMostSignificantFall
func (t *CombatTracker) mostSignificantFall() *CombatEntry {
	var result, alternative *CombatEntry
	var altDamage, bestFall float64
	for i := range t.entries {
		entry := &t.entries[i]
		if isFallTagType(entry.DamageType) && entry.FallDistance > 0 &&
			(result == nil || entry.FallDistance > bestFall) {
			result = entry
			if i > 0 {
				result = &t.entries[i-1]
			}
			bestFall = entry.FallDistance
		}
		if entry.Location == "" || alternative != nil && entry.Damage <= altDamage {
			continue
		}
		alternative = entry
		altDamage = entry.Damage
	}
	if bestFall > fallSignificantDistance && result != nil {
		return result
	}
	if altDamage > fallSignificantDistance && alternative != nil {
		return alternative
	}
	return nil
}

// DeathMessage words the death of the body whose last recorded hurt killed
// it. victim is the body's display name.
// vanilla: CombatTracker.getDeathMessage
func (t *CombatTracker) DeathMessage(victim protocol.Text) protocol.Text {
	tick := 0
	if len(t.entries) > 0 {
		tick = t.entries[len(t.entries)-1].Tick
	}
	return t.DeathMessageAlive(victim, tick, nil, false, nil)
}

// DeathMessageAlive is DeathMessage at tick whose kill credit is KillCredit's.
func (t *CombatTracker) DeathMessageAlive(victim protocol.Text, tick int, player *Killer, remembered bool, alive func(attackerID int32) bool) protocol.Text {
	if len(t.entries) == 0 {
		return protocol.Text{Translate: "death.attack.generic", With: []protocol.Text{victim}}
	}
	last := t.entries[len(t.entries)-1]
	if isFallVariantType(last.DamageType) {
		if knockOff := t.mostSignificantFall(); knockOff != nil {
			killer := last.Attacker
			if last.DirectOnly {
				killer = nil
			}
			return fallMessage(*knockOff, killer, victim)
		}
	}
	if last.Attacker == nil && last.DamageType.Info().DeathMessage != registry.DeathMessageIntentionalGameDesign {
		// A blow with nobody behind it names the kill credit:
		// "<message>.player".
		// vanilla: DamageSource.getLocalizedDeathMessage
		if credit := t.KillCredit(tick, player, remembered, alive); credit != nil {
			return KillCreditMessage(last.DamageType, victim, credit.Attacker)
		}
	}
	if isFallVariantType(last.DamageType) {
		// No knock-off entry: the plain "death.attack.fall".
		// vanilla: CombatTracker.getDeathMessage
		return namedDeath("death.attack.fall", victim, last.Attacker)
	}
	return DeathMessage(last.DamageType, victim, last.Attacker)
}

func fallMessage(knockOff CombatEntry, killer *Killer, victim protocol.Text) protocol.Text {
	if isFallTagType(knockOff.DamageType) {
		location := knockOff.Location
		if location == "" {
			location = FallLocationGeneric
		}
		return protocol.Text{Translate: "death.fell.accident." + string(location), With: []protocol.Text{victim}}
	}
	attacker := knockOff.Attacker
	if knockOff.DirectOnly {
		attacker = nil
	}
	if attacker != nil && (killer == nil || !reflect.DeepEqual(attacker.Name, killer.Name)) {
		return assistedFall(victim, attacker, "death.fell.assist")
	}
	if killer != nil {
		return assistedFall(victim, killer, "death.fell.finish")
	}
	return protocol.Text{Translate: "death.fell.killer", With: []protocol.Text{victim}}
}

func assistedFall(victim protocol.Text, by *Killer, key string) protocol.Text {
	if item, ok := by.weaponText(); ok {
		return protocol.Text{Translate: key + ".item", With: []protocol.Text{victim, by.Name, item}}
	}
	return protocol.Text{Translate: key, With: []protocol.Text{victim, by.Name}}
}
