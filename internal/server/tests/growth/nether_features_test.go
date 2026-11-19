package growth

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestPlaceFeatureGrowsInTheNether places a fungus and a tree from an op in
// the Nether, whose min Y and height differ from the overworld's; bone meal
// and saplings grow through the same placement.
// vanilla: ConfiguredFeature.place, PlaceCommand.placeFeature
func TestPlaceFeatureGrowsInTheNether(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.BlockTestConfig()), "Actor", servertest.OpsListing(t, "Actor"))

	c.ChatCommand("execute in minecraft:the_nether run setblock 8 40 8 minecraft:stone")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "8", "40", "8")
	srv.MovePlayerToDimension("Actor", world.NetherName, 8.5, 41, 8.5)
	c.ExpectViewWithinTicks(srv, 200)
	c.Settle()

	// Clear the air the fungus and the tree grow into, whatever the
	// Nether's terrain holds there.
	c.ChatCommand("fill 10 41 6 22 60 10 minecraft:air")
	filled := int64(13 * 20 * 5)
	servertest.AwaitCommandFeedbackText(t, c, "commands.fill.success", protocol.Text{Number: &filled})

	for _, tc := range []struct {
		ground, feature, want string
		x                     int32
	}{
		{"crimson_nylium", "minecraft:crimson_fungus_planted", "minecraft:crimson_stem", 12},
		{"dirt", "minecraft:oak", "minecraft:oak_log", 20},
	} {
		servertest.Setblock(t, c, tc.x, 40, 8, tc.ground)
		c.ChatCommand("place feature " + tc.feature + " " + strconv.Itoa(int(tc.x)) + " 41 8")
		x, y, z := int64(tc.x), int64(41), int64(8)
		servertest.ExpectFeedbackSoon(t, c, nil, "commands.place.feature.success",
			protocol.Text{Text: tc.feature}, protocol.Text{Number: &x}, protocol.Text{Number: &y}, protocol.Text{Number: &z})
		got := srv.BlockInDimension(world.NetherName, tc.x, 41, 8)
		if name := registry.BlockOf(got).Name(); name != tc.want {
			t.Errorf("%s: block at (%d,41,8) = %s, want %s", tc.feature, tc.x, name, tc.want)
		}
	}
}
