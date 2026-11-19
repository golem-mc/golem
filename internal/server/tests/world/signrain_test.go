package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestRainFallsThroughASign: a sign is not in blocks_motion_in_heightmap, so
// the MOTION_BLOCKING heightmap of its column stops at the ground below it,
// and rain reaches a fire burning under it.
func TestRainFallsThroughASign(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)

	// A one-block pit in the flat ground (top at y=63) floored with a log, so
	// the fire neither burns out on a bare floor nor at age 15; the pit's
	// walls keep the rain off its four neighbours, so only its own column
	// can douse it. A wall sign on stone covers the pit.
	sign, ok := registry.WithProp(registry.BlockOakWallSign.DefaultState(), registry.PropFacing, "west")
	if !ok {
		t.Fatal("oak_wall_sign has no facing=west")
	}
	log := registry.BlockOakLog.DefaultState()
	srv.SetLoadedBlock(6, 62, 3, log)
	srv.SetLoadedBlock(6, 63, 3, 0)
	srv.SetLoadedBlock(7, 64, 3, registry.BlockStone.DefaultState())
	srv.SetLoadedBlock(6, 64, 3, sign)

	c := connect()
	c.Login("Sheltered")
	c.Configure()
	maps, _ := parseHeightmaps(t, chunkAt(t, c, 0, 0))
	column := 3*16 + 6
	// The log at y=62: 62 - (-64) + 1.
	if got := maps[4][column]; got != 127 {
		t.Errorf("MOTION_BLOCKING under the sign = %d, want the log's 127", got)
	}
	if got := maps[5][column]; got != 127 {
		t.Errorf("MOTION_BLOCKING_NO_LEAVES under the sign = %d, want the log's 127", got)
	}
	if got := maps[1][column]; got != 129 {
		t.Errorf("WORLD_SURFACE under the sign = %d, want the sign's 129", got)
	}

	servertest.HoldTicks(t, srv)
	// Scheduled fire ticks share their dice with random chunk ticks. Isolate
	// them and seed before placement, which books the first fire tick.
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 0)
	srv.SetWeatherState(false, false, 12000, 0, 12000)
	servertest.SeedPhysics(t, srv, 2)
	srv.SetBlockAndTell(6, 63, 3, registry.BlockFire.DefaultState())
	// Rain reads skylight; held ticks must not outrun the light worker.
	srv.WaitRelights()
	servertest.RunTicks(t, srv, 40)
	if registry.BlockOf(srv.BlockAt(6, 63, 3)) != registry.BlockFire {
		t.Fatal("the dry control fire did not survive its first scheduled tick")
	}
	if got := srv.BlockAt(6, 62, 3); got != log {
		t.Fatalf("the dry control burned its supporting log: got %d, want %d", got, log)
	}
	srv.SetBlockAndTell(6, 63, 3, 0)
	srv.WaitRelights()
	srv.SetWeatherState(true, false, 12000, 0, 0)
	// Fade to full rain and drain the removed control fire's pending tick.
	servertest.RunTicks(t, srv, 100)
	if !srv.RainingAtInDimension(world.DimensionName, 6, 63, 3) {
		t.Fatalf("rain does not reach the pit under the sign: raining=%v, sky light=%d, motion height=%d",
			srv.IsRaining(), srv.SkyLightAt(6, 63, 3), srv.OverworldWorld().MotionBlockingHeight(6, 3))
	}
	for _, d := range [][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		if srv.RainingAtInDimension(world.DimensionName, 6+d[0], 63, 3+d[1]) {
			t.Fatal("rain reaches a horizontal neighbour of the pit")
		}
	}
	// This seed douses on the first visit, before a burn-out can consume the
	// log. Vanilla allows the log to burn if the rain die loses.
	// vanilla: FireBlock.tick, FireBlock.checkBurnOut
	servertest.SeedPhysics(t, srv, 2)
	srv.SetBlockAndTell(6, 63, 3, registry.BlockFire.DefaultState())
	srv.WaitRelights()
	servertest.RunTicks(t, srv, 1)
	if registry.BlockOf(srv.BlockAt(6, 63, 3)) != registry.BlockFire {
		t.Fatal("the fire did not light in the pit")
	}
	servertest.RunTicks(t, srv, 600)

	if got := registry.BlockOf(srv.BlockAt(6, 63, 3)); got == registry.BlockFire {
		t.Error("the fire under the sign still burns after 600 ticks of rain")
	}
	if got := srv.BlockAt(6, 62, 3); got != log {
		t.Errorf("the log under the fire = %d, want it unburnt (%d): the rain, not the burn, put the fire out", got, log)
	}
	if got := srv.BlockAt(6, 64, 3); got != sign {
		t.Errorf("the sign = %d, want it standing (%d)", got, sign)
	}
}
