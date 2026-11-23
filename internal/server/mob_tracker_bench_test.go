package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// spawnTrackerBenchMobs stands trackerBenchBodies cows on the tracker
// benchmarks' grid, their AI off so the tick's cost is mostly the tracking.
func spawnTrackerBenchMobs(b *testing.B, s *Server) {
	dim := s.allDims()[0]
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		id := s.spawnMobInDim(dim, entity.TypeCow, x, float64(world.GroundLevel), z, int64(i), nil)
		s.mobsMu.Lock()
		if ent := s.mobByEntityIDLocked(dim.w, id); ent != nil {
			ent.mob.NoAI = true
		}
		s.mobsMu.Unlock()
	}
	for range 20 {
		s.tickMobs()
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if n := len(dim.mobs.herd); n != trackerBenchBodies {
		b.Fatalf("%d mobs in the herd after the settling ticks, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickMobsTracked runs the whole mob tick over cows at rest, every
// one watched by every player.
func BenchmarkTickMobsTracked(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
}

// BenchmarkTickMagmaCubesTracked includes takeoff and landing updates for
// 64 jumping cubes watched by the players around the grid.
func BenchmarkTickMagmaCubesTracked(b *testing.B) {
	benchmarkTickCubesTracked(b, entity.TypeMagmaCube)
}

// BenchmarkTickSlimeAndSulfurCubesTracked includes takeoff and landing updates
// for 64 jumping slimes and sulfur cubes watched by the players around the grid.
func BenchmarkTickSlimeAndSulfurCubesTracked(b *testing.B) {
	benchmarkTickCubesTracked(b, entity.TypeSlime, entity.TypeSulfurCube)
}

func benchmarkTickCubesTracked(b *testing.B, kinds ...entity.MobType) {
	const cubes = 64
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range cubes {
		x, z := trackerBenchSpot(i)
		s.spawnMobArriving(dim, kinds[i%len(kinds)], x, float64(world.GroundLevel), z, int64(i+1), nil, false, "", false, func(ent *mobEntity) {
			ent.mob.SetSize(2, true)
			ent.mob.Persistence = true
		})
	}
	for range 20 {
		s.tickMobs()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
	b.StopTimer()
	if n := len(dim.mobs.herd); n != cubes {
		b.Fatalf("%d cubes in the herd after the run, want %d", n, cubes)
	}
}

// BenchmarkTickMobsSwimming runs the whole mob tick over a school of cod
// pathing round stone pillars in a pond, every one watched by every player.
func BenchmarkTickMobsSwimming(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	water, stone := registry.BlockWater.DefaultState(), registry.BlockStone.DefaultState()
	for x := int32(-16); x < 16; x++ {
		for z := int32(-16); z < 16; z++ {
			pillar := x%4 == 0 && z%4 == 0
			for y := int32(world.GroundLevel - 6); y < world.GroundLevel; y++ {
				if pillar {
					w.SetBlock(x, y, z, stone)
				} else {
					w.SetBlock(x, y, z, water)
				}
			}
		}
	}
	w.FlushLighting()
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		id := s.spawnMobInDim(dim, entity.TypeCod, x, float64(world.GroundLevel-3), z, int64(i), nil)
		s.mobsMu.Lock()
		s.mobByEntityIDLocked(dim.w, id).mob.Persistence = true
		s.mobsMu.Unlock()
	}
	for range 20 {
		s.tickMobs()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
	b.StopTimer()
	if n := len(dim.mobs.herd); n != trackerBenchBodies {
		b.Fatalf("%d cod in the herd after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickMobsSeated runs the mob tick over cows riding a vehicle that
// turn every tick, every one watched by every player.
func BenchmarkTickMobsSeated(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	dim := s.allDims()[0]
	s.mobsMu.Lock()
	var seats []*entity.Mob
	for _, ent := range dim.mobs.herd {
		seats = append(seats, ent)
	}
	for i := 0; i+1 < len(seats); i += 2 {
		seats[i].Vehicle = seats[i+1]
	}
	s.mobsMu.Unlock()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s.mobsMu.Lock()
		for j := 0; j+1 < len(seats); j += 2 {
			ent := seats[j]
			ent.Yaw = float32(i % 360)
			ent.HeadYaw = float32(i % 360)
		}
		s.mobsMu.Unlock()
		s.tickMobs()
	}
}

// BenchmarkFrozenMobTracking runs the frozen world's mob tracking pass over
// every cow, with every player in range.
func BenchmarkFrozenMobTracking(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobTracking(s.trackingWatchers())
	}
}

// hopTrackerBenchMobs moves every cow 10 blocks up or back down, too far for
// a relative move, so the next movement tick tells it absolute.
func hopTrackerBenchMobs(s *Server, i int) {
	dim := s.allDims()[0]
	y := float64(world.GroundLevel + 10*(i%2))
	s.mobsMu.Lock()
	for _, m := range dim.mobs.herd {
		m.SetPos(m.X, y, m.Z)
		dim.mobs.book[m].teleported = true
	}
	s.mobsMu.Unlock()
}

// BenchmarkTickMobsTeleported runs the mob tick over cows teleported every
// tick, so each movement tick sends every watcher an absolute sync.
func BenchmarkTickMobsTeleported(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		hopTrackerBenchMobs(s, i)
		s.tickMobs()
	}
}

// BenchmarkFrozenMobTeleports runs the frozen world's mob tracking pass over
// cows teleported every tick, each told absolute to every watcher.
func BenchmarkFrozenMobTeleports(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		hopTrackerBenchMobs(s, i)
		s.tickMobTracking(s.trackingWatchers())
	}
}

// BenchmarkTickCloudsTracked runs the whole cloud tick over clouds that
// shrink every tick and never run out, every one watched by every player.
func BenchmarkTickCloudsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnCloud(w, x, float64(world.GroundLevel), z, nil, 0, protocol.PotionContents{}, rules.LingeringDurationScale, uuid.Nil)
	}
	s.arrowsMu.Lock()
	for _, it := range s.clouds {
		it.cloud.Duration = -1
		it.cloud.WaitTime = 0
		it.cloud.Restore(0, 1000, -1e-3)
	}
	s.arrowsMu.Unlock()
	for range 20 {
		s.tickClouds()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickClouds()
	}
	b.StopTimer()
	if n := len(s.clouds); n != trackerBenchBodies {
		b.Fatalf("%d clouds after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickCloudsInstants runs the cloud tick over clouds with an
// instant heal and a timed effect, each landing on the cow beside it every
// tick.
func BenchmarkTickCloudsInstants(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	health, slow := registry.MustMobEffect("minecraft:instant_health"), registry.MustMobEffect("minecraft:slowness")
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnCloud(w, x, float64(world.GroundLevel), z, []effect.Instance{
			effect.New(health, 1, 0, false, true, true), effect.New(slow, 200, 0, false, true, true),
		}, 0, protocol.PotionContents{}, rules.LingeringDurationScale, uuid.Nil)
	}
	s.arrowsMu.Lock()
	for _, it := range s.clouds {
		it.cloud.Duration, it.cloud.WaitTime, it.cloud.Reapplication, it.cloud.RadiusOnUse = -1, 0, 1, 0
		it.cloud.Restore(0, 1, 0)
	}
	s.arrowsMu.Unlock()
	for range 20 {
		s.tickClouds()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickClouds()
	}
}

// BenchmarkTickMobsPufferfishTouch runs the whole mob tick with a puffed NoAI
// pufferfish standing in a player's box among the tracker cows, so every tick
// collects a touch and walks the world's players for it.
func BenchmarkTickMobsPufferfishTouch(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	spawnTrackerBenchMobs(b, s)
	dim := s.allDims()[0]
	var victim *client
	s.mu.RLock()
	for _, c := range s.clients {
		victim = c
		break
	}
	s.mu.RUnlock()
	pos := victim.player.Position()
	id := s.spawnMobInDim(dim, entity.TypePufferfish, pos.X, pos.Y, pos.Z, 1, nil)
	s.mobsMu.Lock()
	fish := s.mobByEntityIDLocked(dim.w, id).mob
	fish.NoAI, fish.Silent = true, true
	fish.SetPuffState(entity.PuffFull)
	s.mobsMu.Unlock()
	refill := func() {
		victim.player.UpdateVitals(func(v *player.Vitals) { v.Health, v.Dead = 20, false })
	}
	refill()
	s.tickMobs()
	if h := victim.player.Vitals().Health; h >= 20 {
		b.Fatalf("health %v after a tick next to a puffed pufferfish, want a sting", h)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		refill()
		s.tickMobs()
	}
}
