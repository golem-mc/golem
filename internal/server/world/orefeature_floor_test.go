package world

import "testing"

func TestOceanFloorCacheIgnoresDecoration(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(5)
	r := newDecorRegion(g, newChunk(0, 0), worldMinY, worldHeight)
	r.nx, r.nz = 0, 0
	stone := mustState("minecraft:stone", nil)
	const x, z, y = 3, 4, 300
	floor := r.oceanFloorHeight(x, z)
	if r.oceanFloorReaches(x, z, y) {
		t.Fatal("terrain reaches y 300")
	}
	r.set(x, y-1, z, stone)
	if got := oceanFloorWGHeight(r, x, z); got != floor {
		t.Fatalf("placement height %d, want undecorated floor %d", got, floor)
	}
	if r.oceanFloorReaches(x, z, y) {
		t.Fatal("decoration raised the worldgen heightmap")
	}
	r.protoAt(x, z).set(x, y-1-r.minY, z, stone)
	if r.oceanFloorReaches(x, z, y) {
		t.Fatal("proto write raised the worldgen heightmap")
	}
	if !r.oceanFloorReaches(x, z, floor) {
		t.Fatal("terrain does not reach its original floor")
	}
	r.set(x, floor-1, z, airState)
	if !r.oceanFloorReaches(x, z, floor) {
		t.Fatal("decoration lowered the worldgen heightmap")
	}
}

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
