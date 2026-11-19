package world

import (
	"reflect"
	"testing"
)

// TestStructureBlockFieldsSurviveTheSavedForm: every field of the entity
// reads back from the compound a chunk save writes, and an out-of-range
// offset and size are clamped as the game loads them.
// vanilla: StructureBlockEntity.saveAdditional, loadAdditional
func TestStructureBlockFieldsSurviveTheSavedForm(t *testing.T) {
	want := StructureBlock{
		Name: "test:house", Author: "Alex", Metadata: "chest", Pos: [3]int32{-3, 2, 48}, Size: [3]int32{4, 5, 6},
		Rotation: Rotate180, Mirror: MirrorFrontBack, Mode: StructureCorner, Strict: true, Powered: true,
		ShowAir: true, Integrity: 0.5, Seed: 1 << 40,
	}
	if got := ParseStructureBlock(want.Compound()); !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	big := want
	big.Pos, big.Size = [3]int32{-99, 99, 0}, [3]int32{99, -4, 7}
	got := ParseStructureBlock(big.Compound())
	if got.Pos != [3]int32{-48, 48, 0} || got.Size != [3]int32{48, 0, 7} {
		t.Errorf("clamped pos %v size %v, want [-48 48 0] [48 0 7]", got.Pos, got.Size)
	}
}
