package rules

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

var wireBenchState uint16

func BenchmarkWirePlacementState(b *testing.B) {
	w := testWorld{}
	w[[3]int32{0, 1, 0}] = registry.MustState("minecraft:stone", nil)
	wire := registry.MustState("minecraft:redstone_wire", nil)
	for _, p := range [][3]int32{{1, 2, 0}, {-1, 2, 0}, {0, 2, 1}, {0, 2, -1}} {
		w[p] = wire
	}
	b.ReportAllocs()
	for b.Loop() {
		wireBenchState, _ = WirePlacementState(w, 0, 2, 0)
	}
}
