package rules

import "github.com/golem-mc/golem/nbt"

// WardenSpawnTracker is how warned a player is by the sculk shriekers around
// them: the level climbs a step per shriek, at most one every 200 ticks, and
// falls a step every 12000.
// vanilla: WardenSpawnTracker
type WardenSpawnTracker struct {
	TicksSinceLastWarning int
	WarningLevel          int
	CooldownTicks         int
}

// Tag is the tracker as the player's warden_spawn_tracker compound.
// vanilla: WardenSpawnTracker.CODEC
func (t WardenSpawnTracker) Tag() nbt.Compound {
	return nbt.Compound{
		"ticks_since_last_warning": nbt.Int(t.TicksSinceLastWarning),
		"warning_level":            nbt.Int(t.WarningLevel),
		"cooldown_ticks":           nbt.Int(t.CooldownTicks),
	}
}

// WardenSpawnTrackerFromTag reads a tracker, defaulting an invalid record to zero.
// vanilla: WardenSpawnTracker.CODEC, ServerPlayer.readAdditionalSaveData
func WardenSpawnTrackerFromTag(tag nbt.Compound) WardenSpawnTracker {
	var t WardenSpawnTracker
	for _, field := range []struct {
		name string
		into *int
	}{
		{"ticks_since_last_warning", &t.TicksSinceLastWarning},
		{"warning_level", &t.WarningLevel},
		{"cooldown_ticks", &t.CooldownTicks},
	} {
		value, present := tag[field.name]
		if !present {
			continue
		}
		n, ok := nbt.IntValue(value)
		if !ok || n < 0 {
			// The optional fields are strict: one bad value rejects the record.
			return WardenSpawnTracker{}
		}
		*field.into = int(n)
	}
	return t
}

// vanilla: WardenSpawnTracker.MAX_WARNING_LEVEL, DECREASE_WARNING_LEVEL_EVERY_INTERVAL,
// WARNING_LEVEL_INCREASE_COOLDOWN
const (
	MaxWardenWarningLevel   = 4
	wardenWarningDecayTicks = 12000
	wardenWarningCooldown   = 200
)

// Tick runs one tick of the tracker's timers.
func (t *WardenSpawnTracker) Tick() {
	if t.TicksSinceLastWarning >= wardenWarningDecayTicks {
		t.WarningLevel = max(t.WarningLevel-1, 0)
		t.TicksSinceLastWarning = 0
	} else {
		t.TicksSinceLastWarning++
	}
	if t.CooldownTicks > 0 {
		t.CooldownTicks--
	}
}

// WardenWarn raises the warning of the players near a shrieker (the
// triggering player among them) and returns the new level; false when one of
// them is on cooldown. Every tracker ends up with the highest one's data.
// vanilla: WardenSpawnTracker.tryWarn
func WardenWarn(trackers []*WardenSpawnTracker) (int, bool) {
	if len(trackers) == 0 {
		return 0, false
	}
	highest := trackers[0]
	for _, t := range trackers {
		if t.CooldownTicks > 0 {
			return 0, false
		}
		if t.WarningLevel > highest.WarningLevel {
			highest = t
		}
	}
	highest.TicksSinceLastWarning = 0
	highest.CooldownTicks = wardenWarningCooldown
	highest.WarningLevel = min(highest.WarningLevel+1, MaxWardenWarningLevel)
	for _, t := range trackers {
		*t = *highest
	}
	return highest.WarningLevel, true
}
