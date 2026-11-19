package server

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickBoatsTracked runs the whole boat tick over unridden hulls at
// rest on the ground, every one watched by every player.
func BenchmarkTickBoatsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnBoat(w, "minecraft:oak_boat", x, float64(world.GroundLevel), z, float32(i*7), customName{})
	}
	for range 40 {
		s.tickBoats()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBoats()
	}
	b.StopTimer()
	if n := len(s.boats); n != trackerBenchBodies {
		b.Fatalf("%d boats after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickMinecartsTracked runs the whole minecart tick over empty carts
// at rest on the ground, every one watched by every player.
func BenchmarkTickMinecartsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnMinecart(w, entity.MinecartPlain, x, float64(world.GroundLevel), z)
	}
	for range 40 {
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	b.StopTimer()
	if n := len(s.minecarts); n != trackerBenchBodies {
		b.Fatalf("%d minecarts after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickMinecartsCommandPowered runs the whole minecart tick over
// command carts resting on powered activator rails, each running a cheap
// command every fourth tick; game time advances with every tick.
func BenchmarkTickMinecartsCommandPowered(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	rail := registry.MustState("minecraft:activator_rail", map[string]string{"shape": "east_west", "powered": "true"})
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		w.SetBlock(int32(math.Floor(x)), world.GroundLevel, int32(math.Floor(z)), rail)
		it := s.spawnMinecart(w, entity.MinecartCommand, x, float64(world.GroundLevel)+0.0625, z)
		it.command.Program("execute if entity @s", false)
	}
	tick := func() {
		w.TickTime()
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	for range 40 {
		tick()
	}
	lastRuns := func() (sum int64) {
		for _, it := range s.minecarts {
			if it.command != nil && it.command.SuccessCount == 1 {
				sum += it.command.LastExecution
			}
		}
		return sum
	}
	before := lastRuns()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		tick()
	}
	b.StopTimer()
	if b.N >= 4 && lastRuns() <= before {
		b.Fatal("the carts did not run again during the measured ticks")
	}
}

// BenchmarkTickSpawnerMinecartsTracked runs the whole minecart tick over
// spawner carts with a player in range, counting down a delay that outlasts
// the run, every one watched by every player.
func BenchmarkTickSpawnerMinecartsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	// Two players among the carts, within the default 16-block range of
	// most; the rest stay out at the edge.
	near := []player.Position{
		{X: -8, Y: float64(world.GroundLevel), Z: -6},
		{X: 8, Y: float64(world.GroundLevel), Z: 6},
	}
	s.mu.RLock()
	for _, c := range s.clients {
		if len(near) == 0 {
			break
		}
		c.player.SetPosition(near[0])
		near = near[1:]
	}
	s.mu.RUnlock()
	delay := nbt.Compound{"Delay": nbt.Short(32767)}
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		it := s.spawnMinecart(w, entity.MinecartSpawner, x, float64(world.GroundLevel), z)
		it.spawner.LoadSpawnerTag(delay)
	}
	for range 40 {
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	b.StopTimer()
	if n := len(s.minecarts); n != trackerBenchBodies {
		b.Fatalf("%d minecarts after the run, want %d", n, trackerBenchBodies)
	}
	for _, it := range s.minecarts {
		if d, _ := nbt.Get[nbt.Short](it.spawner.SpawnerTag(), "Delay"); d <= 0 {
			b.Fatalf("a cart's delay ran out (%d) after %d ticks; raise it", d, b.N)
		}
	}
}

// BenchmarkTickMinecartsRollingAmongMobs runs the whole minecart tick over
// empty plain carts rolling fast enough to pick a body up, in a world whose
// herd of mobs stands clear of them, so every cart looks for a mob each tick
// and finds none.
func BenchmarkTickMinecartsRollingAmongMobs(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	type spot struct {
		it   *minecartEntity
		x, z float64
	}
	carts := make([]spot, 0, trackerBenchBodies)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		carts = append(carts, spot{s.spawnMinecart(w, entity.MinecartPlain, x, float64(world.GroundLevel), z), x, z})
		s.spawnMobInDim(dim, entity.TypeCow, x+30, float64(world.GroundLevel), z, int64(i+1), nil)
	}
	roll := func() {
		s.minecartsMu.Lock()
		for _, c := range carts {
			c.it.cart.X, c.it.cart.Y, c.it.cart.Z = c.x, float64(world.GroundLevel), c.z
			c.it.cart.VX, c.it.cart.VY, c.it.cart.VZ = 0.4, 0, 0
		}
		s.minecartsMu.Unlock()
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	for range 40 {
		roll()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		roll()
	}
	b.StopTimer()
	for _, c := range carts {
		if v := c.it.cart.VX*c.it.cart.VX + c.it.cart.VZ*c.it.cart.VZ; v < 0.01 || len(c.it.mobs) != 0 {
			b.Fatalf("cart %d: speed² %v, carrying %d mobs; want every cart at 0.1 blocks a tick or more and empty", c.it.cart.ID, v, len(c.it.mobs))
		}
	}
}

// BenchmarkTickMinecartsRollingAmongHerd is the rolling-cart tick with a
// large herd hovering out of every cart's reach.
func BenchmarkTickMinecartsRollingAmongHerd(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	type spot struct {
		it   *minecartEntity
		x, z float64
	}
	carts := make([]spot, 0, trackerBenchBodies)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		carts = append(carts, spot{s.spawnMinecart(w, entity.MinecartPlain, x, float64(world.GroundLevel), z), x, z})
	}
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeCow, float64(i%45)*2-45, float64(world.GroundLevel)+40, float64(i/45)*2-45, int64(i+1), nil)
	}
	roll := func() {
		s.minecartsMu.Lock()
		for _, c := range carts {
			c.it.cart.X, c.it.cart.Y, c.it.cart.Z = c.x, float64(world.GroundLevel), c.z
			c.it.cart.VX, c.it.cart.VY, c.it.cart.VZ = 0.4, 0, 0
		}
		s.minecartsMu.Unlock()
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
	for range 40 {
		roll()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		roll()
	}
	b.StopTimer()
	for _, c := range carts {
		if len(c.it.mobs) != 0 {
			b.Fatalf("cart %d carries %d mobs, want none", c.it.cart.ID, len(c.it.mobs))
		}
	}
}

