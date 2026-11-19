package registry

import (
	"strings"
	"sync/atomic"
)

// DamageScaling is a damage type's scaling field: whether its damage grows
// with the difficulty.
type DamageScaling uint8

// vanilla: DamageScaling
const (
	DamageScalesNever DamageScaling = iota
	DamageScalesWhenCausedByLivingNonPlayer
	DamageScalesAlways
)

// DeathMessageType is a damage type's death_message_type field.
type DeathMessageType uint8

// vanilla: DeathMessageType
const (
	DeathMessageDefault DeathMessageType = iota
	DeathMessageFallVariants
	DeathMessageIntentionalGameDesign
)

// DamageEffects is a damage type's effects field, which picks a player's hurt
// sound; absent is DamageEffectsHurt.
type DamageEffects uint8

// vanilla: DamageEffects
const (
	DamageEffectsHurt DamageEffects = iota
	DamageEffectsThorns
	DamageEffectsDrowning
	DamageEffectsBurning
	DamageEffectsPoking
	DamageEffectsFreezing
)

// PlayerHurtSound is the sound event a player hurt with these effects makes.
// vanilla: DamageEffects
func (e DamageEffects) PlayerHurtSound() Sound {
	switch e {
	case DamageEffectsDrowning:
		return SoundEntityPlayerHurtDrown
	case DamageEffectsBurning:
		return SoundEntityPlayerHurtOnFire
	case DamageEffectsPoking:
		return SoundEntityPlayerHurtSweetBerryBush
	case DamageEffectsFreezing:
		return SoundEntityPlayerHurtFreeze
	default:
		return SoundEntityPlayerHurt
	}
}

// DamageTypeInfo holds the fields of a damage type the server acts on.
// vanilla: DamageType
type DamageTypeInfo struct {
	MessageID    string
	Scaling      DamageScaling
	Exhaustion   float64
	DeathMessage DeathMessageType
	Effects      DamageEffects
}

// packDamageTypes is the data packs' damage types over the built-in table.
var packDamageTypes atomic.Pointer[map[string]DamageTypeInfo]

// ApplyPackDamageTypes makes the data packs' damage types, by registry name,
// override and extend DamageTypes for LookupDamageType; nil removes them.
func ApplyPackDamageTypes(types map[string]DamageTypeInfo) {
	if len(types) == 0 {
		packDamageTypes.Store(nil)
		damageTypesNow.Store(newDamageTypeState())
		return
	}
	packDamageTypes.Store(&types)
	damageTypesNow.Store(newDamageTypeState())
}

// LookupDamageType returns a damage type's fields. A name in neither the table
// nor a pack gets its path as message_id, no scaling and no exhaustion.
func LookupDamageType(name string) DamageTypeInfo {
	if d, ok := DamageTypeByName(name); ok {
		return d.Info()
	}
	_, id, _ := strings.Cut(name, ":")
	return DamageTypeInfo{MessageID: id}
}

var damageTypesNow = func() *atomic.Pointer[damageTypeState] {
	p := new(atomic.Pointer[damageTypeState])
	p.Store(newDamageTypeState())
	return p
}()
