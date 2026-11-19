package registry

import "testing"

func TestNoteBlockSoundIDs(t *testing.T) {
	seen := map[string]bool{}
	for state := range uint16(StateCount) {
		if BlockOf(state) != BlockNoteBlock {
			continue
		}
		instrument := StateValue(state, PropInstrument)
		if seen[instrument] {
			continue
		}
		seen[instrument] = true
		want := "minecraft:block.note_block." + instrument
		switch instrument {
		case "zombie", "skeleton", "creeper", "wither_skeleton", "piglin":
			want = "minecraft:block.note_block.imitate." + instrument
		case "dragon":
			want = "minecraft:block.note_block.imitate.ender_dragon"
		case "custom_head":
			want = ""
		}
		if got := NoteBlockSound(instrument); got.Name() != want {
			t.Errorf("%s: got %d (%s), want %s", instrument, got, got.Name(), want)
		}
	}
}
