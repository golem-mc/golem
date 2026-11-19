package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// The distance and block-entry triggers: the player moves through the world
// and the criterion is read off the player's progress.

// TestTravellingFarThroughTheNetherObtainsSubspaceBubble crosses into the
// nether and back twice: a return 100 blocks from where the player left the
// overworld does not obtain fast_travel, one over 7000 blocks away does.
// vanilla: ServerPlayer.triggerDimensionChangeTriggers, DistanceTrigger
func TestTravellingFarThroughTheNetherObtainsSubspaceBubble(t *testing.T) {
	t.Parallel()
	const fast = "minecraft:nether/fast_travel"
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Traveller")
	srv.MovePlayerToDimension("Traveller", world.DimensionName, 0.5, 64, 0.5)

	srv.MovePlayerToDimension("Traveller", world.NetherName, 0.5, 40, 0.5)
	srv.MovePlayerToDimension("Traveller", world.DimensionName, 100.5, 64, 0.5)
	if srv.AdvancementObtained("Traveller", fast, "travelled") {
		t.Fatal("a return 100 blocks away obtained fast_travel")
	}
	srv.MovePlayerToDimension("Traveller", world.NetherName, 0.5, 40, 0.5)
	srv.MovePlayerToDimension("Traveller", world.DimensionName, 7200.5, 64, 0.5)
	if !srv.AdvancementObtained("Traveller", fast, "travelled") {
		t.Error("a return 7100 blocks from where the player left did not obtain fast_travel")
	}
}

// TestTheNetherCrossingOutlivesADeathInTheNether leaves the overworld at
// the origin, dies and respawns at a point in the nether, and returns over
// 7000 blocks away: the respawned body still knows where it left, so the
// return obtains fast_travel.
// vanilla: ServerPlayer.restoreFrom
func TestTheNetherCrossingOutlivesADeathInTheNether(t *testing.T) {
	t.Parallel()
	const name, fast = "Revenant", "minecraft:nether/fast_travel"
	const x, y, z = 40, 65, 40
	srv, c, entityID := servertest.JoinAs(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))
	srv.MovePlayerToDimension(name, world.DimensionName, 0.5, 64, 0.5)
	srv.MovePlayerToDimension(name, world.NetherName, x, y, z)
	servertest.UntilTicks(t, srv, 1200, "the nether chunk is loaded", func() bool {
		return srv.PlayerDimension(name) == world.NetherName && srv.ChunkLoadedInDimension(world.NetherName, x, z)
	})
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlockInDimension(world.NetherName, x, y-1, z, stone)
	srv.SetBlockInDimension(world.NetherName, x, y, z, 0)
	srv.SetBlockInDimension(world.NetherName, x, y+1, z, 0)
	c.StandAt(x+0.5, y, z+0.5, 0)
	c.MarkLoaded()
	c.Settle()
	c.ChatCommand("spawnpoint @s 40 65 40")
	servertest.UntilTicks(t, srv, 100, "the spawn point is set", func() bool {
		_, _, ok := srv.SpawnPointOf(name)
		return ok
	})

	servertest.KillAndRespawn(t, c, entityID, name)
	if got := srv.PlayerDimension(name); got != world.NetherName {
		t.Fatalf("the respawn left the body in %s, want the nether", got)
	}
	c.Settle()
	srv.MovePlayerToDimension(name, world.DimensionName, 7200.5, 64, 0.5)
	if !srv.AdvancementObtained(name, fast, "travelled") {
		t.Error("a return 7200 blocks from where the player left, across a death in the nether, did not obtain fast_travel")
	}
}

// TestRisingFiftyBlocksOnLevitationObtainsLevitate rises under Levitation:
// 30 blocks above where it took hold does not obtain levitate, 55 does.
// vanilla: ServerPlayer.tick, LevitationTrigger
func TestRisingFiftyBlocksOnLevitationObtainsLevitate(t *testing.T) {
	t.Parallel()
	const levitate = "minecraft:end/levitate"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Floater")
	srv.SetMoveChecks(false)
	servertest.HoldTicks(t, srv)
	start := srv.PlayerPosition("Floater")
	srv.GiveEffect("Floater", "minecraft:levitation", 600, 0, false)
	c.Settle()

	c.MoveToGround(start.X, start.Y+30, start.Z, false)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if srv.AdvancementObtained("Floater", levitate, "levitated") {
		t.Fatal("rising 30 blocks obtained levitate")
	}
	c.MoveToGround(start.X, start.Y+55, start.Z, false)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "levitate is obtained", func() bool {
		return srv.AdvancementObtained("Floater", levitate, "levitated")
	})
}

