package world

import "testing"

func BenchmarkOreProbeAboveTerrain(b *testing.B) {
	g := NewOverworldGenerator(5)
	stone := mustState("minecraft:stone", nil)
	cfg := &oreConfig{size: 9, probe: true, targets: []oreTarget{{test: func(s uint16, _ int32) bool { return s == stone }, state: stone}}}
	b.ReportAllocs()
	for b.Loop() {
		r := newDecorRegion(g, newChunk(0, 0), worldMinY, worldHeight)
		r.nx, r.nz = 0, 0
		for i := range int32(100) {
			r.placeOre([3]int32{i % 16, 300, i / 16}, cfg)
		}
	}
}
