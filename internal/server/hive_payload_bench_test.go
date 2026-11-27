package server

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
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

func BenchmarkCancelledHiveRelease(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	// Spawn event binding resolves entities through the command query.
	for _, c := range s.clients {
		c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
	}
	fixture := &hiveReleaseBenchPlugin{}
	s.loadPlugins([]plugin.Registration{{Name: "bench", Plugin: fixture}})
	s.enablePlugins()
	s.DriveTicksHere()
	w.SetBlock(3, 64, 3, registry.BlockBeehive.DefaultState())
	occupant := nbt.Compound{
		"entity_data":       nbt.Compound{"id": nbt.String("minecraft:bee")},
		"ticks_in_hive":     nbt.Int(601),
		"min_ticks_in_hive": nbt.Int(600),
	}

	b.ReportAllocs()
	for b.Loop() {
		s.spawnOccupant(w, 3, 64, 3, 0, -1, false, occupant, false, nil)
	}
	if fixture.calls != b.N {
		b.Fatalf("spawn listener calls = %d, want %d", fixture.calls, b.N)
	}
}

type hiveReleaseBenchPlugin struct{ calls int }

func (p *hiveReleaseBenchPlugin) Enable(s plugin.Server) error {
	plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
		p.calls++
		e.SetCancelled(true)
	})
	return nil
}

func (*hiveReleaseBenchPlugin) Disable() {}

func BenchmarkTickHivesFarm(b *testing.B) {
	for _, n := range []int{1, 16, 100, 256} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			s, _ := newTrackerBenchServer(b)
			w := s.allDims()[0].w
			tag, err := nbt.ParseOrderedSNBT(`{bees:[` + strings.Repeat(`{entity_data:{id:"minecraft:bee"},ticks_in_hive:1,min_ticks_in_hive:2147483647},`, 3) + `]}`)
			if err != nil {
				b.Fatal(err)
			}
			for i := range int32(n) {
				x, y, z := i%16, 64+i/256, i/16%16
				w.SetBlock(x, y, z, registry.BlockBeeNest.DefaultState())
				w.SetBlockEntityTag(x, y, z, "minecraft:beehive", tag)
			}
			if len(w.Beehives()) != n {
				b.Fatalf("occupied hives = %d, want %d", len(w.Beehives()), n)
			}
			b.ReportAllocs()
			for b.Loop() {
				s.tickHives(w, nil)
			}
		})
	}
}
