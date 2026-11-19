package server

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkTickHivePayloads(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	w := s.allDims()[0].w
	tag, err := nbt.ParseOrderedSNBT(`{bees:[{entity_data:{id:"minecraft:bee",BB:1,Aa:2,nested:{BB:3,Aa:4}},ticks_in_hive:1,min_ticks_in_hive:2147483647}]}`)
	if err != nil {
		b.Fatal(err)
	}
	for x := int32(1); x <= 3; x++ {
		w.SetBlock(x, 64, 2, registry.BlockBeeNest.DefaultState())
		w.SetBlockEntityTag(x, 64, 2, "minecraft:beehive", tag)
	}
	if len(w.Beehives()) != 3 {
		b.Fatal("missing occupied nests")
	}
	b.ReportAllocs()
	for b.Loop() {
		s.tickHives(w, nil)
	}
}
