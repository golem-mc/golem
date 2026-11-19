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
