package server

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickEndCrystalsTracked runs the whole end crystal tick over
// crystals of the overworld, every one watched by every player.
func BenchmarkTickEndCrystalsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.registerEndCrystal(s.newEndCrystalEntity(w, x, float64(world.GroundLevel), z))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickEndCrystals()
	}
}

// BenchmarkTickEyesTracked runs the whole eye of ender tick over eyes
// hovering over their own targets, every one watched by every player. The
// targets are set again every 64 ticks, before an eye's 80-tick flight
// ends it.
func BenchmarkTickEyesTracked(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.SpawnEyeEntity(x, float64(world.GroundLevel)+5, z, 0, 0, 0)
	}
	rng := rand.New(rand.NewSource(1))
	resignal := func() {
		s.eyesMu.Lock()
		for _, ent := range s.eyes {
			ent.eye.SignalTo(ent.eye.X, ent.eye.Y, ent.eye.Z, rng)
		}
		s.eyesMu.Unlock()
	}
	resignal()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%64 == 0 {
			b.StopTimer()
			resignal()
			b.StartTimer()
		}
		s.tickEyes()
	}
	b.StopTimer()
	if n := len(s.eyes); n != trackerBenchBodies {
		b.Fatalf("%d eyes after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickFireworksTracked runs the whole firework rocket tick over
// rockets shot at an angle with no speed and no end to their life, every
// one watched by every player.
func BenchmarkTickFireworksTracked(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.SpawnFireworkEntity(x, float64(world.GroundLevel)+5, z)
	}
	for _, it := range s.fireworks {
		it.rocket.VX, it.rocket.VY, it.rocket.VZ = 0, 0, 0
		it.rocket.Restore(0, 1<<30, true, true)
	}
	for range 40 {
		s.tickFireworks()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickFireworks()
	}
	b.StopTimer()
	if n := len(s.fireworks); n != trackerBenchBodies {
		b.Fatalf("%d rockets after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkWholeTickFireworksFrozen measures a frozen tick with and without
// rockets in range of the connected players.
func BenchmarkWholeTickFireworksFrozen(b *testing.B) {
	for _, count := range []int{0, trackerBenchBodies} {
		b.Run(fmt.Sprintf("%d_rockets", count), func(b *testing.B) {
			s, _ := newIdlePlayersBenchServer(b, trackerBenchWatchers)
			s.tickRate.SetFrozen(true)
			for i := range count {
				x, z := trackerBenchSpot(i)
				s.SpawnFireworkAtAngle(x, float64(world.GroundLevel)+5, z, 1.6, 0, 0, 30)
			}
			for range 20 {
				s.tick()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tick()
			}
		})
	}
}

// BenchmarkTickBobbersTracked runs the whole fishing bobber tick over hooks
// floating in water, every one watched by every player and all cast by one
// more player standing in their midst with a rod.
func BenchmarkTickBobbersTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	rod, _ := registry.ItemID("minecraft:fishing_rod")
	angler := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
	angler.SetPosition(player.Position{Y: float64(world.GroundLevel)})
	angler.Inventory(func(inv *player.Inventory) {
		inv.Slots[player.HotbarSlot] = protocol.ItemStack{ItemID: rod, Count: 1}
	})
	c := &client{srv: s, player: angler}
	c.dim.Store(s.allDims()[0])
	s.mu.Lock()
	s.assignPlayerSlot(c)
	s.clients[angler.UUID] = c
	s.mu.Unlock()

	water, _ := registry.FindState("minecraft:water", nil)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		w.SetBlock(int32(x), world.GroundLevel-1, int32(z), water)
		s.SpawnBobberEntity(x, float64(world.GroundLevel), z)
	}
	for _, it := range s.bobbers {
		it.owner = angler.UUID
	}
	for range 40 {
		s.tickBobbers()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBobbers()
	}
	b.StopTimer()
	if n := len(s.bobbers); n != trackerBenchBodies {
		b.Fatalf("%d bobbers after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkFrozenTrackingProjectileKinds runs the frozen world's tracking
// passes of the end crystals, the eyes and the bobbers over every body of
// each, with every player in range.
func BenchmarkFrozenTrackingProjectileKinds(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.registerEndCrystal(s.newEndCrystalEntity(w, x, float64(world.GroundLevel), z))
		s.SpawnEyeEntity(x, float64(world.GroundLevel)+5, z, 0, 0, 0)
		s.SpawnBobberEntity(x, float64(world.GroundLevel), z)
	}
	watchers := s.trackingWatchers()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		endCrystalTracker.frozenPass(s, watchers)
		eyeTracker.frozenPass(s, watchers)
		bobberTracker.frozenPass(s, watchers)
	}
}
