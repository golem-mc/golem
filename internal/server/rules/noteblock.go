package rules

import "github.com/golem-mc/golem/registry"

// NoteBlockInstrumentState returns the note block state with the instrument
// the blocks above and below (x, y, z) name, its note and power kept; a
// state that is no note block comes back unchanged.
// vanilla: NoteBlock.getStateForPlacement, NoteBlock.setInstrument
func NoteBlockInstrumentState(w BlockReader, state uint16, x, y, z int32) uint16 {
	if registry.BlockOf(state) != registry.BlockNoteBlock {
		return state
	}
	above := registry.BlockOf(w.BlockAt(x, y+1, z))
	below := registry.BlockOf(w.BlockAt(x, y-1, z))
	if s, ok := registry.WithProp(state, registry.PropInstrument, registry.NoteBlockInstrument(above, below)); ok {
		return s
	}
	return state
}
