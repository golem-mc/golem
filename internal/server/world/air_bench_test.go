package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkAirIn measures the flight check over a player's surrounding cells.
func BenchmarkAirIn(b *testing.B) {
	w := benchCollisionWorld(b)
	box := [6]float64{8.2, float64(GroundLevel) + 1, 8.2, 8.8, float64(GroundLevel) + 3, 8.8}
	b.ReportAllocs()
	for b.Loop() {
		if !w.AirIn(box) {
			b.Fatal("expected air above the ground")
		}
	}
}

// BenchmarkIsTurtleEgg measures the grounded body's per-tick egg test.
func BenchmarkIsTurtleEgg(b *testing.B) {
	states := [...]uint16{registry.BlockTurtleEgg.DefaultState(), registry.BlockStone.DefaultState(), registry.BlockAir.DefaultState()}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if IsTurtleEgg(states[i%len(states)]) != (i%len(states) == 0) {
			b.Fatal("wrong egg classification")
		}
	}
}
