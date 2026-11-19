package server

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickBurningVehiclesTracked measures lava hurt, metadata and sound
// collection with the same bodies/watchers as the dry vehicle benchmarks.
func BenchmarkTickBurningVehiclesTracked(b *testing.B) {
	for _, kind := range []string{"boat", "minecart"} {
		b.Run(kind, func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			lava := registry.MustState("minecraft:lava", nil)
			resets := make([]func(), 0, trackerBenchBodies)
			for i := range trackerBenchBodies {
				x, z := trackerBenchSpot(i)
				w.SetBlock(int32(math.Floor(x)), world.GroundLevel, int32(math.Floor(z)), lava)
				if kind == "boat" {
					it := s.spawnBoat(w, "minecraft:oak_boat", x, float64(world.GroundLevel), z, float32(i*7), customName{})
					resets = append(resets, func() {
						it.boat.X, it.boat.Y, it.boat.Z = x, float64(world.GroundLevel), z
						it.boat.VX, it.boat.VY, it.boat.VZ = 0, 0, 0
						it.boat.HurtDamage = -100
					})
				} else {
					it := s.spawnMinecart(w, entity.MinecartPlain, x, float64(world.GroundLevel), z)
					resets = append(resets, func() {
						it.cart.X, it.cart.Y, it.cart.Z = x, float64(world.GroundLevel), z
						it.cart.VX, it.cart.VY, it.cart.VZ = 0, 0, 0
						it.cart.HurtDamage = -100
					})
				}
			}
			// Leave room below the break threshold for repeated block-effect hurts:
			// every measured tick must still land hurt and collect its effects.
			// No main loop runs in this fixture, so resets need no body locks.
			tick := func() {
				for _, reset := range resets {
					reset()
				}
				if kind == "boat" {
					s.tickBoats()
				} else {
					s.tickMinecartTracking()
					s.tickMinecarts()
				}
			}
			for range 40 {
				tick()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tick()
			}
			b.StopTimer()
			n := len(s.boats) + len(s.minecarts)
			if n != trackerBenchBodies {
				b.Fatalf("%d vehicles after the run, want %d", n, trackerBenchBodies)
			}
		})
	}
}
