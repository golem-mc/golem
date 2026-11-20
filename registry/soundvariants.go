package registry

import (
	"sync/atomic"

	"github.com/golem-mc/golem/nbt"
)

// SoundVariantSet is one mob sound set of a *_sound_variant entry; a field a
// registry's entries do not have stays SoundNone.
// vanilla: WolfSoundVariant.WolfSoundSet, CatSoundVariant.CatSoundSet, PigSoundVariant.PigSoundSet, ChickenSoundVariant.ChickenSoundSet, CowSoundVariant
type SoundVariantSet struct {
	Ambient, Hurt, Death, Eat, Step               Sound
	Growl, Whine, Pant                            Sound
	BegForFood, Hiss, Purr, Purreow, StrayAmbient Sound
}

// SoundVariantRegistries are the registries SoundVariantOf serves.
var SoundVariantRegistries = []string{
	"minecraft:cat_sound_variant",
	"minecraft:chicken_sound_variant",
	"minecraft:cow_sound_variant",
	"minecraft:pig_sound_variant",
	"minecraft:wolf_sound_variant",
}

// soundVariantFields maps a sound key of an entry to its field.
var soundVariantFields = map[string]func(*SoundVariantSet) *Sound{
	"ambient_sound":       func(s *SoundVariantSet) *Sound { return &s.Ambient },
	"hurt_sound":          func(s *SoundVariantSet) *Sound { return &s.Hurt },
	"death_sound":         func(s *SoundVariantSet) *Sound { return &s.Death },
	"eat_sound":           func(s *SoundVariantSet) *Sound { return &s.Eat },
	"step_sound":          func(s *SoundVariantSet) *Sound { return &s.Step },
	"growl_sound":         func(s *SoundVariantSet) *Sound { return &s.Growl },
	"whine_sound":         func(s *SoundVariantSet) *Sound { return &s.Whine },
	"pant_sound":          func(s *SoundVariantSet) *Sound { return &s.Pant },
	"beg_for_food_sound":  func(s *SoundVariantSet) *Sound { return &s.BegForFood },
	"hiss_sound":          func(s *SoundVariantSet) *Sound { return &s.Hiss },
	"purr_sound":          func(s *SoundVariantSet) *Sound { return &s.Purr },
	"purreow_sound":       func(s *SoundVariantSet) *Sound { return &s.Purreow },
	"stray_ambient_sound": func(s *SoundVariantSet) *Sound { return &s.StrayAmbient },
}

type soundVariantKey struct{ registry, entry string }

// soundVariantsNow holds the adult and baby sets by registry then entry name:
// the built-in ones with the data packs' entries over them.
var soundVariantsNow = func() *atomic.Pointer[map[soundVariantKey][2]SoundVariantSet] {
	var p atomic.Pointer[map[soundVariantKey][2]SoundVariantSet]
	m := buildSoundVariants()
	p.Store(&m)
	return &p
}()

func reindexSoundVariants() {
	m := buildSoundVariants()
	soundVariantsNow.Store(&m)
}

// SoundVariantOf returns the adult or baby sound set of a *_sound_variant
// entry, false for an entry the registry does not have. A farm animal's baby
// speaks its own set only for kinds whose entries carry one; cows do not.
// vanilla: Wolf.getSoundSet, Cat.getSoundSet
func SoundVariantOf(registryName, entry string, baby bool) (SoundVariantSet, bool) {
	sets, ok := (*soundVariantsNow.Load())[soundVariantKey{registryName, entry}]
	if !ok {
		return SoundVariantSet{}, false
	}
	if baby {
		return sets[1], true
	}
	return sets[0], true
}

func buildSoundVariants() map[soundVariantKey][2]SoundVariantSet {
	out := map[soundVariantKey][2]SoundVariantSet{}
	for _, regName := range SoundVariantRegistries {
		r, ok := Get(regName)
		if !ok {
			continue
		}
		for _, name := range r.Entries {
			if data, packed := nbt.CompoundView(r.Data[name]); packed {
				if sets, ok := packSoundSets(regName, data); ok {
					out[soundVariantKey{regName, name}] = sets
					continue
				}
			}
			if raw, ok := builtinSoundVariants[regName][name]; ok {
				var sets [2]SoundVariantSet
				for i := range sets {
					sets[i], _ = soundSetOf(regName, raw[i], false)
				}
				out[soundVariantKey{regName, name}] = sets
			}
		}
	}
	return out
}

// packSoundSets reads a pack entry; false when a sound the registry's entries
// need is missing, which keeps the built-in entry of that name.
func packSoundSets(regName string, data nbt.Compound) ([2]SoundVariantSet, bool) {
	var sets [2]SoundVariantSet
	adult, hasAdult := nbt.CompoundView(data["adult_sounds"])
	baby, hasBaby := nbt.CompoundView(data["baby_sounds"])
	if !hasAdult {
		adult, baby, hasBaby = data, data, true
	}
	if !hasBaby {
		return sets, false
	}
	for i, c := range [2]nbt.Compound{adult, baby} {
		raw := make(map[string]string, len(c))
		for key, tag := range c {
			raw[key] = soundEventName(tag)
		}
		set, ok := soundSetOf(regName, raw, true)
		if !ok {
			return sets, false
		}
		sets[i] = set
	}
	return sets, true
}

// soundEventName is the name a sound_event field holds: a string, or the
// inline form's sound_id (its fixed range is not kept).
// vanilla: SoundEvent.CODEC
func soundEventName(tag nbt.Tag) string {
	if s, ok := tag.(nbt.String); ok {
		return Qualify(string(s))
	}
	if c, ok := nbt.CompoundView(tag); ok {
		if s, ok := c["sound_id"].(nbt.String); ok {
			return Qualify(string(s))
		}
	}
	return ""
}

// soundSetOf resolves the named sounds; with strict, a key the registry's
// classic entry has but raw lacks fails the entry.
func soundSetOf(regName string, raw map[string]string, strict bool) (SoundVariantSet, bool) {
	var set SoundVariantSet
	if strict {
		for key := range builtinSoundVariants[regName]["minecraft:classic"][0] {
			if raw[key] == "" {
				return set, false
			}
		}
	}
	for key, name := range raw {
		if field, ok := soundVariantFields[key]; ok && name != "" {
			*field(&set) = SoundByName(name)
		}
	}
	return set, true
}
