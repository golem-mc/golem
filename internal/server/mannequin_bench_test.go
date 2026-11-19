package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// BenchmarkMannequinPairing builds the packets a new watcher receives.
func BenchmarkMannequinPairing(b *testing.B) {
	for _, dressed := range []bool{false, true} {
		name := "bare"
		ent := &mannequinEntity{mannequin: entity.NewMannequin(7, 0.5, 64, 0.5)}
		if dressed {
			name = "equipped"
			applyMannequinTag(ent, nbt.Compound{
				"CustomName": nbt.String("Guide"), "CustomNameVisible": nbt.Byte(1), "Glowing": nbt.Byte(1),
				"equipment": nbt.Compound{"mainhand": nbt.Compound{"id": nbt.String("minecraft:diamond_sword")}, "head": nbt.Compound{"id": nbt.String("minecraft:diamond_helmet")}},
			})
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				mannequinPairing(ent)
			}
		})
	}
}

// BenchmarkTickMannequinsTracked runs the mannequin pass over bodies at rest,
// every one watched by every player.
func BenchmarkTickMannequinsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.registerMannequin(&mannequinEntity{
			mannequin: entity.NewMannequin(s.nextEntity(), x, float64(world.GroundLevel), z),
			w:         w,
		})
	}
	for range 40 {
		s.tickMannequins()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMannequins()
	}
}

// BenchmarkTickMannequinsNone runs the mannequin pass in a world with none,
// the cost every server pays.
func BenchmarkTickMannequinsNone(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMannequins()
	}
}

// BenchmarkMannequinPushInputsMobs snapshots a mannequin's crowd among tracked mobs.
func BenchmarkMannequinPushInputsMobs(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	s.registerMannequin(&mannequinEntity{
		mannequin: entity.NewMannequin(s.nextEntity(), 0.5, float64(world.GroundLevel), 0.5),
		w:         w,
	})
	teams := s.pushTeams()
	s.mannequinPushInputs(teams)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.mannequinPushInputs(teams)
	}
}
