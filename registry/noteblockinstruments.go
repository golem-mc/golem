package registry

// noteBlockInstrumentOf is noteBlockInstruments by Block, the harp filled in.
var noteBlockInstrumentOf = func() *[BlockCount]string {
	t := new([BlockCount]string)
	for b := range Block(BlockCount) {
		t[b] = "harp"
		if instrument, ok := noteBlockInstruments[blockInfo[b].name]; ok {
			t[b] = instrument
		}
	}
	return t
}()

// NoteBlockInstrumentOf is the instrument a block names a note block,
// "harp" for every block that sets none.
// vanilla: BlockBehaviour.Properties.instrument
func NoteBlockInstrumentOf(b Block) string {
	if b >= BlockCount {
		return "harp"
	}
	return noteBlockInstrumentOf[b]
}

// NoteBlockHeadInstrument reports whether an instrument is a head's (a mob
// head's or the custom head's), which a note block takes from the block
// above it and which sounds whatever stands over the note block.
// vanilla: NoteBlockInstrument.worksAboveNoteBlock
func NoteBlockHeadInstrument(instrument string) bool {
	switch instrument {
	case "zombie", "skeleton", "creeper", "dragon", "wither_skeleton", "piglin", "custom_head":
		return true
	}
	return false
}

// NoteBlockTunable reports whether an instrument plays at the note's pitch;
// the heads play at pitch 1. vanilla: NoteBlockInstrument.isTunable
func NoteBlockTunable(instrument string) bool {
	return !NoteBlockHeadInstrument(instrument)
}

// NoteBlockInstrument is the instrument a note block takes between the
// blocks above and below it: a head above names its own, otherwise the
// block below does, a head below counting as the harp.
// vanilla: NoteBlock.setInstrument
func NoteBlockInstrument(above, below Block) string {
	if instrument := NoteBlockInstrumentOf(above); NoteBlockHeadInstrument(instrument) {
		return instrument
	}
	if instrument := NoteBlockInstrumentOf(below); !NoteBlockHeadInstrument(instrument) {
		return instrument
	}
	return "harp"
}

// NoteBlockSound returns an instrument's registered event, or zero for a custom head.
// vanilla: NoteBlockInstrument.getSoundEvent
func NoteBlockSound(instrument string) Sound { return noteBlockSounds[instrument] }

var noteBlockSounds = map[string]Sound{
	"harp":              SoundBlockNoteBlockHarp,
	"basedrum":          SoundBlockNoteBlockBasedrum,
	"snare":             SoundBlockNoteBlockSnare,
	"hat":               SoundBlockNoteBlockHat,
	"bass":              SoundBlockNoteBlockBass,
	"flute":             SoundBlockNoteBlockFlute,
	"bell":              SoundBlockNoteBlockBell,
	"guitar":            SoundBlockNoteBlockGuitar,
	"chime":             SoundBlockNoteBlockChime,
	"xylophone":         SoundBlockNoteBlockXylophone,
	"iron_xylophone":    SoundBlockNoteBlockIronXylophone,
	"cow_bell":          SoundBlockNoteBlockCowBell,
	"didgeridoo":        SoundBlockNoteBlockDidgeridoo,
	"bit":               SoundBlockNoteBlockBit,
	"banjo":             SoundBlockNoteBlockBanjo,
	"pling":             SoundBlockNoteBlockPling,
	"trumpet":           SoundBlockNoteBlockTrumpet,
	"trumpet_exposed":   SoundBlockNoteBlockTrumpetExposed,
	"trumpet_oxidized":  SoundBlockNoteBlockTrumpetOxidized,
	"trumpet_weathered": SoundBlockNoteBlockTrumpetWeathered,
	"zombie":            SoundBlockNoteBlockImitateZombie,
	"skeleton":          SoundBlockNoteBlockImitateSkeleton,
	"creeper":           SoundBlockNoteBlockImitateCreeper,
	"dragon":            SoundBlockNoteBlockImitateEnderDragon,
	"wither_skeleton":   SoundBlockNoteBlockImitateWitherSkeleton,
	"piglin":            SoundBlockNoteBlockImitatePiglin,
}
