package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// The observer watching a fire come and go: lava lighting it, a fire
// spreading into the cell and a fire burning out are Level.setBlock writes
// without flag 16, so the neighbour shape pass runs and an observer facing
// the cell pulses, and so does the flag-260 aging write of a fire.
// vanilla: LavaFluid.randomTick, FireBlock.tick; Level.setBlock,
// ObserverBlock.updateShape.

// fireWatchSetup joins an operator, stops the loop, pins the dice and lays
// the observer at (ox, oy, oz) with the lamp at (lx, ly, lz) behind it, then
// lets the placement's own pulse run out.
func fireWatchSetup(t *testing.T, observer string, ox, oy, oz, lx, ly, lz int32) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	servertest.SeedPhysics(t, srv, fireSeed)
	servertest.Setblock(t, c, lx, ly, lz, "redstone_lamp")
	servertest.Setblock(t, c, ox, oy, oz, observer)
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	return srv, c
}

// awaitFirePulse runs step until happened reports the change the observer
// watches, then lets the pulse run its course and reports whether the
// observer stood powered and the lamp lit.
func awaitFirePulse(t *testing.T, srv *server.Server, step func(), happened func() bool, ox, oy, oz, lx, ly, lz int32) (pulsed, lampLit bool) {
	t.Helper()
	for range 4000 {
		step()
		if happened() {
			for range 9 {
				srv.TickPhysics(1)
				if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
					pulsed = true
				}
				if servertest.LampLitAt(srv, lx, ly, lz) {
					lampLit = true
				}
			}
			return pulsed, lampLit
		}
	}
	t.Fatal("the change the observer watches never came")
	return false, false
}

// TestAFireBurningOutPulsesTheObserverWatchingIt stands a fire alone on the
// ground at (7,64,3) with an observer[facing=west] at (8,64,3): with nothing
// to eat it goes out once it is old enough, and the removal's shape pass
// pulses the observer.
func TestAFireBurningOutPulsesTheObserverWatchingIt(t *testing.T) {
	t.Parallel()
	srv, c := fireWatchSetup(t, "observer[facing=west]", 8, 64, 3, 9, 64, 3)
	servertest.Setblock(t, c, 7, 64, 3, "fire")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 8, 64, 3); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	pulsed, lamp := awaitFirePulse(t, srv, func() { srv.TickPhysics(1) },
		func() bool { return fireAge(srv.BlockAt(7, 64, 3)) < 0 }, 8, 64, 3, 9, 64, 3)
	if !pulsed || !lamp {
		t.Errorf("the fire burned out without a pulse: observer=%v lamp=%v", pulsed, lamp)
	}
}

// TestAFireSpreadingIntoTheWatchedCellPulsesTheObserver puts a fire at
// (6,64,3) on netherrack, which never lets it die, beside the empty watched
// cell (7,64,3) with planks over it, and an observer[facing=west] at (8,64,3): the fire spreads into the cell, and the
// write's shape pass pulses the observer.
func TestAFireSpreadingIntoTheWatchedCellPulsesTheObserver(t *testing.T) {
	t.Parallel()
	srv, c := fireWatchSetup(t, "observer[facing=west]", 8, 64, 3, 9, 64, 3)
	servertest.Setblock(t, c, 7, 65, 3, "oak_planks")
	servertest.Setblock(t, c, 6, 63, 3, "netherrack")
	servertest.Setblock(t, c, 6, 64, 3, "fire")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 8, 64, 3); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	pulsed, lamp := awaitFirePulse(t, srv, func() { srv.TickPhysics(1) },
		func() bool { return fireAge(srv.BlockAt(7, 64, 3)) >= 0 }, 8, 64, 3, 9, 64, 3)
	if !pulsed || !lamp {
		t.Errorf("the fire spread into the watched cell without a pulse: observer=%v lamp=%v", pulsed, lamp)
	}
}

// TestLavaLightingTheWatchedCellPulsesTheObserver lays a lava source at
// (6,64,3) beside planks at (7,64,3) and an observer[facing=down] at
// (7,66,3) over the cell (7,65,3): the lava's random tick lights the cell,
// and the write's shape pass pulses the observer.
func TestLavaLightingTheWatchedCellPulsesTheObserver(t *testing.T) {
	t.Parallel()
	srv, c := fireWatchSetup(t, "observer[facing=down]", 7, 66, 3, 7, 67, 3)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), growthTickSpeed)
	servertest.Setblock(t, c, 7, 64, 3, "oak_planks")
	servertest.Setblock(t, c, 6, 64, 3, "lava")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 7, 66, 3); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	pulsed, lamp := awaitFirePulse(t, srv,
		func() { srv.TickRandomTicks(1); srv.TickPhysics(1) },
		func() bool { return fireAge(srv.BlockAt(7, 65, 3)) >= 0 }, 7, 66, 3, 7, 67, 3)
	if !pulsed || !lamp {
		t.Errorf("the lava lit the watched cell without a pulse: observer=%v lamp=%v", pulsed, lamp)
	}
}

// TestAFireAgingPulsesTheObserverWatchingIt stands a fire on netherrack at
// (7,64,3), which never lets it die, with an observer[facing=west] at
// (8,64,3): the aging write's shape pass pulses the observer.
func TestAFireAgingPulsesTheObserverWatchingIt(t *testing.T) {
	t.Parallel()
	srv, c := fireWatchSetup(t, "observer[facing=west]", 8, 64, 3, 9, 64, 3)
	servertest.Setblock(t, c, 7, 63, 3, "netherrack")
	servertest.Setblock(t, c, 7, 64, 3, "fire")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 8, 64, 3); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	pulsed, lamp := awaitFirePulse(t, srv, func() { srv.TickPhysics(1) },
		func() bool { return fireAge(srv.BlockAt(7, 64, 3)) > 0 }, 8, 64, 3, 9, 64, 3)
	if !pulsed || !lamp {
		t.Errorf("the fire aged without a pulse: observer=%v lamp=%v", pulsed, lamp)
	}
}

// growthTickSpeed is the random tick rule the observer pulses run at: dice
// enough that the lava's tick lands inside the test's wait.
const growthTickSpeed = 4000
