package movement

import (
	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

// TestMinecartLandingHurtsItsMobRider: a cow riding a cart that falls ten
// blocks takes the fall past three, cut to a fifth on a hay bale.
// vanilla: Entity.propagateFallToPassengers
func TestMinecartLandingHurtsItsMobRider(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block string
		want  float64
		sound string
	}{
		{"stone", "minecraft:stone", 7, "minecraft:entity.generic.big_fall"},
		{"hay bale", "minecraft:hay_block", 1, "minecraft:entity.generic.small_fall"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := rideMobWorld(t)
			srv.SetBlock(6, 63, 6, servertest.BlockState(t, tc.block))
			srv.SpawnMinecart(6.5, 74, 6.5)
			cow := srv.SpawnMob("minecraft:cow", 6.5, 74, 6.5)
			c.Settle()
			c.ChatCommand("ride @e[type=cow,limit=1] mount @e[type=minecart,limit=1]")
			servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
			servertest.UntilTicks(t, srv, 40, "the cart lands and hurts the cow", func() bool {
				h, _ := srv.MobHealth(cow)
				return h < 10
			})
			var big, small int
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlaySoundEffect {
					switch servertest.SoundOf(data) {
					case "minecraft:entity.generic.big_fall":
						big++
					case "minecraft:entity.generic.small_fall":
						small++
					}
				}
			})
			wantBig, wantSmall := 0, 1
			if tc.sound == "minecraft:entity.generic.big_fall" {
				wantBig, wantSmall = 1, 0
			}
			if big != wantBig || small != wantSmall {
				t.Errorf("fall sounds: big=%d small=%d, want big=%d small=%d", big, small, wantBig, wantSmall)
			}
			h, _ := srv.MobHealth(cow)
			if got := 10 - h; math.Abs(got-tc.want) > 1e-4 {
				t.Errorf("the cow lost %g health, want %g", got, tc.want)
			}
		})
	}
}

// TestMinecartLavaReducesItsRidersFall drops ten blocks through a contained
// lava layer; fire resistance isolates the fall from the fluid's burn damage.
func TestMinecartLavaReducesItsRidersFall(t *testing.T) {
	t.Parallel()
	srv, c := rideMobWorld(t)
	c.ChatCommand("gamemode survival @s")
	c.ChatCommand("effect give @s minecraft:fire_resistance 100 0 true")
	c.Settle()
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(6, 63, 6, stone)
	for x := int32(5); x <= 7; x++ {
		for z := int32(5); z <= 7; z++ {
			srv.SetBlock(x, 69, z, stone)
		}
	}
	c.ChatCommand("setblock 6 68 6 minecraft:crimson_fence_gate[open=true]")
	c.ChatCommand("setblock 6 69 6 minecraft:lava")
	c.Settle()
	c.ChatCommand("summon minecraft:minecart 6.5 74 6.5 {Invulnerable:1b}")
	_, cart := c.AwaitNextSpawn()
	c.ChatCommand("ride @s mount @e[type=minecart,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	servertest.UntilTicks(t, srv, 40, "the cart lands after crossing lava", func() bool {
		_, y, _, _, ok := srv.MinecartState(cart.EntityID)
		return ok && y == 64
	})
	if got := srv.PlayerHealth("Rider"); got != 18 {
		t.Errorf("rider health = %g, want 18 after the lava-reduced fall", got)
	}
}
