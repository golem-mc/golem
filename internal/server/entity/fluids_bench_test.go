package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkFallingColumnFlow reads the current of a falling water column
// walled on its east side, the path that tests the neighbours' faces.
func BenchmarkFallingColumnFlow(b *testing.B) {
	falling, ok := registry.FindState("minecraft:water", map[string]string{"level": "8"})
	if !ok {
		b.Fatal("the block registry has no falling water state")
	}
	w := newTestWorld()
	stone := defaultState(b, "minecraft:stone")
	for y := int32(60); y <= 64; y++ {
		w.set(0, y, 0, falling)
		w.set(1, y, 0, stone)
	}
	cell := fluidCellOf(falling)
	var sink float64
	b.ReportAllocs()
	for b.Loop() {
		_, fy, _ := fluidFlowAt(w, 0, 62, 0, cell)
		sink += fy
	}
	if sink == 0 {
		b.Fatal("no downward flow beside the wall")
	}
}
