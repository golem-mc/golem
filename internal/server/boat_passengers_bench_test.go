package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickBoatsMobPassengers runs the whole boat tick with two pigs
// aboard each hull, including the per-tick carry effects for every seat.
func BenchmarkTickBoatsMobPassengers(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnBoat(w, "minecraft:oak_boat", x, float64(world.GroundLevel), z, float32(i*7), customName{})
		for j := range 2 {
			id := s.spawnMobInDim(s.allDims()[0], entity.TypePig, x, float64(world.GroundLevel), z, int64(i*2+j+1), nil)
			s.mobsMu.Lock()
			s.mobByEntityIDLocked(w, id).mob.NoAI = true
			s.mobsMu.Unlock()
		}
	}
	checkPassengers := func() {
		if n := len(s.boats); n != trackerBenchBodies {
			b.Fatalf("%d boats after ticking, want %d", n, trackerBenchBodies)
		}
		for _, it := range s.boats {
			if n := len(it.passengers); n != 2 {
				b.Fatalf("boat %d carries %d passengers, want 2", it.boat.ID, n)
			}
			for i, p := range it.passengers {
				if p.ent == nil || p.ent.vehicle != it || !p.ent.mob.RidesVehicle {
					b.Fatalf("boat %d seat %d lost its mob passenger", it.boat.ID, i)
				}
				x, y, z := it.seatOf(2, i, true)
				m := p.ent.mob
				if m.X != x || m.Y != y || m.Z != z {
					b.Fatalf("boat %d seat %d: mob at %v/%v/%v, want %v/%v/%v", it.boat.ID, i, m.X, m.Y, m.Z, x, y, z)
				}
			}
		}
	}
	for range 40 {
		s.tickBoats()
	}
	checkPassengers()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBoats()
	}
	b.StopTimer()
	checkPassengers()
}
