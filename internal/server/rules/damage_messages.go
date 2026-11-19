package rules

import "github.com/golem-mc/golem/registry"

// deathKey is the translation key of a damage type's death message from its
// message_id.
// vanilla: DamageSource.getLocalizedDeathMessage
func deathKey(damageType registry.DamageType) string {
	return "death.attack." + damageType.Info().MessageID
}

// DamageScales reports whether a damage type's damage scales with the
// difficulty: always, or when a living non-player is behind it.
// vanilla: DamageSource.scalesWithDifficulty
func DamageScales(damageType registry.DamageType, causedByLivingNonPlayer bool) bool {
	switch damageType.Info().Scaling {
	case registry.DamageScalesAlways:
		return true
	case registry.DamageScalesWhenCausedByLivingNonPlayer:
		return causedByLivingNonPlayer
	}
	return false
}
