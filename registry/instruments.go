package registry

import (
	"math"

	"github.com/golem-mc/golem/nbt"
)

// Instrument is a minecraft:instrument entry's gameplay data.
// vanilla: Instrument; data/minecraft/instrument
type Instrument struct {
	Sound       Sound
	UseSeconds  float32
	RangeBlocks float32
}

// DefaultInstrument is the entry a goat horn without the component plays.
// vanilla: Items.GOAT_HORN
const DefaultInstrument = "minecraft:ponder_goat_horn"

// InstrumentByID returns the entry the minecraft:instrument registry ID
// names, false for an ID past the registry.
func InstrumentByID(id int32) (Instrument, bool) {
	r, ok := Get("minecraft:instrument")
	if !ok || id < 0 || int(id) >= len(r.Entries) {
		return Instrument{}, false
	}
	return instrumentOf(r, r.Entries[id])
}

// instrumentOf reads a data pack's entry when there is one, else the built-in
// table. A pack entry without a sound or with a bad duration or range is not
// an instrument, as vanilla's codec rejects it.
// vanilla: Instrument.DIRECT_CODEC
func instrumentOf(r Registry, name string) (Instrument, bool) {
	data, packed := nbt.CompoundView(r.Data[name])
	if !packed {
		in, ok := instruments[name]
		return in, ok
	}
	sound := soundEventName(data["sound_event"])
	use, uok := nbt.FloatField(data, "use_duration")
	rng, rok := nbt.FloatField(data, "range")
	if sound == "" || !uok || !rok || use < 0 || rng <= 0 || math.IsNaN(float64(use)) || math.IsNaN(float64(rng)) || math.IsInf(float64(use), 0) || math.IsInf(float64(rng), 0) {
		return Instrument{}, false
	}
	return Instrument{Sound: SoundByName(sound), UseSeconds: use, RangeBlocks: rng}, true
}

// InstrumentID returns the minecraft:instrument registry ID of a name, false
// for an entry the registry does not have.
func InstrumentID(name string) (int32, bool) {
	r, ok := Get("minecraft:instrument")
	if !ok {
		return 0, false
	}
	if id := r.ID(name); id >= 0 {
		return id, true
	}
	return 0, false
}

// DefaultInstrumentData returns the entry of DefaultInstrument.
func DefaultInstrumentData() Instrument { return instruments[DefaultInstrument] }
