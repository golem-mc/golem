package registry

import "testing"

// TestBlockTablesMatchNameMaps checks the Block-indexed note-block and
// falling-event tables against the generated maps they are built from.
func TestBlockTablesMatchNameMaps(t *testing.T) {
	seen := map[string]int{}
	for b := range Block(BlockCount) {
		name := b.Name()
		want, ok := noteBlockInstruments[name]
		if !ok {
			want = "harp"
		} else {
			seen["instrument"]++
		}
		if got := NoteBlockInstrumentOf(b); got != want {
			t.Errorf("%s: instrument %q, want %q", name, got, want)
		}
		wantLand, wantOK := FallingLandEvent[name]
		if got, ok := b.FallingLandEvent(); got != wantLand || ok != wantOK {
			t.Errorf("%s: land event %d %v, want %d %v", name, got, ok, wantLand, wantOK)
		} else if ok {
			seen["land"]++
		}
		wantBroken, wantOK := FallingBrokenEvent[name]
		if got, ok := b.FallingBrokenEvent(); got != wantBroken || ok != wantOK {
			t.Errorf("%s: broken event %d %v, want %d %v", name, got, ok, wantBroken, wantOK)
		} else if ok {
			seen["broken"]++
		}
	}
	// Every map key must name a block, or its entry is lost in the table.
	if seen["instrument"] != len(noteBlockInstruments) || seen["land"] != len(FallingLandEvent) ||
		seen["broken"] != len(FallingBrokenEvent) {
		t.Errorf("tables hold %v entries, maps hold %d, %d and %d",
			seen, len(noteBlockInstruments), len(FallingLandEvent), len(FallingBrokenEvent))
	}
	if NoteBlockInstrument(BlockZombieHead, BlockStone) != "zombie" || NoteBlockInstrument(BlockAir, BlockStone) != "basedrum" ||
		NoteBlockInstrument(BlockAir, BlockZombieHead) != "harp" {
		t.Error("a head above names the instrument, a head below counts as the harp")
	}
	if e, ok := Block(BlockCount).FallingLandEvent(); ok || e != 0 || NoteBlockInstrumentOf(Block(BlockCount)) != "harp" {
		t.Error("a Block past the last must read as no event and the harp")
	}
}
