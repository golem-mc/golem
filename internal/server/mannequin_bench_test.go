package server

import (
	"fmt"
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

// BenchmarkTickMannequinsMoving is BenchmarkTickMannequinsTracked with every
// mannequin moved a block and its lodged arrow count changed each tick, so
// each tick opens a movement pass that sends a position and the data.
func BenchmarkTickMannequinsMoving(b *testing.B) {
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
	for i := range b.N {
		for _, ent := range s.mannequins {
			ent.mannequin.Y = float64(world.GroundLevel) + float64(i%2)
			ent.mannequin.ArrowCount = i % 2
		}
		s.tickMannequins()
	}
}

// BenchmarkTickMannequinsFalling runs the mannequin pass over bodies falling
// in sight of every player, so each movement gate sends motion and a move.
func BenchmarkTickMannequinsFalling(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	var ents []*mannequinEntity
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		ent := &mannequinEntity{
			mannequin: entity.NewMannequin(s.nextEntity(), x, float64(world.GroundLevel)+200, z),
			w:         w,
		}
		ents = append(ents, ent)
		s.registerMannequin(ent)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%40 == 0 { // back to the top before any lands
			b.StopTimer()
			for _, ent := range ents {
				ent.mannequin.SetPos(ent.mannequin.X, float64(world.GroundLevel)+200, ent.mannequin.Z)
				ent.mannequin.VY = 0
			}
			b.StartTimer()
		}
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

// BenchmarkTickMannequinsInHerd times one mannequin far from a herd of cows.
func BenchmarkTickMannequinsInHerd(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(fmt.Sprint(cows), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			dim := s.allDims()[0]
			for i := range cows {
				s.spawnMobInDim(dim, entity.TypeCow, float64(i%45)-22, float64(world.GroundLevel), float64(i/45)-22, int64(i+1), nil)
			}
			s.registerMannequin(&mannequinEntity{
				mannequin: entity.NewMannequin(s.nextEntity(), 40, float64(world.GroundLevel), 40),
				w:         w,
			})
			for range 20 {
				s.tickMannequins()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickMannequins()
			}
		})
	}
}
