package advancements

import (
	"testing"

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
