package mobs

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestATurtleWithAnEggCrossesADeepPondHome: a turtle carrying an egg at the
// far end of a 3-deep pond swims across it and climbs onto its home beach.
// Its go-home goal picks open water cells, stable over any block but air.
// vanilla: Turtle.TurtleGoHomeGoal, Turtle.TurtlePathNavigation.isStableDestination
func TestATurtleWithAnEggCrossesADeepPondHome(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 4; seed++ {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			// A canal walled above the goal's reach (y 63+7), so the only stable
			// cells are the water, the air over it and the beach.
			c.ChatCommand("fill -2 60 -5 33 71 5 minecraft:stone")
			c.ChatCommand("fill -1 64 -4 32 71 4 minecraft:air")
			c.ChatCommand("fill -1 61 -4 24 63 4 minecraft:water")
			c.ChatCommand("fill 25 63 -4 32 63 4 minecraft:sand")
			id := srv.SpawnMobSeeded("minecraft:turtle", 1.5, 61, 0.5, seed)
			c.ExpectMobSpawn(fishType(t, "minecraft:turtle"))
			raw, _ := srv.MobUUID(id)
			c.ChatCommand("data merge entity " + raw + " {has_egg:1b,home_pos:[I;29,64,0]}")
			servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified",
				servertest.EntityDisplayComponent("minecraft:turtle", "entity.minecraft.turtle", uuid.MustParse(raw)))
			// Home targets score brightness. /fill relights on background workers,
			// so held ticks alone do not pin the seeded route. Stop the join's
			// chunk submissions before draining the fixture's lighting.
			// vanilla: Turtle.getWalkTargetValue
			servertest.AwaitChunksSettled(t, srv)
			srv.OverworldWorld().FlushLighting()
			// Settled seeds 1..4 land in 249, 296, 383 and 246 ticks;
			// 400 leaves 17 ticks beyond the slowest route.
			start := srv.TicksRun()
			servertest.UntilTicks(t, srv, 400, "the turtle on its home beach", func() bool {
				x, _, _, _ := srv.MobPos(id)
				return x >= 25 && srv.MobOnGround(id)
			})
			t.Logf("home beach after %d ticks", srv.TicksRun()-start)
		})
	}
}
