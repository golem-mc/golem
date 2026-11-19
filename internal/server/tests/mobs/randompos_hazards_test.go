package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Turtle.TurtleGoHomeGoal, GoalUtils.hasMalus
func TestTurtleGoHomeRefusesBerryBorderTargets(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	// An elevated corridor with walls above the random pick's vertical range
	// leaves only destinations in, over or beside a berry bush.
	c.ChatCommand("fill 2 100 -2 28 110 4 minecraft:stone")
	c.ChatCommand("fill 3 101 0 27 110 2 minecraft:air")
	c.ChatCommand("fill 3 100 0 27 100 2 minecraft:dirt")
	c.ChatCommand("fill 3 101 0 27 101 0 minecraft:sweet_berry_bush[age=1]")
	c.ChatCommand("fill 3 101 2 27 101 2 minecraft:sweet_berry_bush[age=1]")
	c.Settle()
	id := srv.SpawnMobSeeded("minecraft:turtle", 4.5, 101, 1.5, 1)
	raw, _ := srv.MobUUID(id)
	c.ChatCommand("data merge entity " + raw + " {has_egg:1b,home_pos:[I;16,101,1],Invulnerable:1b}")
	c.Settle()
	servertest.RunTicks(t, srv, 300)
	x, _, z, ok := srv.MobPos(id)
	if !ok || math.Hypot(x-4.5, z-1.5) > 0.01 {
		t.Fatalf("turtle walked towards a harmful destination: position (%g, %g), exists %v", x, z, ok)
	}
	// Removing the bushes leaves the same corridor with harmless destinations.
	c.ChatCommand("fill 3 101 0 27 101 0 minecraft:air")
	c.ChatCommand("fill 3 101 2 27 101 2 minecraft:air")
	c.Settle()
	servertest.UntilTicks(t, srv, 300, "the turtle walking home after removing the berries", func() bool {
		x, _, _, _ := srv.MobPos(id)
		return x > 6
	})
}
