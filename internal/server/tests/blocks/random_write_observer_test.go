package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

const cropsSeed = int64(21)

// watchRandomWrite lays the cells of a random-tick write with the observer
// facing the watched cell, runs the dice until done reports the write, and
// fails the test unless the observer pulsed.
//
// vanilla: Level.setBlock runs the shape pass for every flag set without 16.
func watchRandomWrite(t *testing.T, lay func(t *testing.T, c *servertest.Client, srv *server.Server), watched func(srv *server.Server) bool, ox, oy, oz int32, facing string) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, cropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), growthTickSpeed)

	lay(t, c, srv)
	servertest.Setblock(t, c, ox, oy, oz, "observer[facing="+facing+"]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	// The lamp coordinates are unused here: the lamp check is the pulse count.
	pulseTicks, _, _ := followRandomGrowthPulse(t, srv, func() bool { return watched(srv) }, ox, oy, oz, ox, oy+5, oz)
	if !watched(srv) {
		t.Fatal("the write the observer watches never came")
	}
	if pulseTicks == 0 {
		t.Error("the write happened without pulsing the observer beside it")
	}
}

func TestLeafDecayPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	watchRandomWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_leaves", map[string]string{"distance": "7", "persistent": "false"})
	}, func(srv *server.Server) bool {
		return srv.BlockAt(5, 64, 0) == 0
	}, 6, 64, 0, "west")
}

func TestGrassSpreadingPulsesTheObserverBesideTheDirt(t *testing.T) {
	t.Parallel()
	watchRandomWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:grass_block", map[string]string{"snowy": "false"})
		servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:dirt", nil)
	}, func(srv *server.Server) bool {
		return servertest.CropsNameOf(t, srv.BlockAt(6, 64, 0)) == "minecraft:grass_block"
	}, 7, 64, 0, "west")
}

func TestIceMeltingPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	watchRandomWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:ice", nil)
		servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:glowstone", nil)
	}, func(srv *server.Server) bool {
		return servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)) != "minecraft:ice"
	}, 6, 64, 0, "west")
}

// watchBoneMealWrite lays the cells, bonemeals the block at (ux, uy, uz) with
// an observer at (wx+1, wy, wz) facing the cell (wx, wy, wz) the growth writes,
// and fails the test unless the observer pulsed.
func watchBoneMealWrite(t *testing.T, lay func(t *testing.T, c *servertest.Client, srv *server.Server), ux, uy, uz, wx, wy, wz int32, what string) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, cropsSeed)
	lay(t, c, srv)
	servertest.Setblock(t, c, wx+1, wy, wz, "observer[facing=west]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, wx+1, wy, wz); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")
	c.UseBlock(ux, uy, uz)
	servertest.AwaitBlockState(t, c, wx, wy, wz)

	pulsed := false
	for range 4 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, wx+1, wy, wz); powered {
			pulsed = true
		}
	}
	if !pulsed {
		t.Errorf("the bone meal grew %s without pulsing the observer beside it", what)
	}
}

func TestBoneMealGrowthPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	watchBoneMealWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:wheat", map[string]string{"age": "0"})
	}, 5, 64, 0, 5, 64, 0, "the wheat")
}

// vanilla: ServerLevel.tickPrecipitation sets the layer with setBlockAndUpdate.
func TestSnowfallPulsesTheObserverBesideTheNewLayer(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 2
	servertest.SnowyFlatWorld(t, cfg)
	srv, c := servertest.Join(t, cfg, "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 120 {
		srv.TickWeather()
	}
	servertest.SeedPhysics(t, srv, cropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), growthTickSpeed)

	// A bottom-slab roof refuses the layers, so only the hole at (2, 64, 2)
	// can take one.
	slab := registry.MustState("minecraft:stone_slab", map[string]string{"type": "bottom"})
	for x := range int32(16) {
		for z := range int32(16) {
			if x != 2 || z != 2 {
				srv.SetBlock(x, 70, z, slab)
			}
		}
	}
	servertest.Setblock(t, c, 3, 64, 2, "observer[facing=west]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 3, 64, 2); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	pulseTicks, _, _ := followRandomGrowthPulse(t, srv, func() bool {
		return servertest.CropsNameOf(t, srv.BlockAt(2, 64, 2)) == "minecraft:snow"
	}, 3, 64, 2, 3, 69, 2)
	if pulseTicks == 0 {
		t.Error("the snow fell without pulsing the observer beside it")
	}
}
