package registry

import "slices"

// MobEffect is what vanilla's MobEffects registers a status effect with that
// the data report leaves out: the particle colour, whether the effect is
// instantaneous (InstantaneousMobEffect), and for an effect whose particle
// is not the coloured entity_effect one, that payload-free particle type.
type MobEffect struct {
	Color    int32
	Instant  bool
	Particle string
}

var mobEffectIDs = func() map[string]int32 {
	ids := make(map[string]int32, len(MobEffects))
	for id, name := range MobEffects {
		ids[name] = int32(id)
	}
	return ids
}()

// MobEffectID returns the minecraft:mob_effect ID of an effect name.
func MobEffectID(name string) (int32, bool) {
	id, ok := mobEffectIDs[Qualify(name)]
	return id, ok
}

// MustMobEffect is MobEffectID for names this package is generated with.
func MustMobEffect(name string) int32 {
	id, ok := MobEffectID(name)
	if !ok {
		panic("unknown mob effect " + name)
	}
	return id
}

// MobEffectName returns the name of a mob effect ID, "" for none.
func MobEffectName(id int32) string {
	if id < 0 || int(id) >= len(MobEffects) {
		return ""
	}
	return MobEffects[id]
}

// MobEffectOf returns the registration of a mob effect ID.
func MobEffectOf(id int32) MobEffect {
	return mobEffects[MobEffectName(id)]
}

// BeaconOffersEffect reports whether the pyramid offers the mob effect at
// one of its levels, the seven BEACON_EFFECTS and no other.
// vanilla: BeaconBlockEntity.VALID_EFFECTS
func BeaconOffersEffect(effect int32) bool {
	levels := BeaconEffectLevels(effect)
	return levels >= 1 && levels <= len(beaconEffects)
}

// BeaconEffectLevels is the least pyramid level at which a beacon may hold
// the mob effect: zero for the -1 of an unmade choice, one to four for one
// of the effects the pyramid offers, and one past the top for an effect no
// beacon offers. vanilla: BeaconBlockEntity.getRequiredLevelsFor
func BeaconEffectLevels(effect int32) int {
	if effect < 0 {
		return 0
	}
	for level, effects := range beaconEffects {
		if slices.Contains(effects, effect) {
			return level + 1
		}
	}
	return len(beaconEffects) + 1
}
