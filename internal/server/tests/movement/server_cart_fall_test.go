package movement

import (
	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMinecartLandingHurtsItsRider: a cart that falls ten blocks hurts the
// player aboard by the fall past three, cut to a fifth on a hay bale.
// vanilla: Entity.propagateFallToPassengers
func TestMinecartLandingHurtsItsRider(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		block      string
		multiplier float64
		safe       float64
		want       float64
	}{
		{"stone", "minecraft:stone", 1, 3, 7},
		{"double", "minecraft:stone", 2, 3, 14},
		{"half", "minecraft:stone", 0.5, 3, 3},
		{"zero", "minecraft:stone", 0, 3, 0},
		{"safe distance", "minecraft:stone", 2, 4, 12},
		{"double on hay", "minecraft:hay_block", 2, 3, 2},
		{"hay bale", "minecraft:hay_block", 1, 3, 1}, // floor((10-3)*0.2)
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := rideMobWorld(t)
			c.ChatCommand("gamemode survival @s")
			c.Settle()
			srv.SetBlock(6, 63, 6, servertest.BlockState(t, tc.block))
			cart := srv.SpawnMinecart(6.5, 74, 6.5)
			for _, attr := range []struct {
				name  string
				value float64
			}{{"fall_damage_multiplier", tc.multiplier}, {"safe_fall_distance", tc.safe}} {
				c.ChatCommand("attribute @s minecraft:" + attr.name + " base set " + strconv.FormatFloat(attr.value, 'f', -1, 64))
				servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			}
			c.Settle()
			c.ChatCommand("ride @s mount @e[type=minecart,limit=1]")
			servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
			servertest.UntilTicks(t, srv, 40, "the cart lands", func() bool {
				_, y, _, _, ok := srv.MinecartState(cart)
				return ok && y == 64
			})
			if got := 20 - srv.PlayerHealth("Rider"); math.Abs(got-tc.want) > 1e-4 {
				t.Errorf("the rider lost %g health, want %g", got, tc.want)
			}
		})
	}
}