// fallInto falls the player from above the build limit, in ten-block steps,
// into water placed with its surface at landY.
func fallInto(t *testing.T, name string, landY int32) *server.Server {
	t.Helper()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name)
	srv.SetMoveChecks(false)
	servertest.HoldTicks(t, srv)
	srv.SetBlock(0, landY, 0, servertest.BlockState(t, "minecraft:water"))
	srv.SetBlock(0, landY+1, 0, servertest.BlockState(t, "minecraft:air"))
	for y := 330.0; y > float64(landY); y -= 10 {
		c.MoveToGround(0.5, y, 0.5, false)
	}
	c.MoveToGround(0.5, float64(landY), 0.5, false)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	return srv
}

// TestFallingFromWorldHeightIntoWaterObtainsCavesAndCliffs falls from above
// the build limit into water: landing at y 0 does not obtain
// fall_from_world_height, landing at the bottom of the world does.
// vanilla: ServerPlayer.resetFallDistance, DistanceTrigger
func TestFallingFromWorldHeightIntoWaterObtainsCavesAndCliffs(t *testing.T) {
	t.Parallel()
	const cliffs = "minecraft:adventure/fall_from_world_height"
	for _, tc := range []struct {
		name  string
		landY int32
		want  bool
	}{
		{"Shallow", 0, false},
		{"Deep", -60, true},
	} {
		srv := fallInto(t, tc.name, tc.landY)
		if tc.want {
			servertest.UntilTicks(t, srv, 20, "fall_from_world_height is obtained", func() bool {
				return srv.AdvancementObtained(tc.name, cliffs, "fall_from_world_height")
			})
		} else if srv.AdvancementObtained(tc.name, cliffs, "fall_from_world_height") {
			t.Errorf("a fall into water at y %d obtained fall_from_world_height", tc.landY)
		}
	}
}

// TestSteppingIntoAnEndGatewayObtainsRemoteGetaway stands the player in an
// end gateway, which obtains enter_end_gateway.
// vanilla: ServerPlayer.onInsideBlock, EnterBlockTrigger
func TestSteppingIntoAnEndGatewayObtainsRemoteGetaway(t *testing.T) {
	t.Parallel()
	const gateway = "minecraft:end/enter_end_gateway"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Getaway")
	srv.SetMoveChecks(false)
	if srv.AdvancementObtained("Getaway", gateway, "entered_end_gateway") {
		t.Fatal("enter_end_gateway is obtained before any gateway")
	}
	srv.SetLoadedBlock(3, 90, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	c.MoveToGround(3.5, 90, 3.5, false)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "enter_end_gateway is obtained", func() bool {
		return srv.AdvancementObtained("Getaway", gateway, "entered_end_gateway")
	})
}

// TestFallingAfterAWindBurstObtainsWhoNeedsRockets bursts a wind charge at the
// player's feet, who then rises and starts falling more than seven blocks
// above the burst: fall_after_explosion with the wind charge as the cause.
// vanilla: ServerPlayer.trackStartFallingPosition, FallAfterExplosionTrigger
func TestFallingAfterAWindBurstObtainsWhoNeedsRockets(t *testing.T) {
	t.Parallel()
	const rockets = "minecraft:adventure/who_needs_rockets"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Rocketeer")
	srv.SetMoveChecks(false)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 100, 0.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	srv.RunWindBurst(0.5, 99.5, 0.5)
	c.MoveToGround(0.5, 112, 0.5, false)
	c.Settle()
	c.MoveToGround(0.5, 111, 0.5, false)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "who_needs_rockets is obtained", func() bool {
		return srv.AdvancementObtained("Rocketeer", rockets, "who_needs_rockets")
	})
}

// TestFallingFromWorldHeightIntoABoatObtainsCavesAndCliffs falls from above
// the build limit and boards a boat at the bottom of the world: the seat ends
// the fall and fires fall_from_height from where it began.
// vanilla: LivingEntity.rideTick, ServerPlayer.resetFallDistance
func TestFallingFromWorldHeightIntoABoatObtainsCavesAndCliffs(t *testing.T) {
	t.Parallel()
	const cliffs = "minecraft:adventure/fall_from_world_height"
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Clutch"))
	c := connect().Join("Clutch")
	srv.SetMoveChecks(false)
	servertest.HoldTicks(t, srv)
	if srv.SpawnBoat(2.5, -60, 0.5, "minecraft:oak_boat") == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	for y := 330.0; y > -60; y -= 10 {
		c.MoveToGround(0.5, y, 0.5, false)
	}
	c.MoveToGround(0.5, -60, 0.5, false)
	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	c.ExpectWithinTicks(srv, 10, protocol.IDPlaySystemChat)
	servertest.RunTicks(t, srv, 2)
	servertest.UntilTicks(t, srv, 20, "fall_from_world_height is obtained", func() bool {
		return srv.AdvancementObtained("Clutch", cliffs, "fall_from_world_height")
	})
}
