package world

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const noteBlockName = "minecraft:note_block"

// noteBlockNeighborChanged moves POWERED on the signal's edges and plays the
// note on a rise. Its own arrival is no neighbour change, so the placed visit
// reads nothing. A flag-3 write: neighbour update, then the shape pass an
// observer reads.
// vanilla: NoteBlock.neighborChanged
func (p *Physics) noteBlockNeighborChanged(x, y, z int32, state uint16, changes *[]BlockChange, placed bool) {
	if placed {
		return
	}
	powered := p.hasNeighborSignal(x, y, z)
	if registry.StatePowered(state) == powered {
		return
	}
	if powered {
		p.playNote(x, y, z, state, false)
	}
	want, _ := registry.WithBool(state, registry.PropPowered, powered)
	p.setShaped(x, y, z, want, changes)
}

// noteBlockUsed steps the note up, wrapping 24 to 0, and plays it, with the
// same flag-3 write.
// vanilla: NoteBlock.useWithoutItem
func (p *Physics) noteBlockUsed(x, y, z int32, state uint16, changes *[]BlockChange) {
	want, _ := registry.WithInt(state, registry.PropNote, (stateIntOr(state, registry.PropNote, 0)+1)%25)
	p.setShaped(x, y, z, want, changes)
	p.playNote(x, y, z, want, true)
}

// AttackBlock answers a survival left-click: a note block plays and redstone
// ore lights.
// vanilla: BlockBehaviour.attack, NoteBlock.attack, RedStoneOreBlock.attack
func (p *Physics) AttackBlock(x, y, z int32) ([]BlockChange, []Effect) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	switch {
	case block == registry.BlockNoteBlock:
		p.playNote(x, y, z, state, true)
	case redstoneOreBlocks[block]:
		p.writeRedstoneOreLit(x, y, z, state, true, &changes)
	}
	effects, _ := p.takeWake()
	return changes, effects
}

// noteBlockShapeUpdate re-reads the instrument when the block above or below
// changed, with the shape update's flag-2 write.
// vanilla: NoteBlock.updateShape, NoteBlock.setInstrument
func (p *Physics) noteBlockShapeUpdate(x, y, z int32, d [3]int32, state uint16, changes *[]BlockChange) {
	if d[1] == 0 {
		return
	}
	above := registry.BlockOf(p.w.BlockAt(x, y+1, z))
	below := registry.BlockOf(p.w.BlockAt(x, y-1, z))
	instrument := registry.NoteBlockInstrument(above, below)
	if instrument == registry.StateValue(state, registry.PropInstrument) {
		return
	}
	if want, ok := registry.WithProp(state, registry.PropInstrument, instrument); ok {
		p.setClientShaped(x, y, z, want, changes)
	}
}

// vanilla: NoteBlock.playNote, NoteBlock.triggerEvent
func (p *Physics) playNote(x, y, z int32, state uint16, byUser bool) {
	instrument := registry.StateValue(state, registry.PropInstrument)
	if !registry.NoteBlockHeadInstrument(instrument) {
		if !IsAirState(p.w.BlockAt(x, y+1, z)) {
			return
		}
	}
	note := stateIntOr(state, registry.PropNote, 0)
	if p.listens(HookNote) {
		d := HookDecision{Kind: HookNote, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), Instrument: instrument, Note: note}
		if p.ask(&d) {
			return
		}
		instrument, note = d.Instrument, d.Note
	}
	event := Effect{X: x, Y: y, Z: z, GameEvent: GameEventNoteBlockPlay, GameEventByUser: byUser}
	sound := registry.NoteBlockSound(instrument)
	var customSound string
	if instrument == "custom_head" {
		tag, _ := p.w.BlockEntityTag(x, y+1, z)
		s, ok := nbt.Get[nbt.String](tag, "note_block_sound")
		if !ok {
			// A silent head still posts the vibration.
			p.effects = append(p.effects, event)
			return
		}
		customSound = string(s)
	}
	pitch := float32(1)
	if registry.NoteBlockTunable(instrument) {
		// vanilla: NoteBlock.getPitchFromNote
		pitch = float32(math.Pow(2, float64(note-12)/12))
	}
	p.effects = append(p.effects, event, Effect{
		SoundID:          sound,
		CustomSound:      customSound,
		SoundCategory:    protocol.SoundCategoryRecord,
		X:                x,
		Y:                y,
		Z:                z,
		Volume:           3, // vanilla: NoteBlock.NOTE_VOLUME
		Pitch:            pitch,
		HasBlockAction:   true,
		BlockActionBlock: protocol.BlockNoteBlock,
	})
}
