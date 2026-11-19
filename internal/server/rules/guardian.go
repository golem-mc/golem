package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
)

// ElderCurseApplies reports whether the elder guardian's mining fatigue is
// handed to a player: one in survival or adventure, within the radius, who
// does not already carry the effect at the same strength with more than
// displayLimit ticks left. have is the player's active instance of it, if
// any.
// vanilla: MobEffectUtil.addEffectToPlayersAround
func ElderCurseApplies(mode int32, dist2, radius float64, have *effect.Instance, amplifier, displayLimit int32) bool {
	if mode != protocol.GameModeSurvival && mode != protocol.GameModeAdventure {
		return false
	}
	if dist2 >= radius*radius {
		return false
	}
	if have != nil && have.Amplifier >= amplifier && (have.Duration == effect.Infinite || have.Duration > displayLimit-1) {
		return false
	}
	return true
}
