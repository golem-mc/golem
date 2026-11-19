package mobs

import (
	"fmt"
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
