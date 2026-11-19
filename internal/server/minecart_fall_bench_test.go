package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickMinecartsOccupiedLandings measures 200 occupied carts landing
// ten blocks in one tick, including rider damage and tracking for eight watchers.
func BenchmarkTickMinecartsOccupiedLandings(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	type occupied struct {
		cart *minecartEntity
		mob  *mobEntity
		x, z float64
	}
	carts := make([]occupied, 0, trackerBenchBodies)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		cart := s.spawnMinecart(w, entity.MinecartPlain, x, float64(world.GroundLevel), z)
		id := s.spawnMobInDim(s.allDims()[0], entity.TypeCow, x, float64(world.GroundLevel), z, int64(i+1), nil)
		mob := s.mobByEntityID(w, id)
		if !s.mountMobOnMinecart(mob, cart, false) {
			b.Fatal("could not seat the cow")
		}
		carts = append(carts, occupied{cart, mob, x, z})
	}
	land := func() {
		// A fast downward step lands the full ten blocks in this tick. Reset
		// health and the hurt window so every iteration exercises a whole hurt.
		for _, c := range carts {
			c.cart.cart.X, c.cart.cart.Y, c.cart.cart.Z = c.x, float64(world.GroundLevel)+10, c.z
			c.cart.cart.VX, c.cart.cart.VY, c.cart.cart.VZ = 0, -11, 0
			c.cart.cart.OnGround = false
			c.mob.mob.Health, c.mob.mob.InvulnTicks = 10, 0
		}
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	for range 20 {
		land()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		land()
	}
	b.StopTimer()
	for _, c := range carts {
		if !c.cart.cart.OnGround || c.cart.cart.Y != float64(world.GroundLevel) || len(c.cart.mobs) != 1 || c.mob.mob.Dead {
			b.Fatal("the cart did not land with its cow alive aboard")
		}
	}
}
