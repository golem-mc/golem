package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestATurtleWithAnEggSwimsHomeAlongAChannel: a turtle carrying an egg swims
// the length of a walled channel to its home on the beach at the far end,
// whatever its seed. The budget is about half again the slowest seed (263).
// vanilla: Turtle.TurtleGoHomeGoal, TurtlePathNavigation
func TestATurtleWithAnEggSwimsHomeAlongAChannel(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 4; seed++ {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			// Walls too high to aim over and a channel one block deep: the
			// go-home goal's target search finds no cell over deeper water,
			// and a cell beside the channel would send the turtle round it.
			c.ChatCommand("fill 2 63 -8 38 70 18 minecraft:stone")
			c.ChatCommand("fill 3 64 4 37 70 6 minecraft:air")
			c.ChatCommand("fill 3 63 4 29 63 6 minecraft:water")
			c.ChatCommand("fill 30 63 4 37 63 6 minecraft:sand")
			c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
			id := srv.SpawnMobSeeded("minecraft:turtle", 4.5, 63, 5.5, seed)
			uuid, _ := srv.MobUUID(id)
			c.ChatCommand("data merge entity " + uuid + " {has_egg:1b,home_pos:[I;35,64,5]}")
			// Pins the merge to the next tick: a command still on the wire
			// when the ticks start lands a tick or two late, which shifts
			// the turtle's random draws and its time home by hundreds of ticks.
			c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
			servertest.UntilTicks(t, srv, 400, "the turtle at the end of the channel", func() bool {
				x, _, _, _ := srv.MobPos(id)
				return x >= 27
			})
		})
	}
}

// TestATurtleTemptedAcrossDeepWaterSwimsStraight: a turtle at the bottom of
// a canal 5 deep follows seagrass 21 blocks off, passing each path node it
// is a block or more below on a clear line rather than circling it. It
// comes in 83 ticks on every seed; circling each node costs it 20 more.
// vanilla: AmphibiousPathNavigation.canMoveDirectly, PathNavigation.shouldTargetNextNodeInDirection
func TestATurtleTemptedAcrossDeepWaterSwimsStraight(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 4; seed++ {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "minecraft:seagrass", 1, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("fill 1 58 -4 24 71 4 minecraft:stone")
			c.ChatCommand("fill 2 59 -3 23 63 3 minecraft:water")
			c.ChatCommand("fill 2 64 -3 23 71 3 minecraft:air")
			c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
			id := srv.SpawnMobSeeded("minecraft:turtle", 21.5, 59, 0.5, seed)
			c.ChatCommand("attribute @e[type=turtle,limit=1] minecraft:tempt_range base set 30")
			// Pins the command to the next tick, so each seed swims the same
			// way every run.
			c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
			servertest.UntilTicks(t, srv, 92, "the turtle at the seagrass", func() bool {
				x, _, z, _ := srv.MobPos(id)
				return math.Hypot(x-0.5, z-0.5) < 3
			})
		})
	}
}
