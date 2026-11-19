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

func TestGrassDyingPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	watchRandomWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:grass_block", map[string]string{"snowy": "false"})
		servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:stone", nil)
	}, func(srv *server.Server) bool {
		return servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)) == "minecraft:dirt"
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

// vanilla: MangroveLeavesBlock.performBonemeal sets the propagule with flag 2.
func TestBoneMealedMangroveLeavesPulseTheObserverUnderThem(t *testing.T) {
	t.Parallel()
	watchBoneMealWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:mangrove_leaves", map[string]string{"persistent": "true"})
	}, 5, 65, 0, 5, 64, 0, "a propagule")
}

// vanilla: CropBlock.growCrops sets the torchflower with flag 2.
func TestBoneMealedTorchflowerCropPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	watchBoneMealWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:torchflower_crop", map[string]string{"age": "1"})
	}, 5, 64, 0, 5, 64, 0, "a torchflower")
}

func TestFrostedIceAgingPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, cropsSeed)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:frosted_ice", map[string]string{"age": "0"})
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:glowstone", nil)
	servertest.Setblock(t, c, 6, 64, 0, "observer[facing=west]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 0); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	frozen := servertest.CropsStateOf(t, "minecraft:frosted_ice", map[string]string{"age": "0"})
	pulsed := false
	// The observer pulses a tick or two after the write, so keep polling
	// past the first age step.
	for range 400 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 0); powered {
			pulsed = true
		}
		if pulsed || servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)) != "minecraft:frosted_ice" {
			break
		}
	}
	if srv.BlockAt(5, 64, 0) == frozen {
		t.Fatal("the frosted ice never aged")
	}
	if !pulsed {
		t.Error("the ice aged without pulsing the observer beside it")
	}
}

func TestLeafDistanceChangePulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, cropsSeed)
	leaf := servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_leaves", map[string]string{"distance": "7", "persistent": "true"})
	servertest.Setblock(t, c, 6, 64, 0, "observer[facing=west]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 0); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:oak_log", map[string]string{"axis": "y"})
	pulsed := false
	for range 20 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 0); powered {
			pulsed = true
		}
	}
	if srv.BlockAt(5, 64, 0) == leaf {
		t.Fatal("the leaf kept its distance beside a log")
	}
	if !pulsed {
		t.Error("the leaf changed distance without pulsing the observer beside it")
	}
}

// vanilla: IceBlock.melt posts block_destroy (frequency 12) at the cell.
func TestFrostedIceMeltingActivatesTheSculkSensorBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, cropsSeed)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:frosted_ice", map[string]string{"age": "0"})
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:glowstone", nil)
	servertest.Setblock(t, c, 6, 64, 0, "sculk_sensor")
	c.Settle()
	srv.TickPhysics(8)
	if _, props := servertest.StatePropsOf(t, srv, 6, 64, 0); props["sculk_sensor_phase"] != "inactive" {
		t.Fatalf("setup: the sensor is %s", props["sculk_sensor_phase"])
	}
	for range 400 {
		srv.TickPhysics(1)
		if servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)) != "minecraft:frosted_ice" {
			break
		}
	}
	if name := servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)); name == "minecraft:frosted_ice" {
		t.Fatal("the frosted ice never melted")
	}
	srv.TickPhysics(4)
	if _, props := servertest.StatePropsOf(t, srv, 6, 64, 0); props["sculk_sensor_phase"] != "active" {
		t.Errorf("the ice melted and the sensor beside it is %s, want active", props["sculk_sensor_phase"])
	}
}

// vanilla: GrowingPlantHeadBlock.performBonemeal sets the new head with setBlockAndUpdate.
func TestBoneMealedKelpPulsesTheObserverBesideTheNewHead(t *testing.T) {
	t.Parallel()
	watchBoneMealWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:sand", nil)
		for y := int32(65); y <= 67; y++ {
			servertest.SetFarmBlock(t, c, srv, 5, y, 0, "minecraft:water", map[string]string{"level": "0"})
		}
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:kelp", map[string]string{"age": "0"})
	}, 5, 64, 0, 5, 65, 0, "the kelp")
}

// vanilla: ShortDryGrassBlock.performBonemeal sets the tall grass with setBlockAndUpdate.
func TestBoneMealedShortGrassPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	watchBoneMealWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:sand", nil)
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:short_dry_grass", nil)
	}, 5, 64, 0, 5, 64, 0, "the dry grass")
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

// vanilla: PitcherCropBlock.grow sets the upper half with setBlockAndUpdate.
func TestBoneMealedPitcherCropPulsesTheObserverBesideTheUpperHalf(t *testing.T) {
	t.Parallel()
	watchBoneMealWrite(t, func(t *testing.T, c *servertest.Client, srv *server.Server) {
		servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
		servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:pitcher_crop", map[string]string{"age": "2", "half": "lower"})
	}, 5, 64, 0, 5, 65, 0, "the pitcher's upper half")
}

// vanilla: DragonEggBlock.teleport sets the new cell and removes the old one.
func TestATeleportingDragonEggPulsesTheObserverItLeaves(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, cropsSeed)
	servertest.Setblock(t, c, 5, 64, 0, "dragon_egg")
	servertest.Setblock(t, c, 6, 64, 0, "observer[facing=west]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 0); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	c.UseBlock(5, 64, 0)
	servertest.AwaitBlockState(t, c, 5, 64, 0)
	for range 6 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, 6, 64, 0); powered {
			return
		}
	}
	t.Error("the egg left its cell without pulsing the observer beside it")
}
