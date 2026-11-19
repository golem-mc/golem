package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// bookSand sets n sand blocks with air between them and books a visit of each
// due on tick 1.
func bookSand(w *World, p *Physics, n int) {
	sand := registry.BlockStates["minecraft:sand"]
	for i := range n {
		col, k := i%4096, i/4096
		x, z := int32(col%64)-16, int32(col/64)-16
		y := GroundLevel + 4 + 2*int32(k)
		w.SetBlock(x, y, z, sand)
		p.schedule(x, y, z, 1)
	}
}

func TestScheduledTickBudgetBlocks(t *testing.T) {
	w, p := newPhysicsWorld(t)
	bookSand(w, p, 20000)
	_, falls, _, _ := p.Tick(1)
	if len(falls) != 20000 {
		t.Fatalf("falls = %d, want 20000", len(falls))
	}
}
