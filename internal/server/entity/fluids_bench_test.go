package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkFallingColumnFlow reads the current of a falling water column
// walled on its east side, the path that tests the neighbours' faces.
func BenchmarkFallingColumnFlow(b *testing.B) {
	w, cell := fallingColumnFlowFixture(b)
	var sink float64
	b.ReportAllocs()
	for b.Loop() {
		_, fy, _ := fluidFlowAt(w, 0, fallingColumnY, 0, cell)
		sink += fy
	}
	if sink == 0 {
		b.Fatal("no downward flow beside the wall")
	}
}

const fallingColumnY = 62

func fallingColumnFlowFixture(t testing.TB) (testWorld, fluidCell) {
	t.Helper()
	falling, ok := registry.FindState("minecraft:water", map[string]string{"level": "8"})
	if !ok {
		t.Fatal("the block registry has no falling water state")
	}
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for y := int32(fallingColumnY - 2); y <= fallingColumnY+2; y++ {
		w.set(0, y, 0, falling)
		w.set(1, y, 0, stone)
	}
	return w, fluidCellOf(falling)
}

type fluidReadCounter struct {
	World
	reads map[[3]int32]int
}

func (w *fluidReadCounter) BlockAt(x, y, z int32) uint16 {
	w.reads[[3]int32{x, y, z}]++
	return w.World.BlockAt(x, y, z)
}

func TestFluidFlowReadsHorizontalNeighborsOnce(t *testing.T) {
	w, cell := fallingColumnFlowFixture(t)
	counter := &fluidReadCounter{World: w, reads: make(map[[3]int32]int)}
	fx, fy, fz := fluidFlowAt(counter, 0, fallingColumnY, 0, cell)
	if fx != 0 || fy != -1 || fz != 0 {
		t.Errorf("flow beside the east wall = (%v, %v, %v), want (0, -1, 0)", fx, fy, fz)
	}
	for _, pos := range [4][3]int32{{1, fallingColumnY, 0}, {-1, fallingColumnY, 0}, {0, fallingColumnY, 1}, {0, fallingColumnY, -1}} {
		if reads := counter.reads[pos]; reads != 1 {
			t.Errorf("block at %v read %d times, want 1", pos, reads)
		}
	}
}
