package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// put sets a block by name and properties.
func put(t *testing.T, srv *server.Server, x, y, z int32, name string, props map[string]string) {
	t.Helper()
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state for %s %v", name, props)
	}
	srv.SetBlock(x, y, z, state)
}

// The lightning rod tests: a thunderstorm's strike goes to a rod in range,
// the rod powers for eight ticks, and a bolt scrapes the copper it lands on.

// TestAThunderstormStrikeLandsOnALightningRod checks the strike's target: the
// bolt stands on the rod instead of over the struck column, the rod powers a
// lamp beside it, and both go off again after eight ticks.
func TestAThunderstormStrikeLandsOnALightningRod(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	put(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"facing": "up"})
	put(t, srv, 31, 64, 30, "minecraft:redstone_lamp", nil)
	thunderstorm(srv)
	srv.SetLightningOdds(1)

	srv.TickRandomTicks(1)
	// The main loop ticks physics before the bolts, so the strike books its
	// release against the current tick.
	srv.TickPhysics(1)
	arrived, _ := boltWatch(c, srv, 1)
	if len(arrived) == 0 {
		t.Fatal("no bolt struck")
	}
	for _, b := range arrived {
		if b.x != 30.5 || b.y != 65 || b.z != 30.5 {
			t.Fatalf("a bolt stands at (%v, %v, %v), want on the rod at (30.5, 65, 30.5)", b.x, b.y, b.z)
		}
	}
	servertest.ExpectBlockNamed(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 31, 64, 30, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	srv.TickPhysics(7)
	servertest.ExpectBlockNamed(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"powered": "true"})
	srv.TickPhysics(1)
	servertest.ExpectBlockNamed(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"powered": "false"})
	srv.TickPhysics(4)
	servertest.ExpectBlockNamed(t, srv, 31, 64, 30, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestAThunderstormStrikeReleasesAWaterloggedRod checks that the release tick
// of a waterlogged rod runs beside the fluid visit its cell also takes.
func TestAThunderstormStrikeReleasesAWaterloggedRod(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	put(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"facing": "up", "waterlogged": "true"})
	thunderstorm(srv)
	srv.SetLightningOdds(1)

	srv.TickRandomTicks(1)
	srv.TickPhysics(1)
	arrived, _ := boltWatch(c, srv, 1)
	if len(arrived) == 0 {
		t.Fatal("no bolt struck")
	}
	servertest.ExpectBlockNamed(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"powered": "true", "waterlogged": "true"})
	srv.TickPhysics(8)
	servertest.ExpectBlockNamed(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"powered": "false", "waterlogged": "true"})
}

// TestAThunderstormStrikeSkipsARodUnderABlock checks the rod search's column
// test: a rod with a block over it is not the top of its column, so the
// strike goes where it would have gone without it.
func TestAThunderstormStrikeSkipsARodUnderABlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	put(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"facing": "up"})
	put(t, srv, 30, 65, 30, "minecraft:stone", nil)
	thunderstorm(srv)
	srv.SetLightningOdds(1)

	srv.TickRandomTicks(1)
	arrived, _ := boltWatch(c, srv, 1)
	if len(arrived) == 0 {
		t.Fatal("no bolt struck")
	}
	for _, b := range arrived {
		if b.x == 30.5 && b.z == 30.5 {
			t.Fatalf("a bolt stands on the covered rod at (%v, %v, %v)", b.x, b.y, b.z)
		}
	}
}

// TestALightningRodStrongPowersTheBlockItStandsOn checks the direct signal:
// the rod gives its 15 into the block behind it, and a lamp beside that block
// reads it through the conductor.
func TestALightningRodStrongPowersTheBlockItStandsOn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	put(t, srv, 30, 63, 30, "minecraft:stone", nil)
	put(t, srv, 30, 64, 30, "minecraft:lightning_rod", map[string]string{"facing": "up"})
	put(t, srv, 31, 63, 30, "minecraft:redstone_lamp", nil)
	srv.TickPhysics(1)

	srv.StrikeLightning(30.5, 65, 30.5)
	boltWatch(c, srv, 1)
	servertest.ExpectBlockNamed(t, srv, 31, 63, 30, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
}

// TestALightningBoltScrapesTheCopperItLandsOn checks the strike's copper
// rule: weathered copper under the bolt goes back to the first stage, waxed
// copper stays as it is.
func TestALightningBoltScrapesTheCopperItLandsOn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	put(t, srv, 30, 64, 30, "minecraft:oxidized_copper", nil)
	put(t, srv, 40, 64, 30, "minecraft:waxed_oxidized_copper", nil)

	srv.StrikeLightning(30.5, 65, 30.5)
	srv.StrikeLightning(40.5, 65, 30.5)
	boltWatch(c, srv, 1)
	servertest.ExpectBlockNamed(t, srv, 30, 64, 30, "minecraft:copper_block", nil)
	servertest.ExpectBlockNamed(t, srv, 40, 64, 30, "minecraft:waxed_oxidized_copper", nil)
}

// TestLightningCleansRodsWithoutLosingTheirProperties covers each copper stage
// and its waxed counterpart through activation and the booked release.
func TestLightningCleansRodsWithoutLosingTheirProperties(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	variants := []struct{ before, after string }{
		{"lightning_rod", "lightning_rod"},
		{"exposed_lightning_rod", "lightning_rod"},
		{"weathered_lightning_rod", "lightning_rod"},
		{"oxidized_lightning_rod", "lightning_rod"},
		{"waxed_lightning_rod", "waxed_lightning_rod"},
		{"waxed_exposed_lightning_rod", "waxed_exposed_lightning_rod"},
		{"waxed_weathered_lightning_rod", "waxed_weathered_lightning_rod"},
		{"waxed_oxidized_lightning_rod", "waxed_oxidized_lightning_rod"},
	}
	position := func(i int) (int32, int32) { return -30 + int32(i%4)*20, 10 + int32(i/4)*20 }
	// Twenty blocks separates rods beyond a copper walk's eight steps of ±1.
	for i, tc := range variants {
		x, z := position(i)
		put(t, srv, x, 64, z, "minecraft:"+tc.before, map[string]string{"facing": "east", "waterlogged": "true"})
	}
	for i := range variants {
		x, z := position(i)
		srv.StrikeLightning(float64(x)+0.5, 65, float64(z)+0.5)
	}
	srv.TickBolts(1)
	check := func(powered bool) {
		t.Helper()
		for i, tc := range variants {
			x, z := position(i)
			want := servertest.BlockStateWith(t, "minecraft:"+tc.after, "facing=east,powered=false,waterlogged=true")
			want, _ = registry.WithBool(want, registry.PropPowered, powered)
			if got := srv.BlockAt(x, 64, z); got != want {
				t.Errorf("%s (powered=%v): state = %d, want %d", tc.before, powered, got, want)
			}
		}
	}
	check(true)
	c.ReadTicks(srv, 7, func(int32, []byte) bool { return false })
	check(true)
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	check(false)
}
