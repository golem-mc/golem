package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: MapItem.update, LinkedHashMultiset keeps the first colour on a tie.
func TestMapPixelColourTieKeepsEncounterOrder(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	for _, first := range []registry.Block{registry.BlockStone, registry.BlockDirt} {
		second := registry.BlockStone
		if first == second {
			second = registry.BlockDirt
		}
		for dx := range int32(2) {
			for dz := range int32(2) {
				block := first
				if dx != dz {
					block = second
				}
				w.SetBlock(dx, GroundLevel, dz, block.DefaultState())
			}
		}
		seen := false
		renderMapFace(w, &MapData{Scale: 1}, 0.5, 0.5, func(int) bool { return true }, func(x, y int, color byte) bool {
			if x == 64 && y == 64 {
				seen = true
				if want := registry.BlockMapColor(first.DefaultState()); color>>2 != want {
					t.Errorf("first %s: colour = %d, want %d", first.Name(), color>>2, want)
				}
			}
			return false
		})
		if !seen {
			t.Fatal("no centre pixel")
		}
	}
}
