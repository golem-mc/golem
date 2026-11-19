package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// drownedPond joins a creative player (no mob targets them),
// freezes the loop and fills the box x 1..9, z -4..4, y bottom..top with
// water inside a stone shell (overworld) or a shell open to the west bank (flat).
// The flat pond has no west wall, so it opens on the bank beside it.
func drownedPond(t *testing.T, bottom, top int32, cfg *config.Config) (*server.Server, *servertest.Client) {
	t.Helper()
	overworld := cfg.LevelType == "normal"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Diver"), func(s *server.Server) {
		// Join beside the pond instead of streaming the terrain spawn first.
		if overworld {
			s.SetWorldSpawn(world.SpawnPos{X: -3, Y: 70, Z: 0})
		}
	})
	srv.SetDifficulty(3)
	c := connect()
	c.JoinAs("Diver")
	c.ChatCommand("gamemode creative")
	// Keep the observer beside the pond and inside entity tracking range.
	c.ChatCommand("tp Diver -3 70 0")
	c.Settle()
	servertest.AwaitChunksSettled(t, srv)
	srv.StopTick()
	// The teleport acknowledgement and the stream run independently; pin
	// both pond chunks before writing, even if the view has not caught up.
	for _, z := range []int32{-16, 0} {
		t.Cleanup(srv.HoldChunk(0, z))
	}
	water, _ := registry.FindState("minecraft:water", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(0); x <= 10; x++ {
		for z := int32(-5); z <= 5; z++ {
			for y := bottom - 1; y <= top; y++ {
				if x == 0 && overworld || x == 10 || z == -5 || z == 5 || y == bottom-1 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
					if srv.BlockAt(x, y, z) != water {
						t.Fatalf("pond at %d,%d,%d has state %d, want water", x, y, z, srv.BlockAt(x, y, z))
					}
				}
			}
		}
	}
	c.Settle()
	return srv, c
}

// drownedSeaConfig keeps the noise world's sea level of 63; the goals use a
// hand-built pond, so structures and terrain beyond its view add no coverage.
func drownedSeaConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelType = "normal"
	cfg.LevelSeed = "12345"
	cfg.GenerateStructures = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	// The pond and the observer fit in the centre chunks and their view ring.
	cfg.LevelName = (servertest.RegionFixture{
		Seed: 12345,
		Min:  world.ChunkPos{X: -3, Z: -2}, Max: world.ChunkPos{X: 1, Z: 2},
	}).Copy(t)
	return cfg
}

// TestADrownedSwimsUpFromTheDeepAtNight leaves a drowned with no target at
// the bottom of a pond twelve blocks under the sea at night:
// DrownedSwimUpGoal sets searchingForLand, which lifts it and swims it
// toward the surface.
func TestADrownedSwimsUpFromTheDeepAtNight(t *testing.T) {
	t.Parallel()
	srv, c := drownedPond(t, 50, 66, drownedSeaConfig(t))
	srv.SetDayTime(18000)
	id := srv.SpawnMob("minecraft:drowned", 5.5, 51, 0.5)
	drowned, _ := registry.EntityTypeID("minecraft:drowned")
	c.ExpectMobSpawn(drowned)
	_, startY, _, _ := srv.MobPos(id)
	highest := startY
	for range 100 {
		srv.TickMobs(10)
		_, y, _, ok := srv.MobPos(id)
		if ok && y > highest {
			highest = y
		}
	}
	if highest < startY+5 {
		t.Errorf("the drowned rose from %v to %v in 1000 ticks, want a climb of five or more", startY, highest)
	}
}

// TestADrownedStaysDownByDay is the daytime counterpart: no swim-up goal
// runs, so a drowned with no target is not lifted.
func TestADrownedStaysDownByDay(t *testing.T) {
	t.Parallel()
	srv, c := drownedPond(t, 50, 66, drownedSeaConfig(t))
	srv.SetDayTime(6000)
	id := srv.SpawnMob("minecraft:drowned", 5.5, 51, 0.5)
	drowned, _ := registry.EntityTypeID("minecraft:drowned")
	c.ExpectMobSpawn(drowned)
	highest := 0.0
	for range 100 {
		srv.TickMobs(10)
		_, y, _, ok := srv.MobPos(id)
		if ok && y > highest {
			highest = y
		}
	}
	if highest > 56 {
		t.Errorf("the drowned rose to %v by day with nothing to chase", highest)
	}
}

// TestADrownedWalksToWaterByDay puts a drowned on dry land at noon with a
// pond nearby: DrownedGoToWaterGoal walks it into the water.
func TestADrownedWalksToWaterByDay(t *testing.T) {
	t.Parallel()
	srv, c := drownedPond(t, 63, 63, servertest.HostileTestConfig())
	srv.SetDayTime(6000)
	id := srv.SpawnMob("minecraft:drowned", -6.5, 64, 0.5)
	drowned, _ := registry.EntityTypeID("minecraft:drowned")
	c.ExpectMobSpawn(drowned)
	inPond := false
	for range 60 {
		srv.TickMobs(10)
		if x, y, _, ok := srv.MobPos(id); ok && x > 0 && y < 64 {
			inPond = true
			break
		}
	}
	if !inPond {
		t.Error("the drowned never walked into the pond by day")
	}
}

// TestADrownedStaysInAWalledOneByOnePoolAtNight leaves a drowned in a 1x1
// pool two deep: every water cell has a blocked face, so no vertical step
// enters open water and it plans no climb over the wall.
// vanilla: AmphibiousNodeEvaluator.isVerticalNeighborValid
func TestADrownedStaysInAWalledOneByOnePoolAtNight(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Diver"))
	srv.SetDifficulty(3)
	c := connect()
	c.JoinAs("Diver")
	c.ChatCommand("gamemode creative")
	c.ChatCommand("tp Diver -5 64 1")
	c.Settle()
	servertest.AwaitChunksSettled(t, srv)
	srv.StopTick()
	t.Cleanup(srv.HoldChunk(0, 0))
	glass, _ := registry.FindState("minecraft:glass", nil)
	water, _ := registry.FindState("minecraft:water", nil)
	for x := int32(0); x <= 2; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(0); z <= 2; z++ {
				srv.SetBlock(x, y, z, glass)
			}
		}
	}
	srv.SetBlock(1, 64, 1, water)
	srv.SetBlock(1, 65, 1, water)
	c.Settle()
	srv.SetDayTime(18000)
	id := srv.SpawnMob("minecraft:drowned", 1.5, 64, 1.5)
	drowned, _ := registry.EntityTypeID("minecraft:drowned")
	c.ExpectMobSpawn(drowned)
	for i := range 200 {
		srv.TickMobs(1)
		x, y, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatalf("the drowned disappeared at tick %d", i)
		}
		if x < 1 || x > 2 || z < 1 || z > 2 || y >= 66 {
			t.Fatalf("tick %d: the drowned left the pool, at %v,%v,%v", i, x, y, z)
		}
	}
}
