package rules

import "time"

// moonBrightness is DimensionType.MOON_BRIGHTNESS_PER_PHASE, by moon phase.
var moonBrightness = [8]float32{1.0, 0.75, 0.5, 0.25, 0.0, 0.25, 0.5, 0.75}

// MoonBrightness is the brightness of the moon of the phase given, zero
// through seven.
func MoonBrightness(phase int) float32 { return moonBrightness[phase&7] }

// EffectiveDifficulty is DifficultyInstance's effective difficulty: the
// difficulty's ID scaled up by the overworld clock's ticks, the ticks
// players have spent in the chunk and the moon's brightness.
// vanilla: DifficultyInstance.calculateDifficulty
func EffectiveDifficulty(difficulty uint8, clockTime, inhabitedTime int64, moon float32) float32 {
	if difficulty == DifficultyPeaceful {
		return 0
	}
	scale := float32(0.75)
	global := clamp32((float32(clockTime)-72000)/1440000, 0, 1) * 0.25
	scale += global
	localShare := float32(0.75)
	if difficulty == DifficultyHard {
		localShare = 1
	}
	local := clamp32(float32(inhabitedTime)/3600000, 0, 1) * localShare
	local += clamp32(moon*0.25, 0, global)
	if difficulty == DifficultyEasy {
		local *= 0.5
	}
	return float32(difficulty) * (scale + local)
}

// SpecialMultiplier is DifficultyInstance.getSpecialMultiplier: nought
// below an effective difficulty of two, one above four, and the share
// between.
func SpecialMultiplier(effective float32) float32 {
	return clamp32((effective-2)/2, 0, 1)
}

// LocalDifficulty is the DifficultyInstance the rolls a spawn makes against
// its place read: the special multiplier of the effective difficulty there,
// and whether the level's own difficulty is hard.
type LocalDifficulty struct {
	Special float32
	Hard    bool

	// Halloween is whether the spawn stands up on vanilla's Halloween, the
	// date the zombie and skeleton families wear a pumpkin on.
	Halloween bool

	// Moon is the overworld moon's brightness, which the cat's all_black
	// spawn condition reads.
	Moon float32
}

// IsHalloween is SpecialDates.isHalloween for the date given: October 31
// alone, not the longer window older versions keep.
// vanilla: SpecialDates.HALLOWEEN
func IsHalloween(t time.Time) bool {
	return t.Month() == time.October && t.Day() == 31
}

// NewLocalDifficulty is the DifficultyInstance at the effective difficulty
// given, for a level of the difficulty given.
func NewLocalDifficulty(level uint8, effective float32) LocalDifficulty {
	return LocalDifficulty{Special: SpecialMultiplier(effective), Hard: level == DifficultyHard}
}

func clamp32(v, lo, hi float32) float32 {
	return min(max(v, lo), hi)
}
