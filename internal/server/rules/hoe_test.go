package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestHoeTill checks the tillables, the air above and the click from below.
func TestHoeTill(t *testing.T) {
	farmland := stateOf(t, "minecraft:farmland")
	dirt := stateOf(t, "minecraft:dirt")
	stone := stateOf(t, "minecraft:stone")
	for _, tc := range []struct {
		name  string
		block uint16
		above uint16
		face  int32
		into  uint16
		drop  string
		ok    bool
	}{
		{"grass from the top", stateOf(t, "minecraft:grass_block"), 0, protocol.FaceTop, farmland, "", true},
		{"dirt from the side", dirt, 0, protocol.FaceNorth, farmland, "", true},
		{"dirt from below", dirt, 0, protocol.FaceBottom, 0, "", false},
		{"dirt under stone", dirt, stone, protocol.FaceTop, 0, "", false},
		{"coarse dirt", stateOf(t, "minecraft:coarse_dirt"), 0, protocol.FaceTop, dirt, "", true},
		{"rooted dirt under stone from below", stateOf(t, "minecraft:rooted_dirt"), stone, protocol.FaceBottom, dirt, "minecraft:till/rooted_dirt", true},
		{"stone", stone, 0, protocol.FaceTop, 0, "", false},
		{"bad face", dirt, 0, 9, 0, "", false},
	} {
		w := stubBlocks{{0, 64, 0}: tc.block, {0, 65, 0}: tc.above}
		into, drop, ok := HoeTill(w, 0, 64, 0, tc.face)
		if ok != tc.ok || into != tc.into || drop != tc.drop {
			t.Errorf("%s: HoeTill = %d %q %v, want %d %q %v", tc.name, into, drop, ok, tc.into, tc.drop, tc.ok)
		}
	}
}
