package rules

// The instruments goats drop, in the order of the instrument tags.
// vanilla: data/minecraft/tags/instrument/regular_goat_horns.json,
// screaming_goat_horns.json
var (
	regularGoatHorns = []string{
		"minecraft:ponder_goat_horn", "minecraft:sing_goat_horn",
		"minecraft:seek_goat_horn", "minecraft:feel_goat_horn",
	}
	screamingGoatHorns = []string{
		"minecraft:admire_goat_horn", "minecraft:call_goat_horn",
		"minecraft:yearn_goat_horn", "minecraft:dream_goat_horn",
	}
)

// GoatHornInstrument is the instrument of the horn a goat drops: a pick from
// its tag, seeded by the hash of the goat's UUID so the same goat always
// drops the same sound.
// vanilla: Goat.createHorn
func GoatHornInstrument(uuid [16]byte, screaming bool) string {
	var msb, lsb uint64
	for i := range 8 {
		msb = msb<<8 | uint64(uuid[i])
		lsb = lsb<<8 | uint64(uuid[8+i])
	}
	// UUID.hashCode
	hilo := msb ^ lsb
	seed := int64(int32(hilo>>32) ^ int32(hilo))
	// SingleThreadedRandomSource.nextInt(4): the power-of-two branch takes
	// the high bits of the first 31-bit draw.
	state := (seed ^ 0x5DEECE66D) & (1<<48 - 1)
	state = (state*0x5DEECE66D + 0xB) & (1<<48 - 1)
	draw := int64(state >> 17)
	tag := regularGoatHorns
	if screaming {
		tag = screamingGoatHorns
	}
	return tag[(int64(len(tag))*draw)>>31]
}
