package entity

import (
	"maps"
	"math"

	"github.com/golem-mc/golem/nbt"
)

// TrialSpawnerSettings is the numeric configuration of one trial spawner mode.
type TrialSpawnerSettings struct {
	SpawnRange                     int
	TotalMobs                      float32
	SimultaneousMobs               float32
	TotalMobsAddedPerPlayer        float32
	SimultaneousMobsAddedPerPlayer float32
	TicksBetweenSpawn              int
}

func trialSettingsKey(ominous bool) string {
	if ominous {
		return "ominous_config"
	}
	return "normal_config"
}

// ReadTrialSpawnerSettings resolves inline or registry configurations with ticker defaults.
func ReadTrialSpawnerSettings(tag nbt.Compound, ominous bool) TrialSpawnerSettings {
	return trialSettingsOf(trialConfigTag(tag, trialSettingsKey(ominous)))
}

// Valid reports whether all numeric settings fit TrialSpawnerConfig.DIRECT_CODEC.
func (s TrialSpawnerSettings) Valid() bool {
	if s.SpawnRange < 1 || s.SpawnRange > 128 || s.TicksBetweenSpawn < 0 || int64(s.TicksBetweenSpawn) > math.MaxInt32 {
		return false
	}
	for _, v := range [...]float32{s.TotalMobs, s.SimultaneousMobs, s.TotalMobsAddedPerPlayer, s.SimultaneousMobsAddedPerPlayer} {
		if v < 0 || math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}

// UpdateTrialSpawnerSettings replaces one mode's numeric fields without mutating shared tags.
// A named config becomes an inline copy; runtime data and the other mode are untouched.
func UpdateTrialSpawnerSettings(tag nbt.Compound, ominous bool, s TrialSpawnerSettings) {
	if !s.Valid() {
		return
	}
	key := trialSettingsKey(ominous)
	cfg := maps.Clone(trialConfigTag(tag, key))
	if cfg == nil {
		cfg = nbt.Compound{}
	}
	cfg["spawn_range"] = nbt.Int(s.SpawnRange)
	cfg["total_mobs"] = nbt.Float(s.TotalMobs)
	cfg["simultaneous_mobs"] = nbt.Float(s.SimultaneousMobs)
	cfg["total_mobs_added_per_player"] = nbt.Float(s.TotalMobsAddedPerPlayer)
	cfg["simultaneous_mobs_added_per_player"] = nbt.Float(s.SimultaneousMobsAddedPerPlayer)
	cfg["ticks_between_spawn"] = nbt.Int(s.TicksBetweenSpawn)
	tag[key] = cfg
}
