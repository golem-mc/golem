package registry

import (
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
)

// Holder returns the wire holder of a sound: a registered one by ID, a data
// pack's sound outside the built-in registry inline by name, and an empty
// holder for none.
func (s Sound) Holder() protocol.SoundHolder {
	if s > Sound(len(SoundEvents)) {
		return protocol.SoundHolder{ID: -1, Name: s.Name()}
	}
	return protocol.SoundHolder{ID: int32(s) - 1}
}

// Name returns the registry key, or empty for an absent or invalid sound holder.
func (s Sound) Name() string {
	if s <= SoundNone {
		return ""
	}
	if int(s) > len(SoundEvents) {
		packSounds.RLock()
		defer packSounds.RUnlock()
		if i := int(s) - len(SoundEvents) - 1; i < len(packSounds.names) {
			return packSounds.names[i]
		}
		return ""
	}
	return SoundEvents[s-1]
}

// IsPack reports whether the sound is one a data pack names that the
// built-in registry lacks; such a sound travels inline by name.
func (s Sound) IsPack() bool { return int(s) > len(SoundEvents) }

// packSounds interns the sound names data packs use outside SoundEvents, so a
// mob's sound set stays a plain Sound. Entries are never removed: the set is
// bounded by the names the loaded packs mention.
var packSounds struct {
	sync.RWMutex
	ids   map[string]Sound
	names []string
}

// SoundByName returns the Sound of a sound event name: the built-in one, else
// an interned stand-in that Holder sends inline, as a pack's sound_event may
// name a resource-pack sound.
func SoundByName(name string) Sound {
	if id, ok := SoundEventID(name); ok {
		return Sound(id + 1)
	}
	packSounds.Lock()
	defer packSounds.Unlock()
	if s, ok := packSounds.ids[name]; ok {
		return s
	}
	if packSounds.ids == nil {
		packSounds.ids = map[string]Sound{}
	}
	packSounds.names = append(packSounds.names, name)
	s := Sound(len(SoundEvents) + len(packSounds.names))
	packSounds.ids[name] = s
	return s
}

// SoundHolderByName resolves an exact external sound name, retaining unknown names inline.
func SoundHolderByName(name string) protocol.SoundHolder {
	if id, ok := SoundEventID(name); ok {
		return protocol.SoundHolder{ID: id}
	}
	return protocol.SoundHolder{ID: -1, Name: name}
}
