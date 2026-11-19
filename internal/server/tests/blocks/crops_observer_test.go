package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// The observer watching the farm blocks grow: a random tick's growth is a
// plain setBlock — the crop's age a flag-2 write, the cane's, the cactus's
// and the bamboo's new block a flag-3 one — and every write without flag 16
// runs the neighbour shape pass Level.setBlock ties to it, which is the
// observer's whole way of knowing. The dice are raised past the farm tests' fifty so a
// few passes land the growth, and each pulse is proved by the lamp the
// observer's output lights.
// vanilla: CropBlock.randomTick, SugarCaneBlock.randomTick,
// CactusBlock.randomTick, BambooStalkBlock.randomTick,
// BambooSaplingBlock.randomTick; Level.setBlock, ObserverBlock.updateShape.

// growthTickSpeed is the random tick rule the growth pulses run at: dice
// enough that a landing on the one watched cell comes within a few passes.
const growthTickSpeed = 4000

// followRandomGrowthPulse runs random tick passes with the physics ticks
// their bookings drain on after each one, until grown reports the growth
// the observer watches, then drives the visits left on the queue. It
// reports how many of those physics ticks the observer at (ox, oy, oz)
// stood powered, and whether the lamp at (lx, ly, lz) behind it was seen
// lit and later dark. grown is read once the pass's five ticks have run.
func followRandomGrowthPulse(t *testing.T, srv *server.Server, grown func() bool, ox, oy, oz, lx, ly, lz int32) (pulseTicks int, lampOn, lampOff bool) {
	t.Helper()
	poll := func() {
		if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
			pulseTicks++
		}
		if lit := servertest.LampLitAt(srv, lx, ly, lz); lit {
			lampOn = true
		} else if lampOn {
			lampOff = true
		}
	}
	for range 60 {
		srv.TickRandomTicks(1)
		for range 5 {
			srv.TickPhysics(1)
			poll()
		}
		if grown() {
			// No dice are left; the booked on and off edges run out here,
			// the lamp keeping its light four ticks past the pulse's end.
			for range 9 {
				srv.TickPhysics(1)
				poll()
			}
			return pulseTicks, lampOn, lampOff
		}
	}
	t.Fatal("the growth the observer watches never came")
	return 0, false, false
}

// TestWheatRipeningPulsesTheObserverBesideIt checks the observer farm's
// crop case: a wheat beside an observer[facing=west] ripens under the
// random ticks, and the age writes pulse the observer through their shape
// pass, the lamp behind it lighting on each pulse and going dark again.
func TestWheatRipeningPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), growthTickSpeed)

	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			servertest.SetFarmBlock(t, c, srv, 5+dx, 63, dz, "minecraft:farmland", map[string]string{"moisture": "7"})
		}
	}
	servertest.SetFarmBlock(t, c, srv, 2, 63, 0, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:wheat", map[string]string{"age": "0"})
	servertest.Setblock(t, c, 7, 64, 0, "redstone_lamp")
	servertest.Setblock(t, c, 6, 64, 0, "observer[facing=west]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 0); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	ripe := func() bool {
		return servertest.CropsProps(t, srv.BlockAt(5, 64, 0), "minecraft:wheat")["age"] == "7"
	}
	pulseTicks, lampOn, lampOff := followRandomGrowthPulse(t, srv, ripe, 6, 64, 0, 7, 64, 0)

	if !ripe() {
		t.Fatal("the wheat never ripened under the raised dice")
	}
	if pulseTicks == 0 {
		t.Error("the wheat ripened without ever pulsing the observer watching it")
	}
	if !lampOn || !lampOff {
		t.Errorf("the lamp behind the observer lit=%v dark=%v, want it to light and go dark on each pulse", lampOn, lampOff)
	}
}

// TestTheGrowingCanePulsesTheObserverAboveIt checks the cane's growth: a
// ripe cane grows into the cell under an observer[facing=down], and the
// growth write's shape pass pulses the observer for the two game ticks
// vanilla's pulse lasts, the lamp above lighting and going dark on it.
func TestTheGrowingCanePulsesTheObserverAboveIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), growthTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:sand", nil)
	servertest.SetFarmBlock(t, c, srv, 4, 63, 0, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:sugar_cane", map[string]string{"age": "15"})
	servertest.Setblock(t, c, 5, 67, 0, "redstone_lamp")
	servertest.Setblock(t, c, 5, 66, 0, "observer[facing=down]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 5, 66, 0); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	grown := func() bool {
		return servertest.CropsNameOf(t, srv.BlockAt(5, 65, 0)) == "minecraft:sugar_cane"
	}
	pulseTicks, lampOn, lampOff := followRandomGrowthPulse(t, srv, grown, 5, 66, 0, 5, 67, 0)

	if !grown() {
		t.Fatal("the cane never grew into the cell under the observer")
	}
	if pulseTicks != 2 {
		t.Errorf("the cane's growth pulsed the observer for %d ticks, want the two-tick pulse", pulseTicks)
	}
	if !lampOn || !lampOff {
		t.Errorf("the lamp above the observer lit=%v dark=%v, want it to light and go dark on the pulse", lampOn, lampOff)
	}
}
