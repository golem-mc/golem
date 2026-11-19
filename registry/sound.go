package registry

import "github.com/golem-mc/golem/internal/protocol"

// Holder returns the wire holder of a registered sound, or an empty inline holder for none.
func (s Sound) Holder() protocol.SoundHolder { return protocol.SoundHolder{ID: int32(s) - 1} }

// Name returns the registry key, or empty for an absent or invalid sound holder.
func (s Sound) Name() string {
	if s <= SoundNone || int(s) > len(SoundEvents) {
		return ""
	}
	return SoundEvents[s-1]
}

// SoundHolderByName resolves an exact external sound name, retaining unknown names inline.
func SoundHolderByName(name string) protocol.SoundHolder {
	if id, ok := SoundEventID(name); ok {
		return protocol.SoundHolder{ID: id}
	}
	return protocol.SoundHolder{ID: -1, Name: name}
}
