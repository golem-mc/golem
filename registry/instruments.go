package registry

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
	in, ok := instruments[r.Entries[id]]
	return in, ok
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
