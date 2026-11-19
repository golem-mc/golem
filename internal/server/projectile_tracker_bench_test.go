package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickArrowsTracked runs the whole arrow tick over arrows hanging in
// the air with no gravity, every one watched by every player.
func BenchmarkTickArrowsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		it := newArrowEntity(s, w, x, world.GroundLevel+10, z, 0, 0, 0, 0, 0,
			false, entity.ArrowBaseDamage, entity.PickupDisallowed, 0, uuid.Nil,
			protocol.ItemStack{}, 0)
		it.arrow.NoGravity = true
		it.arrow.Life = -1 << 30
		s.registerArrow(it)
	}
	for range 40 {
		s.tickArrows()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickArrows()
	}
	b.StopTimer()
	if n := len(s.arrows); n != trackerBenchBodies {
		b.Fatalf("%d arrows after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickThrowablesTracked runs the whole thrown tick over snowballs
// hanging in the air with no gravity, every one watched by every player.
func BenchmarkTickThrowablesTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	item, _ := registry.ItemID("minecraft:snowball")
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.standThrown(entity.ThrownSnowball, w, x, world.GroundLevel+10, z, 0, 0, 0, 0, 0, 0, uuid.Nil,
			int64(i), entity.NewRolls(int64(i)), protocol.ItemStack{ItemID: item, Count: 1})
	}
	for _, it := range s.thrown {
		it.throwable.NoGravity = true
	}
	for range 40 {
		s.tickThrowables()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickThrowables()
	}
	b.StopTimer()
	if n := len(s.thrown); n != trackerBenchBodies {
		b.Fatalf("%d thrown bodies after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkFrozenProjectileTracking runs the frozen world's tracking passes
// of the arrows and the thrown bodies, with every player in range.
func BenchmarkFrozenProjectileTracking(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	item, _ := registry.ItemID("minecraft:snowball")
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.registerArrow(newArrowEntity(s, w, x, world.GroundLevel+10, z, 0, 0, 0, 0, 0,
			false, entity.ArrowBaseDamage, entity.PickupDisallowed, 0, uuid.Nil,
			protocol.ItemStack{}, 0))
		s.standThrown(entity.ThrownSnowball, w, x, world.GroundLevel+10, z, 0, 0, 0, 0, 0, 0, uuid.Nil,
			int64(i), entity.NewRolls(int64(i)), protocol.ItemStack{ItemID: item, Count: 1})
	}
	watchers := s.trackingWatchers()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		arrowTracker.frozenPass(s, watchers)
		thrownTracker.frozenPass(s, watchers)
	}
}

// BenchmarkTickThrownHerd runs the thrown tick over snowballs hanging above
// a herd of cows, the herd's targets what every flight's query sees.
func BenchmarkTickThrownHerd(b *testing.B) { benchTickThrownHerd(b, 0) }

// BenchmarkTickThrownHerdCharges is BenchmarkTickThrownHerd with wind
// charges hanging out of every snowball's reach, which a flight may turn.
func BenchmarkTickThrownHerdCharges(b *testing.B) { benchTickThrownHerd(b, 20) }

// benchTickThrownHerd stands 300 snowballs and the charges over the herd
// and times the thrown tick.
func benchTickThrownHerd(b *testing.B, charges int) {
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeCow, float64(i%45)*2-45, world.GroundLevel, float64(i/45)*2-45, int64(i), nil)
	}
	item, _ := registry.ItemID("minecraft:snowball")
	for i := range 300 {
		x, z := float64(i%20)*1.5-15+0.25, float64(i/20)*1.5-11+0.25
		s.standThrown(entity.ThrownSnowball, w, x, world.GroundLevel+10, z, 0, 0, 0, 0, 0, 0, uuid.Nil,
			int64(i), entity.NewRolls(int64(i)), protocol.ItemStack{ItemID: item, Count: 1})
	}
	for i := range charges {
		s.standThrown(entity.ThrownBreezeWindCharge, w, float64(i)*2-20, world.GroundLevel+30, 0, 0, 0, 0, 0, 0, 0, uuid.Nil,
			int64(i), entity.NewRolls(int64(i)), protocol.ItemStack{})
	}
	for _, it := range s.thrown {
		it.throwable.NoGravity = true
	}
	s.tickThrowables()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickThrowables()
	}
	b.StopTimer()
	if n := len(s.thrown); n != 300+charges {
		b.Fatalf("%d thrown bodies after the run, want %d", n, 300+charges)
	}
}