// BenchmarkTickBoatsAmongHerd is BenchmarkTickBoatsTracked with 2000 cows
// hovering far above the hulls, so no body is ever in reach.
func BenchmarkTickBoatsAmongHerd(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.spawnBoat(w, "minecraft:oak_boat", x, float64(world.GroundLevel), z, float32(i*7), customName{})
	}
	for i := range 2000 {
		s.spawnMobInDim(s.allDims()[0], entity.TypeCow, float64(i%45)*2-45, float64(world.GroundLevel)+40, float64(i/45)*2-45, int64(i+1), nil)
	}
	for range 40 {
		s.tickBoats()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBoats()
	}
}

// BenchmarkTickBodiesBouncing runs the boat, minecart and TNT ticks over
// bodies dropped each iteration, so every body bounces and the deferred
// sound and game event run. Boats and carts land on shelf mushrooms, which
// play a bounce sound to the watchers within reach; TNT lands on slime and
// stays silent.
func BenchmarkTickBodiesBouncing(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	shelf := registry.MustState("minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "0"})
	slime := registry.MustState("minecraft:slime_block", nil)
	// The grid spans 30 blocks; a sound reaches soundRange(1).
	const wx, wz = 0.0, -2.0
	s.mu.Lock()
	for _, c := range s.clients {
		c.player.SetPosition(player.Position{X: wx, Y: float64(world.GroundLevel), Z: wz})
	}
	s.mu.Unlock()
	audible := 0
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		w.SetBlock(int32(math.Floor(x)), world.GroundLevel-1, int32(math.Floor(z)), shelf)
		w.SetBlock(int32(math.Floor(x+1)), world.GroundLevel-1, int32(math.Floor(z)), slime)
		s.spawnBoat(w, "minecraft:oak_boat", x, float64(world.GroundLevel), z, 0, customName{})
		s.spawnMinecart(w, entity.MinecartPlain, x-0.25, float64(world.GroundLevel), z)
		s.spawnTNT(w, x+1, float64(world.GroundLevel), z, 1<<30, 0, "", uuid.Nil, false)
		if r := soundRange(1); (x-wx)*(x-wx)+(z-wz)*(z-wz) < r*r {
			audible++
		}
	}
	if audible == 0 {
		b.Fatal("no bounce is within a watcher's sound range")
	}
	drop := func() {
		for _, it := range s.boats {
			it.boat.Y, it.boat.VY = float64(world.GroundLevel)+0.5, -1
		}
		for _, it := range s.minecarts {
			it.cart.Y, it.cart.VY = float64(world.GroundLevel)+0.5, -1
		}
		for _, it := range s.tnts {
			it.tnt.Y, it.tnt.VY = float64(world.GroundLevel)+0.5, -1
		}
	}
	tick := func() {
		drop()
		s.tickBoats()
		s.tickMinecartTracking()
		s.tickMinecarts()
		s.tickTNT()
	}
	tick()
	for _, it := range s.boats {
		if it.boat.VY <= 0 {
			b.Fatal("a boat did not bounce")
		}
		if rules.BounceSound(w.BlockAt(int32(math.Floor(it.boat.X)), world.GroundLevel-1, int32(math.Floor(it.boat.Z)))) == registry.SoundNone {
			b.Fatal("a boat landed on a block that makes no bounce sound")
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		tick()
	}
}
